package httpapi

import (
	"atlas/internal/planning"
	"atlas/internal/routing"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type planningService struct {
	enabled    bool
	batch      routing.BatchEvaluator
	correction routing.Evaluator
	secret     string
}
type planningReceipt struct {
	Kind      string        `json:"kind"`
	Plan      planning.Plan `json:"plan"`
	ExpiresAt int64         `json:"expires_at"`
}

func (s planningService) sign(p planning.Plan) string {
	b, _ := json.Marshal(planningReceipt{"plan-preview-v1", p, time.Now().Add(30 * time.Minute).Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(b)
	m := hmac.New(sha256.New, []byte(s.secret))
	m.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s planningService) verify(token string) (planning.Plan, error) {
	parts := strings.Split(token, ".")
	if len(token) > 60000 || len(parts) != 2 {
		return planning.Plan{}, routing.ErrRequest
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return planning.Plan{}, routing.ErrRequest
	}
	m := hmac.New(sha256.New, []byte(s.secret))
	m.Write([]byte(parts[0]))
	if !hmac.Equal(sig, m.Sum(nil)) {
		return planning.Plan{}, routing.ErrRequest
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return planning.Plan{}, routing.ErrRequest
	}
	var v planningReceipt
	if json.Unmarshal(b, &v) != nil || v.Kind != "plan-preview-v1" || v.ExpiresAt <= time.Now().Unix() {
		return planning.Plan{}, routing.ErrRequest
	}
	_, err = planning.Inspect(v.Plan)
	return v.Plan, err
}
func planningRoutes(mux *http.ServeMux) {
	j := routing.Jev{APIKey: os.Getenv("OPENROUTER_API_KEY")}
	planningRoutesWithService(mux, planningService{enabled: os.Getenv("ATLAS_PLAN_PREVIEW") == "1", batch: j, correction: &routing.Cached{Provider: j}, secret: rand.Text()})
}
func planningRoutesWithService(mux *http.ServeMux, s planningService) {
	mux.HandleFunc("GET /api/v1/planning", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"enabled": s.enabled, "preview_only": true, "max_actions": planning.MaxActions, "capabilities": []string{"task.create", "reminder.create", "note.create"}, "fallback_url": "/desktop", "persists": false})
	})
	gate := func(w http.ResponseWriter) bool {
		if !s.enabled {
			apiError(w, 404, "planning_disabled", "Plan previews are disabled. Use the existing single-action flow, or start Atlas with ATLAS_PLAN_PREVIEW=1.", false)
			return false
		}
		return true
	}
	reply := func(w http.ResponseWriter, out planning.Result) {
		respond(w, 200, struct {
			planning.Result
			PreviewToken string `json:"preview_token"`
		}{out, s.sign(out.Plan)})
	}

	mux.HandleFunc("GET /api/v1/planning/fixtures", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, planning.Fixtures()) })
	mux.HandleFunc("POST /api/v1/planning/fixtures/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) || !emptyAction(w, r) {
			return
		}
		out, err := planning.FixturePreview(r.Context(), r.PathValue("name"))
		if err != nil {
			routingFailure(w, err)
			return
		}
		reply(w, out)
	})
	mux.HandleFunc("POST /api/v1/planning/preview", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		var input struct {
			Text     string `json:"text"`
			Timezone string `json:"timezone"`
		}
		if !decode(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if s.batch == nil {
			routingFailure(w, routing.ErrUnavailable)
			return
		}
		out, err := planning.Preview(ctx, s.batch, planning.Request{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err != nil {
			routingFailure(w, err)
			return
		}
		reply(w, out)
	})
	mux.HandleFunc("POST /api/v1/planning/revise", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		var input struct {
			PreviewToken string `json:"preview_token"`
			Text         string `json:"text"`
			ActionID     string `json:"action_id"`
			Field        string `json:"field"`
			Value        string `json:"value"`
		}
		if !decode(w, r, &input) {
			return
		}
		p, err := s.verify(input.PreviewToken)
		if err != nil {
			apiError(w, 409, "plan_preview_invalid", "Preview changed, expired or belongs to an earlier server instance. Preview again.", false)
			return
		}
		var evaluation routing.Evaluation
		source := input.Value
		if input.Text != "" {
			if input.ActionID != "" || input.Field != "" || input.Value != "" || len(input.Text) > 4000 {
				routingFailure(w, routing.ErrRequest)
				return
			}
			// Reuse the conversation correction's explicit value grammar. Reject an
			// unparseable value before spending a model call.
			match := statedValue.FindStringSubmatch(input.Text)
			if len(match) != 2 {
				apiError(w, 422, "plan_correction_unresolved", "Name the action and use ‘change <field> to <value>’, or use the field controls.", false)
				return
			}
			if s.correction == nil {
				routingFailure(w, routing.ErrUnavailable)
				return
			}
			stateJSON, _ := json.Marshal(p)
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			evaluation, err = s.correction.Evaluate(ctx, routing.State{Text: input.Text, Timezone: p.Timezone, ReferenceAt: p.ReferenceAt, RecentConversation: string(stateJSON)}, planning.RevisionChoices(p), "")
			if err != nil {
				routingFailure(w, err)
				return
			}
			id, field, ok := planning.RevisionTarget(evaluation.Decision, p)
			if !ok {
				apiError(w, 422, "plan_correction_unresolved", "Please choose one action and field; the existing preview is unchanged.", false)
				return
			}
			input.ActionID, input.Field, input.Value = id, field, strings.TrimSpace(match[1])
			source = input.Text
		}
		out, err := planning.Revise(p, input.ActionID, input.Field, input.Value, source)
		if err != nil {
			routingFailure(w, err)
			return
		}
		if input.Text != "" {
			out.ModelCalls = 1
			if evaluation.CacheHit {
				out.ModelCalls = 0
			}
			out.Model = evaluation.Model
			out.Usage = evaluation.Usage
		}
		reply(w, out)
	})
}
