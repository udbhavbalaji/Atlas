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

func TestReminderAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
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
	for _, body := range []string{`{}`, `null`, `{"title":"x","scheduled_at":"tomorrow","timezone":"UTC"}`, `{"title":"x","scheduled_at":"2026-10-01T12:00:00Z","timezone":"bad"}`, `{"title":"x","scheduled_at":"2026-10-01T12:00:00Z","timezone":"UTC","extra":true}`} {
		if w := request("POST", "/api/v1/reminders", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request("POST", "/api/v1/reminders", `{"title":"Test reminder","scheduled_at":"2026-01-01T12:00:00Z","timezone":"UTC"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r store.Reminder
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(context.Background(), time.Now()); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/reminders", "/api/v1/deliveries", "/api/v1/activity"} {
		if w = request("GET", path, ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	url := "/api/v1/reminders/" + r.ID
	if w = request("POST", url+"/snooze", `{"scheduled_at":"2020-01-01T12:00:00Z"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = request("POST", url+"/dismiss", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request("POST", url+"/complete", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	if w = request("POST", url+"/snooze", `{"scheduled_at":"`+future+`"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w = request("POST", "/api/v1/reminders/missing/complete", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestLinkedReminderAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
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
	w := request("POST", "/api/v1/tasks", `{"title":"Linked task"}`)
	var task store.Task
	if e = json.Unmarshal(w.Body.Bytes(), &task); e != nil || w.Code != 201 {
		t.Fatal(w.Code, e)
	}
	body := `{"title":"Linked reminder","scheduled_at":"2030-01-01T12:00:00Z","timezone":"UTC","task_id":"` + task.ID + `"}`
	w = request("POST", "/api/v1/reminders", body)
	var r store.Reminder
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil || w.Code != 201 || r.TaskID != task.ID {
		t.Fatal(w.Code, r, e)
	}
	w = request("PATCH", "/api/v1/tasks/"+task.ID, `{"status":"completed"}`)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = request("POST", "/api/v1/reminders", body)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("GET", "/api/v1/reminders", "")
	if !strings.Contains(w.Body.String(), `"cancellation_reason":"task.completed"`) {
		t.Fatal(w.Body.String())
	}
	w = request("DELETE", "/api/v1/tasks/"+task.ID, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = request("POST", "/api/v1/reminders", body)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestChangeReminderLinkHTTP(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	task, e := s.Create(context.Background(), "Task")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateReminder(context.Background(), "Reminder", "2030-01-01T09:00:00Z", "UTC")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{{`{}`, 400, "invalid_reminder_link"}, {`{"task_id":null}`, 400, "null_field"}, {`{"task_id":"missing"}`, 404, "task_not_found"}, {`{"task_id":"` + task.ID + `"}`, 200, ""}, {`{"task_id":""}`, 200, ""}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/v1/reminders/"+r.ID, strings.NewReader(tc.body)))
		if tc.code != "" {
			assertErrorCode(t, w, tc.status, tc.code)
		} else {
			var state store.ReminderAction
			if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 200 || state.Reminder.ID != r.ID {
				t.Fatal(w.Code, w.Body.String(), e)
			}
		}
	}
}
