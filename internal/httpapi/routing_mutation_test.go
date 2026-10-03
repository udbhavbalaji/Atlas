package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type editDecisionFixture struct {
	calls   int
	context []routing.State
}

func (f *editDecisionFixture) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	f.calls++
	f.context = append(f.context, state)
	choice := "edit"
	if strings.HasPrefix(strings.ToLower(state.Text), "delete") {
		choice = "delete"
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, choice)
}

func TestConversationEditReportsChangeAndDeleteRequiresConfirmation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	newTime := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	task, err := s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", old)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &editDecisionFixture{}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: fixture, configured: true, secret: "test"})
	send := func(method, path string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		return w.Code, out
	}
	code, c := send("POST", "/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "edit-test", "text": "Reschedule my interview to " + newTime, "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "from") || !strings.Contains(c["prompt"].(string), "to") {
		t.Fatal(code, c)
	}
	if fixture.calls != 1 || len(fixture.context[0].Context) == 0 || fixture.context[0].Context[0].Title != "Interview with Maya" {
		t.Fatal("Jev lacked record context", fixture)
	}
	after, err := s.TaskState(t.Context(), task.ID)
	gotDue, parseErr := time.Parse(time.RFC3339Nano, after.Task.DueAt)
	wantDue, _ := time.Parse(time.RFC3339Nano, newTime)
	if err != nil || parseErr != nil || !gotDue.Equal(wantDue) {
		t.Fatal(after.Task.DueAt, newTime, err, parseErr)
	}
	code, c = send("POST", "/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "delete-test", "text": "Delete my interview", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("deleted before confirmation", err)
	}
	id := c["id"].(string)
	code, c = send("POST", "/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Deleted task") {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err == nil {
		t.Fatal("task survived confirmed deletion")
	}
}

func TestConversationSelectsAmbiguousTargetAndEditsNote(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.Create(t.Context(), "Interview with Alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(t.Context(), "Interview with Bob"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := func(path string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "ambiguous-edit", "text": "Rename my interview to Follow up", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_target" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": a.ID})
	if code != 200 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Interview with Alice") || !strings.Contains(c["prompt"].(string), "Follow up") {
		t.Fatal(code, c)
	}
	task, err := s.TaskState(t.Context(), a.ID)
	if err != nil || task.Task.Title != "Follow up" {
		t.Fatal(task, err)
	}
	note, _, err := s.CreateNoteRequest(t.Context(), "", "Apollo agenda", "", "")
	if err != nil {
		t.Fatal(err)
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "note-edit", "text": "Change my note about Apollo to Apollo agenda revised", "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "from “Apollo agenda” to “Apollo agenda revised”") {
		t.Fatal(code, c)
	}
	updated, err := s.NoteState(t.Context(), note.NoteID)
	if err != nil || updated.Note.Body != "Apollo agenda revised" {
		t.Fatal(updated, err)
	}
}

func TestConversationReminderRescheduleAndNoteDeletion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	newTime := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	reminder, err := s.CreateReminder(t.Context(), "Submit portfolio", old, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	note, _, err := s.CreateNoteRequest(t.Context(), "", "Orion project notes", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := func(path string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "reminder-edit", "text": "Reschedule my reminder Submit portfolio to " + newTime, "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "scheduled at from") {
		t.Fatal(code, c)
	}
	updated, err := s.ReminderState(t.Context(), reminder.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := time.Parse(time.RFC3339Nano, updated.Reminder.ScheduledAt)
	want, _ := time.Parse(time.RFC3339Nano, newTime)
	if !got.Equal(want) {
		t.Fatal(updated.Reminder.ScheduledAt, newTime)
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "delete", "version": "1", "request_id": "reminder-delete", "text": "Delete my reminder Submit portfolio", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" {
		t.Fatal(code, c)
	}
	if _, err = s.ReminderState(t.Context(), reminder.ID); err == nil {
		t.Fatal("reminder survived")
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "delete", "version": "1", "request_id": "note-delete", "text": "Delete my note about Orion", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	if _, err = s.NoteState(t.Context(), note.NoteID); err != nil {
		t.Fatal("note deleted early", err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" {
		t.Fatal(code, c)
	}
	if _, err = s.NoteState(t.Context(), note.NoteID); err == nil {
		t.Fatal("note survived")
	}
}

func TestConversationReminderRescheduleReportsAtomicLinkedEffects(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	oldReminder := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	oldDue := oldReminder.Add(time.Hour)
	newReminder := oldReminder.Add(2 * time.Hour)
	task, err := s.CreateWithFields(t.Context(), "Interview schedule group", "", oldDue.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	primary, err := s.CreateLinkedReminder(t.Context(), "Primary interview reminder", oldReminder.Format(time.RFC3339), "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.CreateLinkedReminder(t.Context(), "Portfolio interview reminder", oldReminder.Add(-24*time.Hour).Format(time.RFC3339), "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	data, _ := json.Marshal(map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "linked-schedule-edit", "text": "Reschedule Primary interview reminder to " + newReminder.Format(time.RFC3339), "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/conversations/routing", bytes.NewReader(data)))
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	prompt, _ := response["prompt"].(string)
	if w.Code != 201 || response["state"] != "answered" || !strings.Contains(prompt, "Linked task") || !strings.Contains(prompt, "Portfolio interview reminder") {
		t.Fatal(w.Code, response)
	}
	state, err := s.TaskState(t.Context(), task.ID)
	if err != nil || state.Task.DueAt != oldDue.Add(2*time.Hour).Format("2006-01-02T15:04:05.000000000Z") {
		t.Fatal(state, err)
	}
	byID := map[string]store.Reminder{}
	for _, reminder := range state.Reminders {
		byID[reminder.ID] = reminder
	}
	if byID[primary.ID].ScheduledAt != newReminder.Format("2006-01-02T15:04:05.000000000Z") || byID[sibling.ID].ScheduledAt != oldReminder.Add(-22*time.Hour).Format("2006-01-02T15:04:05.000000000Z") {
		t.Fatal(byID)
	}
}

func TestConversationGenericEditAsksFieldAndReportsLinkedReminderEffect(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Interview with Maya")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	if _, err = s.CreateLinkedReminder(t.Context(), "Prepare for interview", when, "UTC", task.ID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := func(path string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "generic-edit", "text": "Edit my interview", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_field" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": "details"})
	if code != 200 || c["state"] != "awaiting_change" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": "Bring portfolio"})
	if code != 200 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "details from unset to “Bring portfolio”") {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": "complete-edit", "text": "Complete my interview", "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Linked reminder") {
		t.Fatal(code, c)
	}
	current, err := s.TaskState(t.Context(), task.ID)
	if err != nil || current.Task.Status != "completed" || len(current.Reminders) != 1 || current.Reminders[0].Status != "completed" {
		t.Fatal(current, err)
	}
}
