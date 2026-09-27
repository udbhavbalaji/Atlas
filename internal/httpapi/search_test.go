package httpapi

import (
	"atlas/internal/store"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSearchQueryContract(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	task, e := s.Create(context.Background(), "Find me")
	if e != nil {
		t.Fatal(e)
	}
	for _, query := range []string{"", "q=", "q=x&limit=101", "q=x&offset=-1", "q=x&limit=no", "q=x&q=y", "q=x&unknown=value", "q=x&type=note&status=completed", "q=%ZZ"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/search?"+query, nil))
		assertErrorCode(t, w, 400, "invalid_search")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/search?q=FIND&type=task&status=open&limit=1", nil))
	var result store.SearchResponse
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 || len(result.Results) != 1 || result.Results[0].ID != task.ID || result.Limit != 1 {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/search", nil))
	assertErrorCode(t, w, 405, "method_not_allowed")
}
