package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errRoutingProvider = errors.New("unknown routing provider")
var errRoutingConfiguration = errors.New("configure an OpenRouter key for this Atlas app and restart it; no fallback was used")
var errGroqConfiguration = errors.New("configure a Groq key for this Atlas app and restart it; no fallback was used")

// Both the stateless evaluation API and durable conversation entry use this
// boundary. A turn never calls it again after the session has been created.
func evaluateRouting(parent context.Context, s *store.Store, service routingService, name string, input routing.Request) (routing.Result, error) {
	if err := routing.ValidateRequest(input); err != nil {
		return routing.Result{}, err
	}
	var evaluator routing.Evaluator
	switch name {
	case "mock":
		evaluator = routing.Mock{}
	case "jev":
		if input.Fixture != "" {
			return routing.Result{}, routing.ErrRequest
		}
		if !service.configured {
			return routing.Result{}, errRoutingConfiguration
		}
		if service.free != nil {
			return evaluateJevPipeline(parent, s, service, input)
		}
		evaluator = service.jev
	case "free":
		if input.Fixture != "" {
			return routing.Result{}, routing.ErrRequest
		}
		if !service.configured || service.free == nil {
			return routing.Result{}, errRoutingConfiguration
		}
	case "groq":
		if input.Fixture != "" {
			return routing.Result{}, routing.ErrRequest
		}
		if !service.groqConfigured || service.groq == nil {
			return routing.Result{}, errGroqConfiguration
		}
	default:
		return routing.Result{}, errRoutingProvider
	}
	timeout := 20 * time.Second
	if name == "free" || name == "groq" {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	state := routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), RecentRecordID: input.RecentRecordID, RecentRecordKind: input.RecentRecordKind, Context: []provider.ContextRecord{}}
	if name == "jev" || name == "free" || name == "groq" {
		tasks, err := s.Tasks(ctx)
		if err != nil {
			return routing.Result{}, err
		}
		reminders, err := s.Reminders(ctx)
		if err != nil {
			return routing.Result{}, err
		}
		notes, err := s.Notes(ctx)
		if err != nil {
			return routing.Result{}, err
		}
		add := func(item provider.ContextRecord) {
			if len(state.Context) < 50 {
				state.Context = append(state.Context, item)
			} else {
				state.ContextTruncated = true
			}
		}
		for _, task := range tasks {
			add(provider.ContextRecord{ID: task.ID, Kind: "task", Title: task.Title, Body: lookupExcerpt(task.Details), Status: task.Status, DueAt: task.DueAt, UpdatedAt: task.UpdatedAt})
		}
		for _, reminder := range reminders {
			add(provider.ContextRecord{ID: reminder.ID, Kind: "reminder", Title: reminder.Title, Status: reminder.Status, DueAt: reminder.ScheduledAt, UpdatedAt: reminder.UpdatedAt, TaskID: reminder.TaskID, TaskTitle: reminder.TaskTitle})
		}
		for _, note := range notes {
			record := provider.ContextRecord{ID: note.ID, Kind: "note", Title: noteLabel(note), Body: lookupExcerpt(note.Body), UpdatedAt: note.UpdatedAt}
			for _, link := range note.Links {
				if link.TargetType == "task" {
					record.TaskID, record.TaskTitle = link.TargetID, link.TargetTitle
					break
				}
			}
			add(record)
		}
	} else if strings.TrimSpace(input.ContextQuery) != "" {
		found, err := s.Search(ctx, store.SearchOptions{Query: input.ContextQuery, Type: "task", Status: "open", Limit: 5})
		if err != nil {
			return routing.Result{}, err
		}
		state.ContextTruncated = found.HasMore
		for _, hit := range found.Results {
			snapshot, err := s.TaskState(ctx, hit.ID)
			if err != nil {
				return routing.Result{}, err
			}
			t := snapshot.Task
			if t.Status == "open" {
				state.Context = append(state.Context, provider.ContextRecord{ID: t.ID, Title: t.Title, DueAt: t.DueAt, UpdatedAt: t.UpdatedAt})
			}
		}
	}
	if name == "free" || name == "groq" {
		planner := service.free
		if name == "groq" {
			planner = service.groq
		}
		planned, err := planner.Plan(ctx, state)
		if err != nil {
			return routing.Result{}, err
		}
		planned.Plan = routing.NormalizePlan(planned.Plan)
		planned.Plan = routing.NormalizeEditExtraction(planned.Plan, state)
		planned.Plan = routing.NormalizePlanTiming(planned.Plan, state)
		if err := routing.ValidatePlan(planned.Plan, state); err != nil {
			return routing.Result{}, err
		}
		result := routing.Result{Version: routing.Version, RegistryVersion: routing.RegistryVersion, RequestID: input.RequestID, Provider: name, State: "routed", Input: state, Plan: &planned.Plan, Evaluation: routing.Evaluation{Model: planned.Model, Usage: planned.Usage, ModelCalls: 1, EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
		for _, action := range routing.Registry() {
			if action.ID == planned.Plan.Action {
				result.SelectedChannel = action.Channel
				break
			}
		}
		switch planned.Plan.Action {
		case "clarify":
			result.State = "needs_clarification"
		case "unsupported":
			result.State = "unsupported"
		}
		result.RoutingToken = service.sign(result)
		return result, nil
	}
	evaluation, err := evaluator.Evaluate(ctx, state, routing.Registry(), input.Fixture)
	if err != nil {
		return routing.Result{}, err
	}
	result, err := routing.Decide(input, state, evaluation, name, name == "mock")
	if err != nil {
		return routing.Result{}, err
	}
	if name == "jev" && result.State == "needs_clarification" && evaluation.Decision.Choice != "clarify" {
		// A broad choice can split its probability between, for example,
		// editing an existing task and creating a new one. Ask Jev one
		// narrower question using the same context before interrupting the user.
		second := narrowRouteChoices(evaluation.Decision, routing.Registry())
		if len(second) == 3 {
			focused, focusErr := evaluator.Evaluate(ctx, state, second, "")
			if focusErr == nil && focused.Decision.Choice != "clarify" && focused.Decision.Probabilities[focused.Decision.Choice] >= 0.65 {
				full := map[string]float64{}
				for _, action := range routing.Registry() {
					full[action.ID] = focused.Decision.Probabilities[action.ID]
				}
				focused.Decision.Probabilities = full
				focused.Usage.InputTokens += evaluation.Usage.InputTokens
				focused.Usage.OutputTokens += evaluation.Usage.OutputTokens
				if priorCost, priorErr := strconv.ParseFloat(evaluation.Usage.CostUSD, 64); priorErr == nil {
					if focusedCost, focusCostErr := strconv.ParseFloat(focused.Usage.CostUSD, 64); focusCostErr == nil {
						focused.Usage.CostUSD = strconv.FormatFloat(priorCost+focusedCost, 'f', 8, 64)
					}
				}
				focused.ModelCalls += evaluation.ModelCalls
				result, err = routing.Decide(input, state, focused, name, false)
				if err != nil {
					return routing.Result{}, err
				}
			}
		}
	}
	result.RoutingToken = service.sign(result)
	return result, nil
}

func narrowRouteChoices(decision routing.Choice, actions []routing.Action) []routing.Action {
	if decision.Probabilities[decision.Choice] < 0.40 || decision.Choice == "unsupported" {
		return nil
	}
	first, second := routing.Action{}, routing.Action{}
	for _, action := range actions {
		if action.ID == decision.Choice {
			first = action
		} else if action.ID != "clarify" && action.ID != "unsupported" && (second.ID == "" || decision.Probabilities[action.ID] > decision.Probabilities[second.ID]) {
			second = action
		}
	}
	if first.ID == "" || second.ID == "" || decision.Probabilities[second.ID] == 0 {
		return nil
	}
	for _, action := range actions {
		if action.ID == "clarify" {
			return []routing.Action{first, second, action}
		}
	}
	return nil
}

func routingFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRoutingProvider):
		apiError(w, 404, "routing_provider_not_found", err.Error(), false)
	case errors.Is(err, routing.ErrRequest):
		apiError(w, 400, "invalid_routing_request", err.Error(), false)
	case errors.Is(err, errRoutingConfiguration):
		apiError(w, 503, "routing_unavailable", err.Error(), false)
	case errors.Is(err, errGroqConfiguration):
		apiError(w, 503, "routing_unavailable", err.Error(), false)
	case errors.Is(err, routing.ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		message, retryable := routing.ErrUnavailable.Error(), true
		var upstream routing.RemoteError
		if errors.As(err, &upstream) {
			message = upstream.Error()
			retryable = upstream.Status == 429 || upstream.Status >= 500
		}
		apiError(w, 503, "routing_unavailable", message, retryable)
	case errors.Is(err, routing.ErrContract):
		log.Printf("Atlas routing contract rejected: %v", err)
		apiError(w, 422, "routing_evaluation_rejected", err.Error(), false)
	default:
		failure(w, err)
	}
}
