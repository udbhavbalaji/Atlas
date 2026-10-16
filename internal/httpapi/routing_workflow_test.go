package httpapi

import (
	"atlas/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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
		"Delete all tasks and reminders",
		"Make a note of this: buy milk and create a list",
	} {
		if got := workflowClauses(text); len(got) != 0 {
			t.Errorf("split one capture or ambiguous content %q: %#v", text, got)
		}
	}
	for _, text := range []string{
		"Delete all tasks; add a reminder",
		"Create a task to buy milk and create a note about the recipe",
		"Find my interview. Delete the old reminder",
	} {
		if got := workflowClauses(text); len(got) != 2 {
			t.Errorf("missed two explicit actions %q: %#v", text, got)
		}
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
