package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotesLinksPersistenceAndDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create(ctx, "Context task")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateReminder(ctx, "Context reminder", "2030-01-01T09:00:00Z", "UTC")
	if e != nil {
		t.Fatal(e)
	}
	initial, replay, e := s.CreateNoteRequest(ctx, "one-note", "Keep this context\nSecond line", task.ID, r.ID)
	if e != nil || replay || len(initial.Note.Links) != 2 {
		t.Fatal(initial, replay, e)
	}
	n := initial.Note
	state, e := s.UpdateNote(ctx, n.ID, "Edited context")
	if e != nil || state.Note.Body != "Edited context" {
		t.Fatal(state, e)
	}
	a, e := s.Activity(ctx)
	if e != nil {
		t.Fatal(e)
	}
	count := len(a)
	if _, e = s.UpdateNote(ctx, n.ID, "Edited context"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetNoteLink(ctx, n.ID, "task", task.ID, true); e != nil {
		t.Fatal(e)
	}
	a, e = s.Activity(ctx)
	if e != nil || len(a) != count {
		t.Fatal(a, e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e = s.NoteState(ctx, n.ID)
	if e != nil || state.Note.Body != "Edited context" || len(state.Note.Links) != 2 {
		t.Fatal(state, e)
	}
	taskState, e := s.TaskState(ctx, task.ID)
	if e != nil || len(taskState.Notes) != 1 {
		t.Fatal(taskState, e)
	}
	reminderState, e := s.ReminderState(ctx, r.ID)
	if e != nil || len(reminderState.Notes) != 1 {
		t.Fatal(reminderState, e)
	}
	if e = s.Delete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	state, e = s.NoteState(ctx, n.ID)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, link := range state.Note.Links {
		if link.TargetType == "task" {
			found = true
			if link.TargetExists || link.TargetTitle != "Context task" {
				t.Fatal(link)
			}
		}
	}
	if !found {
		t.Fatal(state)
	}
	saved, replay, e := s.CreateNoteRequest(ctx, "one-note", "Keep this context\nSecond line", task.ID, r.ID)
	if e != nil || !replay || saved.NoteID != n.ID || saved.Note.Body != n.Body {
		t.Fatal(saved, replay, e)
	}
	if _, _, e = s.CreateNoteRequest(ctx, "one-note", "Different", task.ID, r.ID); !errors.Is(e, ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	if _, e = s.SetNoteLink(ctx, n.ID, "task", task.ID, false); e != nil {
		t.Fatal(e)
	}
	deleted, e := s.DeleteNote(ctx, n.ID)
	if e != nil || !deleted.Deleted || deleted.Note != nil {
		t.Fatal(deleted, e)
	}
	saved, replay, e = s.CreateNoteRequest(ctx, "one-note", "Keep this context\nSecond line", task.ID, r.ID)
	if e != nil || !replay || saved.NoteID != n.ID {
		t.Fatal(saved, e)
	}
	notes, e := s.Notes(ctx)
	if e != nil || len(notes) != 0 {
		t.Fatal(notes, e)
	}
	var links int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM note_links").Scan(&links); e != nil || links != 0 {
		t.Fatal(links, e)
	}
}
func TestNoteValidationRollbackAndConcurrentCapture(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, body := range []string{"", " \n\t", strings.Repeat("文", 10001)} {
		if _, _, e = s.CreateNoteRequest(ctx, "", body, "", ""); !errors.Is(e, ErrInvalidNote) {
			t.Fatal(e)
		}
	}
	if _, _, e = s.CreateNoteRequest(ctx, "", "Uncommitted", "missing", ""); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	notes, e := s.Notes(ctx)
	if e != nil || len(notes) != 0 {
		t.Fatal(notes, e)
	}
	results := make(chan NoteAction, 8)
	errs := make(chan error, 8)
	for range 8 {
		go func() { v, _, e := s.CreateNoteRequest(ctx, "concurrent", "One note", "", ""); results <- v; errs <- e }()
	}
	id := ""
	for range 8 {
		v := <-results
		if e = <-errs; e != nil {
			t.Fatal(e)
		}
		if id != "" && id != v.NoteID {
			t.Fatal("duplicate")
		}
		id = v.NoteID
	}
	task, e := s.Create(ctx, "Link")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`CREATE TRIGGER reject_note_link BEFORE INSERT ON activity WHEN NEW.action='note.linked' BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetNoteLink(ctx, id, "task", task.ID, true); e == nil {
		t.Fatal("expected rollback")
	}
	state, e := s.NoteState(ctx, id)
	if e != nil || len(state.Note.Links) != 0 {
		t.Fatal(state, e)
	}
	if _, _, e = s.CreateNoteRequest(ctx, "rollback", "Rollback note", task.ID, ""); e == nil {
		t.Fatal("expected rollback")
	}
	notes, e = s.Notes(ctx)
	if e != nil || len(notes) != 1 {
		t.Fatal(notes, e)
	}
}
