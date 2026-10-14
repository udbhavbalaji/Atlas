package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBatchDeleteAllRequiresApprovalAndKeepsOtherKinds(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Batch task")
	if err != nil {
		t.Fatal(err)
	}
	reminder, err := s.CreateReminder(t.Context(), "Batch reminder", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	note, _, err := s.CreateNoteRequest(t.Context(), "", "Keep this note", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, evaluated := send("/api/v1/routing/mock/evaluate", map[string]any{"version": "1", "request_id": "batch-preview", "text": "Delete all tasks and reminders", "timezone": "UTC"})
	if code != 200 || evaluated["selected_tool"] != "delete_batch" {
		t.Fatal(code, evaluated)
	}
	code, preview := send("/api/v1/routing/dispatch", map[string]any{"version": "1", "routing_token": evaluated["routing_token"]})
	if code != 200 || preview["state"] != "awaiting_delete_confirmation" || preview["persisted"] != false {
		t.Fatal(code, preview)
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-all", "text": "Delete all tasks and reminders", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" || c["channel"] != "batch_delete" {
		t.Fatal(code, c)
	}
	if len(c["batch"].(map[string]any)["targets"].([]any)) != 2 {
		t.Fatal(c["batch"])
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("task deleted before approval", err)
	}
	if _, err = s.ReminderState(t.Context(), reminder.ID); err != nil {
		t.Fatal("reminder deleted before approval", err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err == nil {
		t.Fatal("task survived")
	}
	if _, err = s.ReminderState(t.Context(), reminder.ID); err == nil {
		t.Fatal("reminder survived")
	}
	if _, err = s.NoteState(t.Context(), note.NoteID); err != nil {
		t.Fatal("unselected note deleted", err)
	}
}

func TestBatchDeleteNamedTargetsSelectionAndChangedReview(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Batch Alpha")
	b, _ := s.CreateReminder(t.Context(), "Batch Beta", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC")
	untouched, _ := s.Create(t.Context(), "Batch Gamma")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-named", "text": "Delete Batch Alpha and Batch Beta", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	if len(c["batch"].(map[string]any)["targets"].([]any)) != 2 {
		t.Fatal(c["batch"])
	}
	updated := "Batch Alpha revised"
	if _, err = s.PatchTaskState(t.Context(), a.ID, store.TaskPatch{Title: &updated}); err != nil {
		t.Fatal(err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "awaiting_batch_targets" {
		t.Fatal(code, c)
	}
	if _, err = s.ReminderState(t.Context(), b.ID); err != nil {
		t.Fatal("partial deletion on stale review", err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "field": "batch_targets", "value": a.ID + "," + b.ID})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), a.ID); err == nil {
		t.Fatal("selected task survived")
	}
	if _, err = s.ReminderState(t.Context(), b.ID); err == nil {
		t.Fatal("selected reminder survived")
	}
	if _, err = s.TaskState(t.Context(), untouched.ID); err != nil {
		t.Fatal("unselected task deleted", err)
	}
}

func TestBatchDeleteCancel(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, _ := s.Create(t.Context(), "Keep after cancel")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	_, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-cancel", "text": "Delete all tasks", "timezone": "UTC"})
	code, c := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "cancel"})
	if code != 200 || c["state"] != "cancelled" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestBatchDeletePickerRequiresSeparateConfirmation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, _ := s.Create(t.Context(), "Pick task")
	reminder, _ := s.CreateReminder(t.Context(), "Pick reminder", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-picker", "text": "Delete task and reminder", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_batch_targets" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "field": "batch_targets", "value": task.ID + "," + reminder.ID})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("selection deleted task", err)
	}
	if _, err = s.ReminderState(t.Context(), reminder.ID); err != nil {
		t.Fatal("selection deleted reminder", err)
	}
}

func TestBatchDeleteRechecksLinkedReminderBeforeTaskDeletion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Batch linked task")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	_, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-linked", "text": "Delete all tasks", "timezone": "UTC"})
	if c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(c)
	}
	reminder, err := s.CreateLinkedReminder(t.Context(), "New linked reminder", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, c := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("task deleted after dependency changed", err)
	}
	if _, err = s.ReminderState(t.Context(), reminder.ID); err != nil {
		t.Fatal("linked reminder affected before renewed approval", err)
	}
}

func TestBatchTestPageServed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := httptest.NewRecorder()
	Handler(s).ServeHTTP(w, httptest.NewRequest("GET", "/batch-test", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "Batch deletion test") {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
}

func batchTestSend(t *testing.T, mux *http.ServeMux) func(string, any) (int, map[string]any) {
	t.Helper()
	return func(path string, body any) (int, map[string]any) {
		t.Helper()
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		if w.Code >= 400 && !strings.Contains(w.Body.String(), "expected") {
			t.Log(w.Body.String())
		}
		return w.Code, out
	}
}
