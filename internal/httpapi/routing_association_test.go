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

func TestConversationAssociationReviewAndConfirmation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Prerequisite")
	b, _ := s.Create(t.Context(), "Dependent")
	r, _ := s.CreateReminder(t.Context(), "Linked reminder", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC")
	n, _, _ := s.CreateNoteRequest(t.Context(), "", "Linked note", "", "")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	for _, tc := range []struct{ request, from, to string }{
		{"Add a dependency between tasks", a.ID, b.ID},
		{"Relate a task and note", a.ID, n.NoteID},
		{"Link a note to a task", n.NoteID, a.ID},
		{"Link a reminder to a task", r.ID, a.ID},
	} {
		code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": tc.request, "text": tc.request, "timezone": "UTC"})
		if code != 201 || c["state"] != "awaiting_association_targets" {
			t.Fatalf("%s: %d %v", tc.request, code, c)
		}
		id := c["id"].(string)
		version := int(c["version"].(float64))
		code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": version, "field": "association_targets", "value": tc.from + "," + tc.to})
		if code != 200 || c["state"] != "awaiting_association_confirmation" {
			t.Fatalf("%s review: %d %v", tc.request, code, c)
		}
		if tc.request == "Add a dependency between tasks" {
			deps, _ := s.Dependencies(t.Context())
			if len(deps) != 0 {
				t.Fatal("dependency applied before confirmation")
			}
		}
		version = int(c["version"].(float64))
		code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": version, "text": "yes"})
		if code != 200 || c["state"] != "answered" {
			t.Fatalf("%s commit: %d %v", tc.request, code, c)
		}
	}
	deps, _ := s.Dependencies(t.Context())
	if len(deps) != 1 {
		t.Fatal(deps)
	}
	relations, _ := s.Relations(t.Context(), "", "")
	if len(relations) != 1 {
		t.Fatal(relations)
	}
	note, _ := s.NoteState(t.Context(), n.NoteID)
	if len(note.Note.Links) != 1 {
		t.Fatal(note)
	}
	reminder, _ := s.ReminderState(t.Context(), r.ID)
	if reminder.Reminder.TaskID != a.ID {
		t.Fatal(reminder)
	}
}

func TestAssociationStaleReviewRequiresNewPair(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Before")
	b, _ := s.Create(t.Context(), "After")
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "stale-association", "text": "Add a dependency between tasks", "timezone": "UTC"})
	if code != 201 {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": int(c["version"].(float64)), "value": a.ID + "," + b.ID})
	if code != 200 || c["state"] != "awaiting_association_confirmation" {
		t.Fatal(code, c)
	}
	title := "Changed Before"
	if _, err = s.PatchTaskState(t.Context(), a.ID, store.TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": int(c["version"].(float64)), "text": "yes"})
	if code != 200 || c["state"] != "awaiting_association_targets" {
		t.Fatal(code, c)
	}
	deps, _ := s.Dependencies(t.Context())
	if len(deps) != 0 {
		t.Fatal("stale review mutated dependency")
	}
}

func TestAssociationFocusedPage(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := httptest.NewRecorder()
	Handler(s).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/association-test", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "Relationship test") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAssociationIntentPhrasing(t *testing.T) {
	for _, tc := range []struct {
		text, operation string
		attach          bool
	}{
		{"I want to link a note to a reminder", "note_link", true},
		{"Remove the link between a note and a task", "note_link", false},
		{"Remove a reminder from a task", "reminder_task", false},
		{"Set a dependency between tasks", "dependency", true},
		{"Set a relation between a task and note", "relation", true},
	} {
		got := associationIntent(tc.text)
		if got == nil || got.Operation != tc.operation || got.Attach != tc.attach {
			t.Fatalf("%q: %#v", tc.text, got)
		}
	}
}
