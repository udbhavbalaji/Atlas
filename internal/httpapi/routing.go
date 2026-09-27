package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type routingService struct {
	jev        routing.Evaluator
	configured bool
	secret     string
}
type routingReceipt struct {
	Result    routing.Result `json:"result"`
	ExpiresAt int64          `json:"expires_at"`
}

func (s routingService) sign(r routing.Result) string {
	r.RoutingToken = ""
	body, _ := json.Marshal(routingReceipt{r, time.Now().Add(30 * time.Minute).Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s routingService) verify(token string) (routing.Result, error) {
	if len(token) > 150000 {
		return routing.Result{}, routing.ErrRequest
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return routing.Result{}, routing.ErrRequest
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return routing.Result{}, routing.ErrRequest
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return routing.Result{}, routing.ErrRequest
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return routing.Result{}, routing.ErrRequest
	}
	var receipt routingReceipt
	if json.Unmarshal(body, &receipt) != nil || time.Now().Unix() >= receipt.ExpiresAt || receipt.Result.RegistryVersion != routing.RegistryVersion {
		return routing.Result{}, routing.ErrRequest
	}
	return receipt.Result, nil
}

func routingRoutes(mux *http.ServeMux, s *store.Store) {
	key := os.Getenv("AI_GATEWAY_API_KEY")
	routingRoutesWithService(mux, s, routingService{jev: &routing.Cached{Provider: routing.Jev{APIKey: key}}, configured: key != "", secret: rand.Text()})
}
func routingRoutesWithService(mux *http.ServeMux, s *store.Store, service routingService) {
	mux.HandleFunc("GET /api/v1/routing", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"version": routing.Version, "registry_version": routing.RegistryVersion, "actions": routing.Registry(), "providers": []map[string]any{{"id": "mock", "configured": true, "mock": true}, {"id": "jev", "configured": service.configured, "mock": false, "configuration_only": true}}, "policy": routing.Policy{MinProbability: 0.65, MinMargin: 0.15}, "context_budget": map[string]int{"queries": 1, "records": 5}, "persists_on_evaluation": false, "token_lifetime_seconds": 1800, "cache": map[string]int{"ttl_seconds": 300, "max_entries": 128}})
	})
	mux.HandleFunc("POST /api/v1/routing/{provider}/evaluate", func(w http.ResponseWriter, r *http.Request) {
		var input routing.Request
		if !decode(w, r, &input) {
			return
		}
		if err := routing.ValidateRequest(input); err != nil {
			apiError(w, 400, "invalid_routing_request", err.Error(), false)
			return
		}
		p := r.PathValue("provider")
		var evaluator routing.Evaluator
		switch p {
		case "mock":
			evaluator = routing.Mock{}
		case "jev":
			if input.Fixture != "" {
				apiError(w, 400, "invalid_routing_request", "Fixture controls are only accepted in mock mode.", false)
				return
			}
			if !service.configured {
				apiError(w, 503, "routing_unavailable", "Save the key in data/ai-gateway.key or set AI_GATEWAY_API_KEY and restart Atlas. No fallback was used.", false)
				return
			}
			evaluator = service.jev
		default:
			apiError(w, 404, "routing_provider_not_found", "Unknown routing provider.", false)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		state := routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), Context: []provider.ContextRecord{}}
		if strings.TrimSpace(input.ContextQuery) != "" {
			found, err := s.Search(ctx, store.SearchOptions{Query: input.ContextQuery, Type: "task", Status: "open", Limit: 5})
			if err != nil {
				failure(w, err)
				return
			}
			state.ContextTruncated = found.HasMore
			for _, hit := range found.Results {
				snapshot, err := s.TaskState(ctx, hit.ID)
				if err != nil {
					failure(w, err)
					return
				}
				t := snapshot.Task
				if t.Status == "open" {
					state.Context = append(state.Context, provider.ContextRecord{ID: t.ID, Title: t.Title, DueAt: t.DueAt, UpdatedAt: t.UpdatedAt})
				}
			}
		}
		evaluation, err := evaluator.Evaluate(ctx, state, routing.Registry(), input.Fixture)
		if err == nil {
			var result routing.Result
			result, err = routing.Decide(input, state, evaluation, p, p == "mock")
			if err == nil {
				result.RoutingToken = service.sign(result)
				respond(w, 200, result)
				return
			}
		}
		if errors.Is(err, routing.ErrUnavailable) || ctx.Err() != nil {
			message, retryable := routing.ErrUnavailable.Error(), true
			var upstream routing.RemoteError
			if errors.As(err, &upstream) {
				message = upstream.Error()
				retryable = upstream.Status == 429 || upstream.Status >= 500
			}
			apiError(w, 503, "routing_unavailable", message, retryable)
		} else if errors.Is(err, routing.ErrRequest) {
			apiError(w, 400, "invalid_routing_request", err.Error(), false)
		} else {
			apiError(w, 422, "routing_evaluation_rejected", routing.ErrContract.Error(), false)
		}
	})
	mux.HandleFunc("POST /api/v1/routing/dispatch", func(w http.ResponseWriter, r *http.Request) {
		var input dispatchRequest
		if !decode(w, r, &input) {
			return
		}
		if input.Version != routing.Version {
			apiError(w, 400, "invalid_routing_request", "Unsupported dispatch version.", false)
			return
		}
		result, err := service.verify(input.RoutingToken)
		if err != nil {
			apiError(w, 409, "routing_receipt_invalid", "Routing receipt changed, expired, or belongs to an earlier server instance. Evaluate again.", false)
			return
		}
		channel := result.SelectedChannel
		if input.Channel != "" {
			if result.State != "needs_clarification" || !input.ReviewedChannel {
				apiError(w, 400, "invalid_channel_selection", "Only an unresolved route accepts a reviewed channel selection.", false)
				return
			}
			channel = input.Channel
		}
		if channel == "" {
			apiError(w, 422, "routing_unresolved", "Resolve the primary channel before dispatching. Unsupported requests cannot be dispatched.", false)
			return
		}
		response, err := dispatchChannel(r.Context(), s, result, channel, input)
		if err != nil {
			if errors.Is(err, routing.ErrRequest) {
				apiError(w, 400, "invalid_channel_fields", err.Error(), false)
			} else {
				failure(w, err)
			}
			return
		}
		respond(w, 200, response)
	})
	for path, allow := range map[string]string{"/api/v1/routing": "GET, HEAD", "/api/v1/routing/{provider}/evaluate": "POST", "/api/v1/routing/dispatch": "POST"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}

type dispatchRequest struct {
	Version         string             `json:"version"`
	RoutingToken    string             `json:"routing_token"`
	Channel         string             `json:"channel"`
	ReviewedChannel bool               `json:"reviewed_channel"`
	Fields          store.CaptureInput `json:"fields"`
	Reminder        string             `json:"reminder"`
	Note            string             `json:"note"`
	Prefill         bool               `json:"prefill"`
}
type channelQuestion struct {
	ID        string            `json:"id"`
	Field     string            `json:"field"`
	ValueType string            `json:"value_type"`
	Prompt    string            `json:"prompt"`
	Required  bool              `json:"required"`
	Choices   []provider.Choice `json:"choices"`
}
type channelResponse struct {
	Version    string                 `json:"version"`
	Channel    string                 `json:"channel"`
	State      string                 `json:"state"`
	Source     string                 `json:"source"`
	Input      routing.State          `json:"input"`
	Prefill    *channelPrefill        `json:"prefill,omitempty"`
	Extraction string                 `json:"extraction"`
	Questions  []channelQuestion      `json:"questions"`
	Proposal   *store.CaptureProposal `json:"proposal"`
	Persisted  bool                   `json:"persisted"`
}
type channelHandler func(context.Context, *store.Store, routing.Result, dispatchRequest) (channelResponse, error)

func dispatchChannel(ctx context.Context, s *store.Store, result routing.Result, channel string, input dispatchRequest) (channelResponse, error) {
	handlers := map[string]channelHandler{"tasks": taskChannel, "reminders": reminderChannel, "notes": noteChannel}
	handler, ok := handlers[channel]
	if !ok {
		return channelResponse{}, routing.ErrRequest
	}
	if input.Fields.PreviewID != "" || input.Fields.BeforeTaskVersion != "" || input.Fields.Kind != "" || input.Fields.Timezone != "" {
		return channelResponse{}, routing.ErrRequest
	}
	if input.Reminder != "" && input.Reminder != "add" && input.Reminder != "skip" || input.Note != "" && input.Note != "add" && input.Note != "skip" {
		return channelResponse{}, routing.ErrRequest
	}
	return handler(ctx, s, result, input)
}
func taskChannel(ctx context.Context, s *store.Store, r routing.Result, in dispatchRequest) (channelResponse, error) {
	return captureChannel(ctx, s, r, in, "tasks", "task")
}
func reminderChannel(ctx context.Context, s *store.Store, r routing.Result, in dispatchRequest) (channelResponse, error) {
	return captureChannel(ctx, s, r, in, "reminders", "reminder")
}
func noteChannel(ctx context.Context, s *store.Store, r routing.Result, in dispatchRequest) (channelResponse, error) {
	return captureChannel(ctx, s, r, in, "notes", "note")
}

// Channels collect reviewed fields. Initial preparation can offer source-derived
// hints without rerouting or making a model call; subsequent edits are explicit.
func captureChannel(ctx context.Context, s *store.Store, r routing.Result, in dispatchRequest, channel, kind string) (channelResponse, error) {
	out := channelResponse{Version: routing.Version, Channel: channel, State: "needs_fields", Source: r.Input.Text, Input: r.Input, Extraction: "explicit_fields", Questions: []channelQuestion{}}
	if in.Prefill {
		seed := prepareChannel(r.Input, kind)
		out.Prefill = &seed
		in = applyChannelPrefill(in, *out.Prefill)
		out.Extraction = "local_prefill_review"
	}
	draft := in.Fields
	draft.Kind = kind
	if kind == "task" && in.Reminder == "" && draft.ReminderAt != "" {
		in.Reminder = "add"
	}
	if kind != "note" && in.Note == "" && draft.NoteBody != "" {
		in.Note = "add"
	}
	ask := func(field, typ, prompt string, choices ...provider.Choice) {
		if choices == nil {
			choices = []provider.Choice{}
		}
		out.Questions = append(out.Questions, channelQuestion{ID: channel + "." + field, Field: field, ValueType: typ, Prompt: prompt, Required: true, Choices: choices})
	}
	if kind != "note" && strings.TrimSpace(draft.Title) == "" {
		ask("fields.title", "string", "What title should be saved? Review the source sentence and its timing separately.")
	}
	if kind == "task" {
		if in.Reminder == "" {
			ask("reminder", "choice", "Add a linked reminder?", provider.Choice{Value: "add", Label: "Add reminder"}, provider.Choice{Value: "skip", Label: "Skip reminder"})
		}
		if in.Reminder == "skip" && (draft.ReminderAt != "" || draft.ReminderTitle != "" || draft.Repeat != "") {
			return out, routing.ErrRequest
		}
		if in.Reminder == "add" && draft.ReminderAt == "" {
			ask("fields.reminder_at", "rfc3339", "When should Atlas remind you?")
		}
	} else if kind == "reminder" {
		if in.Reminder != "" {
			return out, routing.ErrRequest
		}
		if draft.ReminderAt == "" {
			ask("fields.reminder_at", "rfc3339", "When should Atlas remind you?")
		}
	} else if in.Reminder != "" || in.Note != "" {
		return out, routing.ErrRequest
	}
	if kind != "note" {
		if in.Note == "" {
			ask("note", "choice", "Attach a note?", provider.Choice{Value: "add", Label: "Add note"}, provider.Choice{Value: "skip", Label: "Skip note"})
		}
		if in.Note == "skip" && draft.NoteBody != "" {
			return out, routing.ErrRequest
		}
		if in.Note == "add" && strings.TrimSpace(draft.NoteBody) == "" {
			ask("fields.note_body", "string", "What should the attached note contain?")
		}
	} else if strings.TrimSpace(draft.NoteBody) == "" {
		ask("fields.note_body", "string", "What information should the note retain?")
	}
	// Dependency candidates must have been supplied in the initial bounded
	// context. CapturePreview rechecks canonical state and captures its version.
	if draft.BeforeTaskID != "" {
		allowed := false
		for _, record := range r.Input.Context {
			if record.ID == draft.BeforeTaskID {
				allowed = true
			}
		}
		if !allowed {
			return out, routing.ErrRequest
		}
	}
	if len(out.Questions) > 0 {
		return out, nil
	}
	if in.Prefill {
		return out, nil
	}
	if draft.ReminderAt != "" {
		draft.Timezone = r.Input.Timezone
	}
	proposal, err := s.CapturePreview(ctx, draft)
	if err != nil {
		return out, err
	}
	out.State = "awaiting_confirmation"
	out.Proposal = &proposal
	return out, nil
}
