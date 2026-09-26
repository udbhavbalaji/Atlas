package httpapi

import (
	"atlas/internal/store"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIIdempotencyAndStructuredActions(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	req := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := req("POST", "/api/v1/tasks", `{"title":"Task"}`, "one-key")
	if w.Code != 201 || w.Header().Get("Idempotency-Replayed") != "false" {
		t.Fatal(w.Code, w.Body.String())
	}
	first := w.Body.String()
	var task store.Task
	json.Unmarshal(w.Body.Bytes(), &task)
	w = req("POST", "/api/v1/tasks", `{ "title": "Task" }`, "one-key")
	if w.Code != 200 || w.Body.String() != first || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = req("POST", "/api/v1/tasks", `{"title":"Other"}`, "one-key")
	assertErrorCode(t, w, 409, "idempotency_conflict")
	w = req("POST", "/api/v1/tasks/"+task.ID+"/complete", "", "")
	var state store.TaskAction
	if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 200 || state.Task.Status != "completed" || state.Reminders == nil || state.Deliveries == nil {
		t.Fatal(state, w.Code, e)
	}
	w = req("DELETE", "/api/v1/tasks/"+task.ID, "", "")
	if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 200 || state.Task != nil || !state.Deleted {
		t.Fatal(state, w.Code, e)
	}
	w = req("GET", "/api/v1/tasks/"+task.ID, "", "")
	assertErrorCode(t, w, 404, "task_not_found")
}
func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var v ErrorResponse
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || w.Code != status || v.Error.Code != code || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestStrictValidationAndRouting(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"title":"first","title":"last"}`, "duplicate_field", 400}, {`{"title":null}`, "null_field", 400}, {`{"title":123}`, "invalid_fields", 400}, {`{"title":"x","unknown":true}`, "invalid_fields", 400}, {`null`, "invalid_json", 400}, {`{"title":"x"} {}`, "invalid_json", 400}, {strings.Repeat("x", 65537), "body_too_large", 413}, {"{\"title\":\"\xff\"}", "invalid_json", 400},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(tc.body)))
		assertErrorCode(t, w, tc.status, tc.code)
	}
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{{"PUT", "/api/v1/tasks", "method_not_allowed", 405}, {"GET", "/api/v1/nope", "endpoint_not_found", 404}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		assertErrorCode(t, w, tc.status, tc.code)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader(`{"title":"x"}`))
	r.Header.Set("Idempotency-Key", "invalid key")
	h.ServeHTTP(w, r)
	assertErrorCode(t, w, 400, "invalid_idempotency_key")
}

func TestReminderWireStateAndSchemas(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if path == "/api/v1/reminders" {
			r.Header.Set("Idempotency-Key", "reminder-wire")
		}
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"title":"Wire reminder","scheduled_at":"2030-01-01T12:00:00Z","timezone":"UTC"}`
	first := request("POST", "/api/v1/reminders", body)
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}
	replay := request("POST", "/api/v1/reminders", body)
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatal(replay.Body.String())
	}
	var reminder store.Reminder
	if err = json.Unmarshal(first.Body.Bytes(), &reminder); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/reminders/" + reminder.ID + "/complete"
	invalid := request("POST", path, `{"unexpected":true}`)
	assertErrorCode(t, invalid, 400, "invalid_fields")
	complete := request("POST", path, `{}`)
	var state store.ReminderAction
	if err = json.Unmarshal(complete.Body.Bytes(), &state); err != nil || complete.Code != 200 || state.Reminder.Status != "completed" || state.Task != nil || len(state.Deliveries) != 1 || state.Deliveries[0].State != "cancelled" {
		t.Fatal(complete.Code, complete.Body.String(), err)
	}
	if complete.Header().Get("Atlas-API-Contract") != "2" {
		t.Fatal("missing contract revision")
	}
	schema := request("GET", "/openapi.json", "")
	var document map[string]any
	if err = json.Unmarshal(schema.Body.Bytes(), &document); err != nil || schema.Code != 200 || document["openapi"] != "3.1.0" {
		t.Fatal(schema.Code, err)
	}
	var checkRefs func(any)
	checkRefs = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				var target any = document
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					object, ok := target.(map[string]any)
					if !ok {
						t.Fatalf("bad schema ref %s", ref)
					}
					target, ok = object[part]
					if !ok {
						t.Fatalf("missing schema ref %s", ref)
					}
				}
			}
			for _, child := range v {
				checkRefs(child)
			}
		case []any:
			for _, child := range v {
				checkRefs(child)
			}
		}
	}
	checkRefs(document)
}

func TestRecurringReminderAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	request := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
	w := request("/api/v1/reminders", `{"title":"Repeat","scheduled_at":"2020-01-01T09:00:00Z","timezone":"UTC","repeat":"monthly"}`)
	assertErrorCode(t, w, 400, "invalid_repeat")
	w = request("/api/v1/reminders", `{"title":"Repeat","scheduled_at":"2020-01-01T09:00:00Z","timezone":"UTC","repeat":"daily"}`)
	var r store.Reminder
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil || w.Code != 201 || r.Repeat != "daily" || r.OccurrenceID == "" {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	if e = s.ProcessDue(context.Background(), time.Now()); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/reminders/" + r.ID + "/occurrences/" + r.OccurrenceID + "/acknowledge"
	w = request(path, `{"action":"complete"}`)
	var state store.ReminderAction
	if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 200 || state.Reminder.Status != "scheduled" || state.Reminder.OccurrenceID == r.OccurrenceID || len(state.Deliveries) != 2 {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	next := state.Reminder.OccurrenceID
	w = request(path, `{"action":"complete"}`)
	if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 200 || state.Reminder.OccurrenceID != next || len(state.Deliveries) != 2 {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	w = request("/api/v1/reminders/"+r.ID+"/dismiss", "")
	assertErrorCode(t, w, 409, "occurrence_state_conflict")
}
