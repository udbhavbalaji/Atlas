package httpapi

import (
	"atlas/internal/store"
	"net/http"
	"net/url"
)

func relationRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/relations", func(w http.ResponseWriter, r *http.Request) {
		q, parseErr := url.ParseQuery(r.URL.RawQuery)
		if parseErr != nil || (len(q) > 0 && (q.Get("record_type") == "" || q.Get("record_id") == "")) {
			apiError(w, 400, "invalid_relation_query", "Use a valid record_type and record_id together, or neither.", false)
			return
		}
		for key, values := range q {
			if (key != "record_type" && key != "record_id") || len(values) != 1 {
				apiError(w, 400, "invalid_relation_query", "Use a single record_type and record_id, or neither.", false)
				return
			}
		}
		v, e := s.Relations(r.Context(), q.Get("record_type"), q.Get("record_id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/relations/{from_type}/{from_id}/{to_type}/{to_id}", func(w http.ResponseWriter, r *http.Request) {
			if !emptyAction(w, r) {
				return
			}
			v, e := s.SetRelation(r.Context(), r.PathValue("from_type"), r.PathValue("from_id"), r.PathValue("to_type"), r.PathValue("to_id"), r.Method == "PUT")
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 200, v)
		})
	}
	for path, allow := range map[string]string{"/api/v1/relations": "GET, HEAD", "/api/v1/relations/{from_type}/{from_id}/{to_type}/{to_id}": "PUT, DELETE"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}
}
