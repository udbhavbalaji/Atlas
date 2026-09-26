package httpapi

import (
	"atlas/internal/store"
	"embed"
	"encoding/json"
	"net/http"
)

//go:embed web/*
var web embed.FS

func Handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	reminderRoutes(mux, s)
	for path, file := range map[string]string{"/": "index.html", "/app.js": "app.js", "/api-test.js": "api-test.js", "/openapi.json": "openapi.json"} {
		pattern := "GET " + path
		if path == "/" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			b, e := web.ReadFile("web/" + file)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			content := "text/javascript; charset=utf-8"
			if file == "index.html" {
				content = "text/html; charset=utf-8"
			}
			if file == "openapi.json" {
				content = "application/json"
			}
			w.Header().Set("Content-Type", content)
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Tasks(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.TaskState(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Title   string `json:"title"`
			Details string `json:"details"`
			DueAt   string `json:"due_at"`
		}
		if !decode(w, r, &input) {
			return
		}
		key, ok := creationKey(w, r)
		if !ok {
			return
		}
		v, replay, e := s.CreateTaskRequest(r.Context(), key, input.Title, input.Details, input.DueAt)
		if e != nil {
			failure(w, e)
			return
		}
		creationResponse(w, key, replay, "/api/v1/tasks/"+v.ID, v)
	})
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input store.TaskPatch
		if !decode(w, r, &input) {
			return
		}
		v, e := s.PatchTaskState(r.Context(), r.PathValue("id"), input)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/tasks/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if !emptyAction(w, r) {
			return
		}
		v, e := s.CompleteTaskState(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !emptyAction(w, r) {
			return
		}
		v, e := s.DeleteTaskState(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/activity", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Activity(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	for path, methods := range map[string]string{"/api/v1/tasks": "GET, HEAD, POST", "/api/v1/tasks/{id}": "GET, HEAD, PATCH, DELETE", "/api/v1/tasks/{id}/complete": "POST", "/api/v1/activity": "GET, HEAD", "/api/v1/reminders": "GET, HEAD, POST", "/api/v1/reminders/{id}": "GET, HEAD", "/api/v1/deliveries": "GET, HEAD", "/api/v1/reminders/{id}/snooze": "POST", "/api/v1/reminders/{id}/dismiss": "POST", "/api/v1/reminders/{id}/complete": "POST"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", methods)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, 404, "endpoint_not_found", "API endpoint not found.", false)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Atlas-API-Contract", "2")
		}
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
