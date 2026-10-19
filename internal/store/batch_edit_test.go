package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchEditStatusIsAtomicAndReplaysReceipt(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "First")
	b, _ := s.Create(t.Context(), "Second")
	r, _ := s.CreateReminder(t.Context(), "Linked", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "UTC")
	if _, err = s.SetReminderTask(t.Context(), r.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	first, _ := s.TaskState(t.Context(), a.ID)
	second, _ := s.TaskState(t.Context(), b.ID)
	reminder, _ := s.ReminderState(t.Context(), r.ID)
	targets := []BatchTarget{{Kind: "task", ID: a.ID, Title: a.Title, UpdatedAt: first.Task.UpdatedAt, LinkedReminders: []string{r.ID + ":" + reminder.Reminder.UpdatedAt}}, {Kind: "task", ID: b.ID, Title: b.Title, UpdatedAt: second.Task.UpdatedAt}}
	stale := append([]BatchTarget(nil), targets...)
	stale[1].UpdatedAt = "old"
	if err = s.EditBatchStatusRequest(t.Context(), "stale", "task", "completed", stale); !errors.Is(err, ErrBatchEditChanged) {
		t.Fatal(err)
	}
	first, _ = s.TaskState(t.Context(), a.ID)
	if first.Task.Status != "open" {
		t.Fatal("partial stale edit")
	}
	if err = s.EditBatchStatusRequest(t.Context(), "confirmed", "task", "completed", targets); err != nil {
		t.Fatal(err)
	}
	if err = s.EditBatchStatusRequest(t.Context(), "confirmed", "task", "completed", targets); err != nil {
		t.Fatal("receipt replay", err)
	}
	for _, id := range []string{a.ID, b.ID} {
		state, e := s.TaskState(t.Context(), id)
		if e != nil || state.Task.Status != "completed" {
			t.Fatal(state, e)
		}
	}
	reminder, _ = s.ReminderState(t.Context(), r.ID)
	if reminder.Reminder.Status != "completed" || reminder.Reminder.CancellationReason != "task.completed" || reminder.Deliveries[0].State != "cancelled" {
		t.Fatal(reminder)
	}
}

func TestBatchNoteEditChecksAllVersions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _, _ := s.CreateNoteRequest(t.Context(), "", "First", "", "")
	b, _, _ := s.CreateNoteRequest(t.Context(), "", "Second", "", "")
	first, _ := s.NoteState(t.Context(), a.NoteID)
	second, _ := s.NoteState(t.Context(), b.NoteID)
	targets := []BatchTarget{{Kind: "note", ID: a.NoteID, UpdatedAt: first.Note.UpdatedAt}, {Kind: "note", ID: b.NoteID, UpdatedAt: second.Note.UpdatedAt}}
	if _, err = s.UpdateNote(t.Context(), b.NoteID, "Changed elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err = s.EditBatchNoteRequest(t.Context(), "review", "body", "New body", targets); !errors.Is(err, ErrBatchEditChanged) {
		t.Fatal(err)
	}
	first, _ = s.NoteState(t.Context(), a.NoteID)
	if first.Note.Body != "First" {
		t.Fatal("partial note edit")
	}
	second, _ = s.NoteState(t.Context(), b.NoteID)
	targets[1].UpdatedAt = second.Note.UpdatedAt
	if err = s.EditBatchNoteRequest(t.Context(), "review", "body", "New body", targets); err != nil {
		t.Fatal(err)
	}
	if err = s.EditBatchNoteRequest(t.Context(), "review", "body", "New body", targets); err != nil {
		t.Fatal("receipt replay", err)
	}
	for _, id := range []string{a.NoteID, b.NoteID} {
		state, e := s.NoteState(t.Context(), id)
		if e != nil || state.Note.Body != "New body" {
			t.Fatal(state, e)
		}
	}
}
