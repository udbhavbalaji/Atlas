package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

type sessionReplyState struct {
	ID             string `json:"id"`
	Version        int    `json:"version"`
	State          string `json:"state"`
	Interpretation struct {
		Draft store.CaptureInput `json:"draft"`
	} `json:"interpretation"`
	Saved *store.TaskAction `json:"saved"`
}

func sessionRequest(t *testing.T, h http.Handler, method, path string, body any, status int) sessionReplyState {
	t.Helper()
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/capture/sessions"+path, bytes.NewReader(b)))
	if w.Code != status {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	var out sessionReplyState
	if status < 300 {
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
	}
	return out
}
func TestConversationPersistenceAndConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	h := Handler(s)
	interview, _ := s.CreateWithFields(t.Context(), "Interview at Ather", "", "2090-01-01T16:30:00Z")
	s.CreateWithFields(t.Context(), "Interview at OtherCo", "", "2090-01-02T16:30:00Z")
	c := sessionRequest(t, h, "POST", "", map[string]any{"text": "Remind me to print my resume before that", "timezone": "UTC"}, 201)
	if c.State != "awaiting_clarification" {
		t.Fatal(c)
	}
	reply := func(text string, status int) sessionReplyState {
		return sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"text": text, "version": c.Version}, status)
	}
	reply("yes", 409)
	c = reply("something unclear", 200)
	if c.State != "awaiting_clarification" {
		t.Fatal(c)
	}
	c = reply("the Ather one", 200)
	if c.State != "awaiting_confirmation" || c.Interpretation.Draft.BeforeTaskID != interview.ID || c.Interpretation.Draft.ReminderAt != "2090-01-01T15:30:00.000000000Z" {
		t.Fatal(c)
	}
	old := c.Version
	c = reply("one day before", 200)
	if c.Interpretation.Draft.ReminderAt != "2089-12-31T16:30:00.000000000Z" {
		t.Fatal(c)
	}
	sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"text": "yes", "version": old}, 409)
	s.Close()
	s, e = store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h = Handler(s)
	c = sessionRequest(t, h, "GET", "/"+c.ID, nil, 200)
	c = reply("yes", 200)
	if c.State != "saved" || c.Saved == nil || len(c.Saved.Reminders) != 1 {
		t.Fatal(c)
	}
	savedID := c.Saved.TaskID
	c = reply("yes", 200)
	if c.Saved.TaskID != savedID {
		t.Fatal(c)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 3 {
		t.Fatal(tasks)
	}
	reply("tomorrow at 9am", 409)
}
func TestConversationTimeNotesAndCancellation(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	c := sessionRequest(t, h, "POST", "", map[string]any{"text": "Remind me to call Mom", "timezone": "UTC"}, 201)
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"version": c.Version, "text": "tomorrow at 9am"}, 200)
	if c.State != "awaiting_confirmation" || c.Interpretation.Draft.Title != "call Mom" || c.Interpretation.Draft.ReminderAt == "" {
		t.Fatal(c)
	}
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"version": c.Version, "text": "cancel"}, 200)
	if c.State != "cancelled" {
		t.Fatal(c)
	}
	tasks, _ := s.Tasks(t.Context())
	reminders, _ := s.Reminders(t.Context())
	if len(tasks) != 0 || len(reminders) != 0 {
		t.Fatal(tasks, reminders)
	}
	c = sessionRequest(t, h, "POST", "", map[string]any{"text": "Remember that the gate code is 1234", "timezone": "UTC"}, 201)
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"version": c.Version, "text": "yes"}, 200)
	if c.Saved == nil || len(c.Saved.Notes) != 1 || c.Saved.TaskID != "" {
		t.Fatal(c)
	}
}
func TestConversationRecoversInterruptedConfirmation(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	c := sessionRequest(t, h, "POST", "", map[string]any{"text": "Call Mom", "timezone": "UTC"}, 201)
	d, _ := s.Session(t.Context(), c.ID)
	var data conversation
	json.Unmarshal(d.Data, &data)
	data.State = "confirming"
	d, e = s.SaveSession(t.Context(), d.ID, d.Version, data)
	if e != nil {
		t.Fatal(e)
	}
	saved, _, e := s.CommitCapture(t.Context(), "session."+d.ID, data.Interpretation.Proposal.Input)
	if e != nil {
		t.Fatal(e)
	}
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"version": d.Version, "text": "confirm"}, 200)
	if c.State != "saved" || c.Saved.TaskID != saved.TaskID {
		t.Fatal(c)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
}

func TestConversationChangedContextRequiresNewConfirmation(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	task, _ := s.CreateWithFields(t.Context(), "Interview at Ather", "", "2090-01-01T16:30:00Z")
	c := sessionRequest(t, h, "POST", "", map[string]any{"text": "Remind me to print my resume before the interview at Ather", "timezone": "UTC"}, 201)
	due := "2090-01-02T16:30:00Z"
	_, e = s.PatchTaskState(t.Context(), task.ID, store.TaskPatch{DueAt: &due})
	if e != nil {
		t.Fatal(e)
	}
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"text": "yes", "version": c.Version}, 200)
	if c.State != "awaiting_confirmation" || c.Saved != nil || c.Interpretation.Draft.ReminderAt != "2090-01-02T15:30:00.000000000Z" {
		t.Fatal(c)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	c = sessionRequest(t, h, "POST", "/"+c.ID+"/reply", map[string]any{"text": "yes", "version": c.Version}, 200)
	if c.State != "saved" {
		t.Fatal(c)
	}
}
