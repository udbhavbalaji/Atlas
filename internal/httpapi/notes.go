package httpapi

import (
	"atlas/internal/store"
	"net/http"
)

func noteRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/notes", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Notes(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.NoteState(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST /api/v1/notes", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body       string `json:"body"`
			TaskID     string `json:"task_id"`
			ReminderID string `json:"reminder_id"`
		}
		if !decode(w, r, &input) {
			return
		}
		key, ok := creationKey(w, r)
		if !ok {
			return
		}
		v, replay, e := s.CreateNoteRequest(r.Context(), key, input.Body, input.TaskID, input.ReminderID)
		if e != nil {
			failure(w, e)
			return
		}
		creationResponse(w, key, replay, "/api/v1/notes/"+v.NoteID, v)
	})
	mux.HandleFunc("PATCH /api/v1/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body  *string `json:"body"`
			Title *string `json:"title"`
		}
		if !decode(w, r, &input) {
			return
		}
		if (input.Body == nil) == (input.Title == nil) {
			apiError(w, 400, "invalid_note", "Provide either title or body.", false)
			return
		}
		var v store.NoteAction
		var e error
		if input.Title != nil {
			v, e = s.RenameNote(r.Context(), r.PathValue("id"), *input.Title)
		} else {
			v, e = s.UpdateNote(r.Context(), r.PathValue("id"), *input.Body)
		}
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("DELETE /api/v1/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !emptyAction(w, r) {
			return
		}
		v, e := s.DeleteNote(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/notes/{id}/links/{kind}/{target}", func(w http.ResponseWriter, r *http.Request) {
			if !emptyAction(w, r) {
				return
			}
			v, e := s.SetNoteLink(r.Context(), r.PathValue("id"), r.PathValue("kind"), r.PathValue("target"), r.Method == "PUT")
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 200, v)
		})
	}
	for path, allow := range map[string]string{"/api/v1/notes": "GET, HEAD, POST", "/api/v1/notes/{id}": "GET, HEAD, PATCH, DELETE", "/api/v1/notes/{id}/links/{kind}/{target}": "PUT, DELETE"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
