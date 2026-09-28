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
var errRoutingConfiguration = errors.New("save the key in data/openrouter.key or set OPENROUTER_API_KEY and restart Atlas; no fallback was used")

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
	default:
		return routing.Result{}, errRoutingProvider
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	state := routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), Context: []provider.ContextRecord{}}
	if strings.TrimSpace(input.ContextQuery) != "" {
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
