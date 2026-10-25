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
	if w.Code != 200 {
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

func TestDesktopConversationPage(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	for _, path := range []string{"/desktop", "/desktop?surface=omarchy", "/desktop.css", "/product.css", "/report.js", "/voice-client-core.js", "/voice-speech.js", "/voice-client.js", "/desktop-test", "/desktop-scroll-test", "/speech-test", "/speech-test.js", "/response-test", "/response-test.js", "/report-test", "/report-test.js", "/sync-test", "/sync-test.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: HTTP %d", path, w.Code)
		}
		if path == "/desktop" && (!strings.Contains(w.Body.String(), "voice-confirm") || !strings.Contains(w.Body.String(), "voice-start-form")) {
			t.Fatal("desktop page is missing conversation controls")
		}
		if path == "/desktop" && (!strings.Contains(w.Body.String(), "voice-report") || !strings.Contains(w.Body.String(), "/report.js")) {
			t.Fatal("desktop page is missing reporting controls")
		}
		if (path == "/desktop" || path == "/desktop?surface=omarchy") && (!strings.Contains(w.Body.String(), "voice-stop-speaking") || !strings.Contains(w.Body.String(), "/voice-speech.js")) {
			t.Fatal("desktop or Omarchy page is missing speech interruption controls")
		}
		if path == "/speech-test" && (!strings.Contains(w.Body.String(), "speech-play") || !strings.Contains(w.Body.String(), "speech-stop")) {
			t.Fatal("speech testing page is missing playback controls")
		}
		if path == "/response-test" && (!strings.Contains(w.Body.String(), "response-fixture") || !strings.Contains(w.Body.String(), "response-candidate") || !strings.Contains(w.Body.String(), "response-false-claim")) {
			t.Fatal("response testing page is missing fixture controls")
		}
		if path == "/report-test" && (!strings.Contains(w.Body.String(), "report-fixture") || !strings.Contains(w.Body.String(), "report-open")) {
			t.Fatal("report testing page is missing fixture controls")
		}
		if path == "/sync-test" && (!strings.Contains(w.Body.String(), "sync-check") || !strings.Contains(w.Body.String(), "/sync-test.js")) {
			t.Fatal("shared server testing page is missing connection controls")
		}
	}
}

func TestRecordsRevisionShowsAnotherClientTaskImmediately(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	revision := func() int64 {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/records/revision", nil))
		var state struct {
			Revision int64 `json:"revision"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
			t.Fatalf("revision: HTTP %d %s", w.Code, w.Body.String())
		}
		return state.Revision
	}
	before := revision()
	phone := httptest.NewRecorder()
	h.ServeHTTP(phone, httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(`{"title":"Added on phone"}`)))
	if phone.Code != 201 {
		t.Fatal(phone.Code, phone.Body.String())
	}
	if after := revision(); after <= before {
		t.Fatalf("record revision did not advance: %d -> %d", before, after)
	}
	laptop := httptest.NewRecorder()
	h.ServeHTTP(laptop, httptest.NewRequest("GET", "/api/v1/tasks", nil))
	if laptop.Code != 200 || !strings.Contains(laptop.Body.String(), "Added on phone") {
		t.Fatal("second client did not see the committed task", laptop.Code, laptop.Body.String())
	}
	for _, item := range []struct{ path, body string }{
		{"/api/v1/notes", `{"body":"Added on laptop"}`},
		{"/api/v1/reminders", `{"title":"Follow up","scheduled_at":"2030-01-01T09:00:00Z","timezone":"UTC"}`},
	} {
		previous := revision()
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest("POST", item.path, strings.NewReader(item.body)))
		if response.Code != 201 || revision() <= previous {
			t.Fatalf("%s did not advance records revision: HTTP %d %s", item.path, response.Code, response.Body.String())
		}
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
	if w = request("DELETE", url, ""); w.Code != 200 {
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

func TestDeadlineAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	for _, body := range []string{`{"title":"Task","due_at":"2026-10-01"}`, `{"title":"Task","due_at":"bad"}`, `{"title":"Task","details":123}`} {
		if w := request("POST", "/api/v1/tasks", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request("POST", "/api/v1/tasks", `{"title":"Task","details":"Context","due_at":"2026-10-01T18:00:00+05:30"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task store.Task
	if e = json.Unmarshal(w.Body.Bytes(), &task); e != nil {
		t.Fatal(e)
	}
	if task.Details != "Context" || task.DueAt != "2026-10-01T12:30:00.000000000Z" {
		t.Fatal(task)
	}
	w = request("PATCH", "/api/v1/tasks/"+task.ID, `{"due_at":"","details":""}`)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var action store.TaskAction
	json.Unmarshal(w.Body.Bytes(), &action)
	task = *action.Task
	if task.DueAt != "" || task.Details != "" {
		t.Fatal(task)
	}
}
