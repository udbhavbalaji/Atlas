package httpapi

import (
	"atlas/internal/store"
	"net/http"
)

func dependencyRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/task-dependencies", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Dependencies(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/tasks/{id}/before/{target}", func(w http.ResponseWriter, r *http.Request) {
			if !emptyAction(w, r) {
				return
			}
			v, e := s.SetDependency(r.Context(), r.PathValue("id"), r.PathValue("target"), r.Method == "PUT")
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 200, v)
		})
	}
	for path, allow := range map[string]string{"/api/v1/task-dependencies": "GET, HEAD", "/api/v1/tasks/{id}/before/{target}": "PUT, DELETE"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
