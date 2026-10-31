package httpapi

import (
	"atlas/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnparseableOrOversizedWorkflowDoesNotExecuteOneStep(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Create(t.Context(), "Keep task"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	for _, tc := range []struct{ text, state string }{
		{"Delete all tasks; perhaps later create a reminder", "awaiting_route"},
		{strings.Repeat("delete all tasks; ", 13), "unsupported"},
	} {
		code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "workflow-audit", "text": tc.text, "timezone": "UTC"})
		if code != 201 || c["state"] != tc.state || c["channel"] != "" {
			t.Fatal(code, c)
		}
	}
	rows, err := s.Tasks(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatal("unreviewed workflow changed records", rows, err)
	}
}

func TestWorkflowClausesKeepAtomicCapturesTogether(t *testing.T) {
	for _, text := range []string{
		"Create a task for my interview, remind me an hour before, and note that I should bring a portfolio",
		"Add a task to buy milk and add a reminder for it",
		"Remind me to call Rahul tomorrow at 10 AM and make a note that we need to discuss the scooter's battery issue",
		"Delete all tasks and reminders",
		"Make a note of this: buy milk and create a list",
	} {
		if got := workflowClauses(text); len(got) != 0 {
			t.Errorf("split one capture or ambiguous content %q: %#v", text, got)
		}
		if text == "Remind me to call Rahul tomorrow at 10 AM and make a note that we need to discuss the scooter's battery issue" && possibleWorkflow(text) {
			t.Errorf("attached note became an unresolved workflow: %q", text)
		}
	}
	for _, text := range []string{
		"Delete all tasks; add a reminder",
		"Remind me to call Rahul tomorrow at 10 AM; make a note about the battery",
		"Remind me to call Rahul and make a note about the battery; delete the old reminder",
		"Remind me to call Rahul and make a note about the battery and delete the old reminder",
		"Create a task to buy milk and create a note about the recipe",
		"Find my interview. Delete the old reminder",
	} {
		if got := workflowClauses(text); len(got) < 2 {
			t.Errorf("missed separate explicit actions %q: %#v", text, got)
		}
	}
}

func TestMockReminderWithAttachedNoteCanBeReviewed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "reminder", "version": "1", "request_id": "mock-reminder-note", "text": "Remind me to call Rahul tomorrow at 10 AM and make a note that we need to discuss the battery issue.", "timezone": "UTC"})
	if code != 201 || c["workflow"] != nil || c["channel"] != "reminders" {
		t.Fatal(code, c)
	}
	path := "/api/v1/conversations/routing/" + c["id"].(string) + "/reply"
	for steps := 0; c["state"] == "awaiting_answer" && steps < 5; steps++ {
		field := c["question"].(map[string]any)["field"].(string)
		value := map[string]string{"fields.title": "Call Rahul", "fields.reminder_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), "fields.note_body": "Discuss the battery issue with Rahul."}[field]
		if value == "" {
			t.Fatal("unexpected question", c)
		}
		code, c = send(path, map[string]any{"version": c["version"], "field": field, "value": value})
		if code != 200 {
			t.Fatal(code, c)
		}
	}
	if c["state"] != "awaiting_confirmation" || !strings.Contains(c["prompt"].(string), "note") {
		t.Fatal(c)
	}
	reminders, _ := s.Reminders(t.Context())
	notes, _ := s.Notes(t.Context())
	if len(reminders) != 0 || len(notes) != 0 {
		t.Fatal("mock preview saved records", reminders, notes)
	}
}

func TestWorkflowRunsReviewedStepsInOrder(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Workflow task")
	if err != nil {
		t.Fatal(err)
	}
	note, _, err := s.CreateNoteRequest(t.Context(), "", "Workflow note", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "ordered-delete", "text": "Delete all tasks; delete all notes", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_workflow_start" || len(c["workflow"].(map[string]any)["steps"].([]any)) != 2 {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("first step ran before approval", err)
	}
	path := "/api/v1/conversations/routing/" + c["id"].(string) + "/reply"
	code, c = send(path, map[string]any{"version": c["version"], "text": "start"})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" {
		t.Fatal(code, c)
	}
	code, c = send(path, map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "continue") {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err == nil {
		t.Fatal("first confirmed step did not run")
	}
	if _, err = s.NoteState(t.Context(), note.NoteID); err != nil {
		t.Fatal("second step ran early", err)
	}
	code, c = send(path, map[string]any{"version": c["version"], "text": "continue"})
	if code != 200 || c["state"] != "awaiting_delete_confirmation" || c["workflow"].(map[string]any)["index"] != float64(1) {
		t.Fatal(code, c)
	}
	code, c = send(path, map[string]any{"version": c["version"], "text": "cancel"})
	if code != 200 || c["state"] != "cancelled" {
		t.Fatal(code, c)
	}
	if _, err = s.NoteState(t.Context(), note.NoteID); err != nil {
		t.Fatal("cancelled step changed its record", err)
	}
}

func TestWorkflowTestPageServed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := httptest.NewRecorder()
	Handler(s).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workflow-test", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "Orchestration test") {
		t.Fatal(w.Code, w.Body.String())
	}
}
