package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRelationsSymmetryLifecycleAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := t.Context()
	a, _ := s.Create(ctx, "Interview")
	b, _ := s.Create(ctx, "Print resume")
	reminder, _ := s.CreateReminder(ctx, "Bring papers", "2090-01-01T09:00:00Z", "UTC")
	note, _, _ := s.CreateNoteRequest(ctx, "", "Bring two copies", "", "")
	for _, pair := range [][4]string{{"task", a.ID, "task", b.ID}, {"note", note.NoteID, "reminder", reminder.ID}, {"task", a.ID, "note", note.NoteID}, {"reminder", reminder.ID, "task", b.ID}} {
		first, e := s.SetRelation(ctx, pair[0], pair[1], pair[2], pair[3], true)
		if e != nil || first.Relation == nil {
			t.Fatal(first, e)
		}
		second, e := s.SetRelation(ctx, pair[2], pair[3], pair[0], pair[1], true)
		if e != nil || first.RelationID != second.RelationID {
			t.Fatal(second, e)
		}
	}
	list, _ := s.Relations(ctx, "", "")
	if len(list) != 4 {
		t.Fatal(list)
	}
	activity, _ := s.Activity(ctx)
	count := 0
	for _, a := range activity {
		if a.Action == "relation.added" {
			count++
			if a.RelationID == "" {
				t.Fatal(a)
			}
		}
	}
	if count != 4 {
		t.Fatal(activity)
	}
	s.Complete(ctx, a.ID)
	state, _ := s.ReminderState(ctx, reminder.ID)
	if state.Reminder.Status != "scheduled" {
		t.Fatal(state)
	}
	filtered, _ := s.Relations(ctx, "task", a.ID)
	if len(filtered) != 2 {
		t.Fatal(filtered)
	}
	s.DeleteNote(ctx, note.NoteID)
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	list, _ = s.Relations(ctx, "note", note.NoteID)
	if len(list) != 2 || list[0].From.Exists || list[0].From.Title != "Bring two copies" {
		t.Fatal(list)
	}
	for range 2 {
		v, e := s.SetRelation(ctx, "note", note.NoteID, "task", a.ID, false)
		if e != nil || !v.Deleted || v.Relation != nil {
			t.Fatal(v, e)
		}
	}
	if _, e = s.SetRelation(ctx, "task", a.ID, "task", a.ID, true); !errors.Is(e, ErrRelation) {
		t.Fatal(e)
	}
	if _, e = s.SetRelation(ctx, "task", a.ID, "note", note.NoteID, true); !errors.Is(e, ErrNoteNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Relations(ctx, "task", ""); !errors.Is(e, ErrRelation) {
		t.Fatal(e)
	}
	s.db.Exec(`CREATE TRIGGER reject_relation_activity BEFORE INSERT ON activity WHEN NEW.action='relation.added' BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if _, e = s.SetRelation(ctx, "task", a.ID, "reminder", reminder.ID, true); e == nil {
		t.Fatal("expected rollback")
	}
	list, _ = s.Relations(ctx, "task", a.ID)
	if len(list) != 1 {
		t.Fatal(list)
	}
}
func TestRelationMigrationPreservesDependenciesAndReceipts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := t.Context()
	target, _ := s.Create(ctx, "Interview")
	p, _ := s.CapturePreview(ctx, CaptureInput{Title: "Print resume", BeforeTaskID: target.ID, NoteBody: "Use good paper"})
	saved, _, e := s.CommitCapture(ctx, "before", p.Input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DROP TABLE record_relations; ALTER TABLE activity DROP COLUMN relation_id; DROP TABLE capture_sessions; PRAGMA user_version=8;"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e := s.TaskState(ctx, saved.TaskID)
	if e != nil || len(state.Dependencies) != 1 || len(state.Notes) != 1 {
		t.Fatal(state, e)
	}
	replay, replayed, e := s.CommitCapture(ctx, "before", p.Input)
	if e != nil || !replayed || replay.TaskID != saved.TaskID {
		t.Fatal(replay, e)
	}
	relations, e := s.Relations(ctx, "", "")
	if e != nil || relations == nil || len(relations) != 0 {
		t.Fatal(relations, e)
	}
}
