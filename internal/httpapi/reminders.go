package httpapi

import (
	"atlas/internal/store"
	"net/http"
)

func reminderRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("DELETE /api/v1/reminders/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.DeleteReminder(r.Context(), r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("PATCH /api/v1/reminders/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			TaskID *string `json:"task_id"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.TaskID == nil {
			apiError(w, 400, "invalid_reminder_link", "Provide task_id; an empty string removes the task link.", false)
			return
		}
		v, e := s.SetReminderTask(r.Context(), r.PathValue("id"), *input.TaskID)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/occurrences/{occurrence}/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Action string `json:"action"`
		}
		if !decode(w, r, &input) {
			return
		}
		v, e := s.FinishOccurrence(r.Context(), r.PathValue("id"), r.PathValue("occurrence"), input.Action)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/reminders/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.ReminderState(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
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
			Repeat      string `json:"repeat"`
			TaskID      string `json:"task_id"`
			ScheduledAt string `json:"scheduled_at"`
			Timezone    string `json:"timezone"`
		}
		if !decode(w, r, &input) {
			return
		}
		key, ok := creationKey(w, r)
		if !ok {
			return
		}
		v, replay, err := s.CreateRepeatingReminderRequest(r.Context(), key, input.Title, input.ScheduledAt, input.Timezone, input.TaskID, input.Repeat)
		if err != nil {
			failure(w, err)
			return
		}
		creationResponse(w, key, replay, "/api/v1/reminders/"+v.ID, v)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/snooze", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ScheduledAt string `json:"scheduled_at"`
		}
		if !decode(w, r, &input) {
			return
		}
		v, err := s.ReminderMutation(r.Context(), r.PathValue("id"), "scheduled", input.ScheduledAt)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if !emptyAction(w, r) {
			return
		}
		v, err := s.ReminderMutation(r.Context(), r.PathValue("id"), "completed", "")
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/reminders/{id}/dismiss", func(w http.ResponseWriter, r *http.Request) {
		if !emptyAction(w, r) {
			return
		}
		v, err := s.ReminderMutation(r.Context(), r.PathValue("id"), "dismissed", "")
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
}
