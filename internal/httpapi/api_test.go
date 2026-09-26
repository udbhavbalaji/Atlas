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

func TestTaskLifecycleAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atlas.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(s)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := request("POST", "/api/v1/tasks", `{"title":"Original"}`)
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	var task store.Task
	json.Unmarshal(w.Body.Bytes(), &task)
	url := "/api/v1/tasks/" + task.ID
	for _, body := range []string{`{}`, `{"status":"invalid"}`, `{"title":" "}`, `{"title":"x","extra":1}`, `null`} {
		if w = request("PATCH", url, body); w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	w = request("PATCH", url, `{"title":"Edited","status":"completed"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.Close()
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h = Handler(s)
	w = request("GET", "/api/v1/tasks", "")
	if !strings.Contains(w.Body.String(), `"Edited"`) || !strings.Contains(w.Body.String(), `"completed"`) {
		t.Fatal(w.Body.String())
	}
	if w = request("PATCH", url, `{"status":"open"}`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request("DELETE", url, ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w = request("DELETE", url, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = request("GET", "/api/v1/tasks", "")
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
	w = request("GET", "/api/v1/activity", "")
	if !strings.Contains(w.Body.String(), "task.deleted") {
		t.Fatal(w.Body.String())
	}
	w = request("GET", "/", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Add task") {
		t.Fatal("missing task interface")
	}
}
