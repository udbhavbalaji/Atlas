package httpapi

import (
	"atlas/internal/store"
	"net/http"
)

func captureRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("POST /api/v1/capture/preview", func(w http.ResponseWriter, r *http.Request) {
		var input store.CaptureInput
		if !decode(w, r, &input) {
			return
		}
		if input.PreviewID != "" {
			failure(w, store.ErrCapturePreview)
			return
		}
		proposal, err := store.PreviewCapture(input)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, proposal)
	})
	mux.HandleFunc("POST /api/v1/capture/commit", func(w http.ResponseWriter, r *http.Request) {
		var input store.CaptureInput
		if !decode(w, r, &input) {
			return
		}
		key, ok := creationKey(w, r)
		if !ok {
			return
		}
		result, replay, err := s.CommitCapture(r.Context(), key, input)
		if err != nil {
			failure(w, err)
			return
		}
		creationResponse(w, key, replay, "/api/v1/tasks/"+result.TaskID, result)
	})
	for _, path := range []string{"/api/v1/capture/preview", "/api/v1/capture/commit"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "POST")
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
