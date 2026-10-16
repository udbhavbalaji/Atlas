package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewedAssociationOperationsAndStaleReview(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.Create(t.Context(), "Before task")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(t.Context(), "After task")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateReminder(t.Context(), "Linked reminder", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := s.CreateNoteRequest(t.Context(), "", "Linked note", "", "")
	if err != nil {
		t.Fatal(err)
	}
	taskA := BatchTarget{Kind: "task", ID: a.ID, Title: a.Title, UpdatedAt: a.UpdatedAt}
	taskB := BatchTarget{Kind: "task", ID: b.ID, Title: b.Title, UpdatedAt: b.UpdatedAt}
	reminder := BatchTarget{Kind: "reminder", ID: r.ID, Title: r.Title, UpdatedAt: r.UpdatedAt}
	note := BatchTarget{Kind: "note", ID: n.Note.ID, Title: n.Note.Title, UpdatedAt: n.Note.UpdatedAt}
	for _, tc := range []struct {
		key    string
		change AssociationChange
	}{
		{"dependency", AssociationChange{Operation: "dependency", Attach: true, From: taskA, To: taskB}},
		{"relation", AssociationChange{Operation: "relation", Attach: true, From: taskA, To: taskB}},
		{"note", AssociationChange{Operation: "note_link", Attach: true, From: note, To: taskA}},
		{"reminder", AssociationChange{Operation: "reminder_task", Attach: true, From: reminder, To: taskA}},
	} {
		if err = s.ApplyAssociationRequest(t.Context(), tc.key, tc.change); err != nil {
			t.Fatalf("%s: %v", tc.key, err)
		}
		if err = s.ApplyAssociationRequest(t.Context(), tc.key, tc.change); err != nil {
			t.Fatalf("%s receipt did not replay: %v", tc.key, err)
		}
	}
	deps, err := s.Dependencies(t.Context())
	if err != nil || len(deps) != 1 {
		t.Fatal(deps, err)
	}
	relations, err := s.Relations(t.Context(), "", "")
	if err != nil || len(relations) != 1 {
		t.Fatal(relations, err)
	}
	noteState, err := s.NoteState(t.Context(), n.NoteID)
	if err != nil || len(noteState.Note.Links) != 1 {
		t.Fatal(noteState, err)
	}
	reminderState, err := s.ReminderState(t.Context(), r.ID)
	if err != nil || reminderState.Reminder.TaskID != a.ID {
		t.Fatal(reminderState, err)
	}
	changed := AssociationChange{Operation: "relation", Attach: false, From: taskA, To: taskB}
	title := "Updated before task"
	if _, err = s.PatchTaskState(t.Context(), a.ID, TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyAssociationRequest(t.Context(), "stale", changed); !errors.Is(err, ErrAssociationChanged) {
		t.Fatalf("stale pair was accepted: %v", err)
	}
	relations, err = s.Relations(t.Context(), "", "")
	if err != nil || len(relations) != 1 {
		t.Fatal("stale removal changed relation", relations, err)
	}
	aState, err := s.TaskState(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	bState, err := s.TaskState(t.Context(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	rState, err := s.ReminderState(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	nState, err := s.NoteState(t.Context(), n.NoteID)
	if err != nil {
		t.Fatal(err)
	}
	taskA = BatchTarget{Kind: "task", ID: a.ID, Title: aState.Task.Title, UpdatedAt: aState.Task.UpdatedAt}
	taskB = BatchTarget{Kind: "task", ID: b.ID, Title: bState.Task.Title, UpdatedAt: bState.Task.UpdatedAt}
	reminder = BatchTarget{Kind: "reminder", ID: r.ID, Title: rState.Reminder.Title, UpdatedAt: rState.Reminder.UpdatedAt}
	note = BatchTarget{Kind: "note", ID: n.NoteID, Title: nState.Note.Title, UpdatedAt: nState.Note.UpdatedAt}
	for _, tc := range []struct {
		key    string
		change AssociationChange
	}{
		{"remove-dependency", AssociationChange{Operation: "dependency", From: taskA, To: taskB}},
		{"remove-relation", AssociationChange{Operation: "relation", From: taskA, To: taskB}},
		{"remove-note-link", AssociationChange{Operation: "note_link", From: note, To: taskA}},
		{"remove-reminder-link", AssociationChange{Operation: "reminder_task", From: reminder, To: taskA}},
	} {
		if err = s.ApplyAssociationRequest(t.Context(), tc.key, tc.change); err != nil {
			t.Fatalf("%s: %v", tc.key, err)
		}
	}
	deps, err = s.Dependencies(t.Context())
	if err != nil || len(deps) != 0 {
		t.Fatal(deps, err)
	}
	relations, err = s.Relations(t.Context(), "", "")
	if err != nil || len(relations) != 0 {
		t.Fatal(relations, err)
	}
	nState, err = s.NoteState(t.Context(), n.NoteID)
	if err != nil || len(nState.Note.Links) != 0 {
		t.Fatal(nState, err)
	}
	rState, err = s.ReminderState(t.Context(), r.ID)
	if err != nil || rState.Reminder.TaskID != "" {
		t.Fatal(rState, err)
	}
}
