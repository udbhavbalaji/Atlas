package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelationAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "A")
	b, _ := s.Create(t.Context(), "B")
	h := Handler(s)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/"+path, strings.NewReader(body)))
		return w
	}
	path := "relations/task/" + a.ID + "/task/" + b.ID
	w := send("PUT", path, "{}")
	var v store.RelationAction
	if e = json.Unmarshal(w.Body.Bytes(), &v); e != nil || w.Code != 200 || v.Relation == nil || v.Relation.Type != "related_to" {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	w = send("GET", "relations?record_type=task&record_id="+a.ID, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), v.RelationID) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?unknown=true", "?record_type=task", "?record_type=task&record_type=task&record_id=" + a.ID} {
		w = send("GET", "relations"+suffix, "")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	assertErrorCode(t, send("PUT", path, `{"unknown":true}`), 400, "invalid_fields")
	assertErrorCode(t, send("POST", path, "{}"), 405, "method_not_allowed")
	w = send("DELETE", path, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send("GET", "relations", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Code, w.Body.String())
	}
}
