package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

var errRoutingProvider = errors.New("unknown routing provider")
var errRoutingConfiguration = errors.New("configure an OpenRouter key for this Atlas app and restart it; no fallback was used")

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
		evaluator = service.jev
	case "free":
		if input.Fixture != "" {
			return routing.Result{}, routing.ErrRequest
		}
		if !service.configured || service.free == nil {
			return routing.Result{}, errRoutingConfiguration
		}
	default:
		return routing.Result{}, errRoutingProvider
	}
	timeout := 20 * time.Second
	if name == "free" {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	state := routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), Context: []provider.ContextRecord{}}
	if name == "jev" || name == "free" {
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
			add(provider.ContextRecord{ID: reminder.ID, Kind: "reminder", Title: reminder.Title, Status: reminder.Status, DueAt: reminder.ScheduledAt, UpdatedAt: reminder.UpdatedAt})
		}
		for _, note := range notes {
			add(provider.ContextRecord{ID: note.ID, Kind: "note", Title: lookupExcerpt(note.Body), Body: lookupExcerpt(note.Body), UpdatedAt: note.UpdatedAt})
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
	if name == "free" {
		planned, err := service.free.Plan(ctx, state)
		if err != nil {
			return routing.Result{}, err
		}
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
	result.RoutingToken = service.sign(result)
	return result, nil
}

func routingFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRoutingProvider):
		apiError(w, 404, "routing_provider_not_found", err.Error(), false)
	case errors.Is(err, routing.ErrRequest):
		apiError(w, 400, "invalid_routing_request", err.Error(), false)
	case errors.Is(err, errRoutingConfiguration):
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
		apiError(w, 422, "routing_evaluation_rejected", err.Error(), false)
	default:
		failure(w, err)
	}
}
