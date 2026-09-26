package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

func Handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
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
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var input struct {
			Title string `json:"title"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil {
			respond(w, 400, map[string]string{"error": "invalid task body"})
			return
		}
		if err := d.Decode(new(any)); err != io.EOF {
			respond(w, 400, map[string]string{"error": "expected one JSON object"})
			return
		}
		t, e := s.Create(r.Context(), input.Title)
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
	if errors.Is(e, store.ErrInvalid) {
		status = 400
		message = e.Error()
	}
	if errors.Is(e, store.ErrNotFound) {
		status = 404
		message = e.Error()
	}
	if status == 500 {
		log.Printf("storage operation failed: %v", e)
	}
	respond(w, status, map[string]string{"error": message})
}
