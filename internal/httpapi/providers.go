package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/store"
	"errors"
	"net/http"
	"time"
)

func providerRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/providers", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"contract_version": provider.Version, "providers": []map[string]any{{"id": "mock", "available": true, "mock": true}, {"id": "jev", "available": false, "mock": false, "reason": "Adapter not connected; API documentation and access pending."}}, "context_budget": map[string]int{"rounds": 1, "queries": 1, "records": 5}, "persists_on_preview": false})
	})
	mux.HandleFunc("POST /api/v1/providers/mock/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Version      string           `json:"version"`
			RequestID    string           `json:"request_id"`
			Text         string           `json:"text"`
			Timezone     string           `json:"timezone"`
			Scenario     string           `json:"scenario"`
			ContextQuery string           `json:"context_query"`
			Answers      provider.Answers `json:"answers"`
		}
		if !decode(w, r, &input) {
			return
		}
		request := provider.Request{Version: input.Version, RequestID: input.RequestID, Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), Scenario: input.Scenario, ContextQuery: input.ContextQuery, Answers: input.Answers}
		result, err := provider.Run(r.Context(), s, provider.Mock{}, request)
		if err != nil {
			known := errors.Is(err, provider.ErrRequest) || errors.Is(err, provider.ErrContract) || errors.Is(err, provider.ErrUnavailable)
			for _, validation := range []error{store.ErrInvalid, store.ErrInvalidFields, store.ErrInvalidReminder, store.ErrInvalidRepeat, store.ErrInvalidNote, store.ErrCaptureKind, store.ErrCaptureReminder, store.ErrDependency, store.ErrNotFound} {
				known = known || errors.Is(err, validation)
			}
			if !known {
				failure(w, err)
				return
			}
			status, code := 422, "provider_proposal_rejected"
			if errors.Is(err, provider.ErrRequest) {
				status, code = 400, "invalid_provider_request"
			}
			if errors.Is(err, provider.ErrUnavailable) {
				status, code = 503, "provider_unavailable"
			}
			respond(w, status, map[string]any{"error": ErrorDetail{code, err.Error(), status == 503}, "result": result})
			return
		}
		respond(w, 200, result)
	})
	for path, allow := range map[string]string{"/api/v1/providers": "GET, HEAD", "/api/v1/providers/mock/preview": "POST"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
