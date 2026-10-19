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
	noteRoutes(mux, s)
	searchRoutes(mux, s)
	captureRoutes(mux, s)
	sessionRoutes(mux, s)
	providerRoutes(mux, s)
	routingRoutes(mux, s)
	reportRoutes(mux, s)
	dependencyRoutes(mux, s)
	relationRoutes(mux, s)
	audioStreamRoutes(mux)
	planningRoutes(mux)
	for path, file := range map[string]string{"/": "index.html", "/desktop": "desktop.html", "/desktop-test": "desktop-test.html", "/report-test": "report-test.html", "/report-test.js": "report-test.js", "/report.js": "report.js", "/sync-test": "sync-test.html", "/sync-test.js": "sync-test.js", "/product.css": "product.css", "/batch-test": "batch-test.html", "/batch-edit-test": "batch-edit-test.html", "/relation-list-test": "relation-list-test.html", "/occurrence-test": "occurrence-test.html", "/conversation-gap-test.js": "conversation-gap-test.js", "/conversation-gap-test.css": "conversation-gap-test.css", "/workflow-test": "workflow-test.html", "/association-test": "association-test.html", "/planning-test": "planning-test.html", "/planning-test.js": "planning-test.js", "/app.js": "app.js", "/styles.css": "styles.css", "/desktop.css": "desktop.css", "/desktop-audio.js": "desktop-audio.js", "/navigation.js": "navigation.js", "/notes.js": "notes.js", "/search.js": "search.js", "/capture.js": "capture.js", "/sessions.js": "sessions.js", "/providers.js": "providers.js", "/routing.js": "routing.js", "/routing-conversation.js": "routing-conversation.js", "/voice-client-core.js": "voice-client-core.js", "/voice-client.js": "voice-client.js", "/dependencies.js": "dependencies.js", "/relations.js": "relations.js", "/api-test.js": "api-test.js", "/openapi.json": "openapi.json"} {
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
			if file == "index.html" || file == "desktop.html" || file == "desktop-test.html" || file == "report-test.html" || file == "sync-test.html" || file == "batch-test.html" || file == "batch-edit-test.html" || file == "relation-list-test.html" || file == "occurrence-test.html" || file == "workflow-test.html" || file == "association-test.html" || file == "planning-test.html" {
				content = "text/html; charset=utf-8"
			}
			if file == "styles.css" || file == "desktop.css" || file == "product.css" || file == "conversation-gap-test.css" {
				content = "text/css; charset=utf-8"
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
	for path, methods := range map[string]string{"/api/v1/reminders/{id}/occurrences/{occurrence}/acknowledge": "POST", "/api/v1/tasks": "GET, HEAD, POST", "/api/v1/tasks/{id}": "GET, HEAD, PATCH, DELETE", "/api/v1/tasks/{id}/complete": "POST", "/api/v1/activity": "GET, HEAD", "/api/v1/reminders": "GET, HEAD, POST", "/api/v1/reminders/{id}": "GET, HEAD, PATCH, DELETE", "/api/v1/deliveries": "GET, HEAD", "/api/v1/reminders/{id}/snooze": "POST", "/api/v1/reminders/{id}/dismiss": "POST", "/api/v1/reminders/{id}/complete": "POST"} {
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
