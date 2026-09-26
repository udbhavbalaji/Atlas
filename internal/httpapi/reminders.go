package httpapi

import (
	"atlas/internal/store"
	"net/http"
)

func reminderRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/reminders", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Reminders(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/deliveries", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Deliveries(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/reminders", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Title       string `json:"title"`
			ScheduledAt string `json:"scheduled_at"`
			Timezone    string `json:"timezone"`
		}
		if !decode(w, r, &input) {
			return
		}
		v, err := s.CreateReminder(r.Context(), input.Title, input.ScheduledAt, input.Timezone)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 201, v)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/snooze", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ScheduledAt string `json:"scheduled_at"`
		}
		if !decode(w, r, &input) {
			return
		}
		if err := s.SnoozeReminder(r.Context(), r.PathValue("id"), input.ScheduledAt); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if err := s.CompleteReminder(r.Context(), r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/dismiss", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DismissReminder(r.Context(), r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
}
