package httpapi

import (
	"atlas/internal/store"
	"net/http"
	"net/url"
	"strconv"
)

func searchRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		query, e := parseSearchQuery(r)
		if e != nil {
			failure(w, e)
			return
		}
		v, e := s.Search(r.Context(), query)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
	})
}
func parseSearchQuery(r *http.Request) (store.SearchOptions, error) {
	o := store.SearchOptions{Limit: 20}
	// ParseQuery reports malformed encoding instead of silently dropping parameters.
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return o, store.ErrInvalidSearch
	}
	for name, list := range values {
		if len(list) != 1 || (name != "q" && name != "type" && name != "status" && name != "limit" && name != "offset") {
			return o, store.ErrInvalidSearch
		}
	}
	o.Query = values.Get("q")
	o.Type = values.Get("type")
	o.Status = values.Get("status")
	if v, ok := values["limit"]; ok {
		o.Limit, e = strconv.Atoi(v[0])
		if e != nil {
			return o, store.ErrInvalidSearch
		}
	}
	if v, ok := values["offset"]; ok {
		o.Offset, e = strconv.Atoi(v[0])
		if e != nil {
			return o, store.ErrInvalidSearch
		}
	}
	return o, nil
}
