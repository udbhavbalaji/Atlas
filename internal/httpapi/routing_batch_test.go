package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"errors"
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

func TestBatchDeleteDoesNotSwallowSecondAction(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Keep until reviewed")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-compound", "text": "Delete all tasks and add a reminder", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_route" || c["channel"] != "" || !strings.Contains(c["prompt"].(string), "another action") {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("task changed before clarification", err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "Delete all tasks"})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
}

func TestBatchDeleteUsesWholeKindWordsAndExplainsInvalidSelection(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = s.Create(t.Context(), "Pick task")
	_, _ = s.CreateReminder(t.Context(), "Pick reminder", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, result := send("/api/v1/routing/mock/evaluate", map[string]any{"version": "1", "request_id": "single-note", "text": "Remove note from reminder", "timezone": "UTC", "fixture": "delete"})
	if code != 200 || result["selected_tool"] == "delete_batch" {
		t.Fatal(code, result)
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-invalid", "text": "Delete task and reminder", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_batch_targets" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "not a listed ID"})
	if code != 200 || c["state"] != "awaiting_batch_targets" || !strings.Contains(c["prompt"].(string), "couldn't match") {
		t.Fatal(code, c)
	}
}

func TestUnsupportedRequestExplainsLimitAndAcceptsClarification(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "unsupported-clarify", "text": "Send an email", "timezone": "UTC", "fixture": "unsupported"})
	if code != 201 || c["state"] != "unsupported" || !strings.Contains(c["prompt"].(string), "can't send") || c["next_request"].(map[string]any)["terminal"] != false {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "note"})
	if code != 200 || c["channel"] != "notes" || c["state"] == "unsupported" {
		t.Fatal(code, c)
	}
}

func TestExplicitUnavailableWorkflowOperations(t *testing.T) {
	for _, tc := range []struct{ request, reason string }{
		{"Link this note to my task", "can't change links"},
		{"Add a dependency between my tasks", "can't change task dependencies"},
		{"Change the repeat rule on my reminder", "can't change an existing reminder's repeat rule"},
		{"Complete all tasks", "can't change multiple"},
		{"Create a project", "can't create a project"},
	} {
		if got := unsupportedWorkflowReason(tc.request); !strings.Contains(got, tc.reason) {
			t.Errorf("%q: %q", tc.request, got)
		}
	}
	if got := unsupportedWorkflowReason("Add a reminder about my project"); got != "" {
		t.Fatalf("supported reminder classified as unsupported: %q", got)
	}
}

func TestBatchDeleteDuplicateTitlesRequireExplicitSelection(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = s.Create(t.Context(), "Duplicate target")
	_, _ = s.Create(t.Context(), "Duplicate target")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	code, c := batchTestSend(t, mux)("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "batch-duplicates", "text": "Delete Duplicate target", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_batch_targets" {
		t.Fatal(code, c)
	}
}

func TestBatchDeleteConfirmationReceiptSurvivesRetry(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Receipt target")
	if err != nil {
		t.Fatal(err)
	}
	targets := []store.BatchTarget{{Kind: "task", ID: task.ID, Title: task.Title, UpdatedAt: task.UpdatedAt}}
	if err = s.DeleteBatchRequest(t.Context(), "batch-retry", targets); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteBatchRequest(t.Context(), "batch-retry", targets); err != nil {
		t.Fatal("committed confirmation did not replay", err)
	}
	changed := append([]store.BatchTarget(nil), targets...)
	changed[0].Title = "Different review"
	if err = s.DeleteBatchRequest(t.Context(), "batch-retry", changed); !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("different confirmation reused receipt: %v", err)
	}
	if err = s.DeleteBatch(t.Context(), nil); !errors.Is(err, store.ErrBatchChanged) {
		t.Fatalf("empty request succeeded: %v", err)
	}
}

func TestAmbiguousRouteShowsLikelyActionsAndRejectedReply(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "ambiguous-suggestions", "text": "Remember the meeting", "timezone": "UTC", "fixture": "ambiguous"})
	if code != 201 || c["state"] != "awaiting_route" || !strings.Contains(c["prompt"].(string), "add a task") || !strings.Contains(c["prompt"].(string), "add a reminder") {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "maybe later"})
	if code != 200 || c["state"] != "awaiting_route" || !strings.Contains(c["prompt"].(string), "couldn't match") {
		t.Fatal(code, c)
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
