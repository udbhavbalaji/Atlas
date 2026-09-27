package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/store"
	"net/http"
	"time"
)

func captureRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("POST /api/v1/capture/interpret", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Text     string `json:"text"`
			Timezone string `json:"timezone"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := interpret.Interpret(input.Text, input.Timezone, time.Now())
		if err != nil {
			apiError(w, 400, "invalid_interpretation", err.Error(), false)
			return
		}
		respond(w, 200, result)
	})
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
		location := "/api/v1/tasks/" + result.TaskID
		if result.TaskID == "" {
			if len(result.Reminders) > 0 {
				location = "/api/v1/reminders/" + result.Reminders[0].ID
			} else {
				location = "/api/v1/notes/" + result.Notes[0].ID
			}
		}
		creationResponse(w, key, replay, location, result)
	})
	for _, path := range []string{"/api/v1/capture/interpret", "/api/v1/capture/preview", "/api/v1/capture/commit"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "POST")
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
