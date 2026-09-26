package httpapi

import (
	"atlas/internal/store"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

//go:embed web/*
var web embed.FS

func Handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	reminderRoutes(mux, s)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := web.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := web.ReadFile("web/app.js")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input store.TaskPatch
		if !decode(w, r, &input) {
			return
		}
		t, err := s.Patch(r.Context(), r.PathValue("id"), input)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, t)
	})
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.Context(), r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Tasks(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		var input struct {
			Title   string `json:"title"`
			Details string `json:"details"`
			DueAt   string `json:"due_at"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil {
			respond(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		if err := d.Decode(new(any)); err != io.EOF {
			respond(w, 400, map[string]string{"error": "expected one JSON object"})
			return
		}
		t, e := s.CreateWithFields(r.Context(), input.Title, input.Details, input.DueAt)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 201, t)
	})
	mux.HandleFunc("POST /api/v1/tasks/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Complete(r.Context(), r.PathValue("id")); e != nil {
			failure(w, e)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/v1/activity", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Activity(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	return mux
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	status := 500
	message := "internal server error"
	if errors.Is(e, store.ErrInvalid) || errors.Is(e, store.ErrInvalidUpdate) || errors.Is(e, store.ErrInvalidFields) || errors.Is(e, store.ErrInvalidReminder) || errors.Is(e, store.ErrSnoozeTime) {
		status = 400
		message = e.Error()
	}
	if errors.Is(e, store.ErrNotFound) || errors.Is(e, store.ErrReminderNotFound) {
		status = 404
		message = e.Error()
	}
	if errors.Is(e, store.ErrReminderConflict) {
		status = 409
		message = e.Error()
	}
	if status == 500 {
		log.Printf("storage operation failed: %v", e)
	}
	respond(w, status, map[string]string{"error": message})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request body"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		respond(w, 400, map[string]string{"error": "expected one JSON object"})
		return false
	}
	return true
}
