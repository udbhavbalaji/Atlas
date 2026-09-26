package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	for _, body := range []string{`{"title":""}`, `{"title":"x","unknown":true}`, `{"title":"x"} {}`, `null`, `{`} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(`{"title":"Buy milk"}`)))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task store.Task
	if e = json.Unmarshal(w.Body.Bytes(), &task); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks/"+task.ID+"/complete", nil))
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/tasks", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"completed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks/missing/complete", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
