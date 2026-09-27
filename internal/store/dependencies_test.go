package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDependencyCaptureStalenessAndReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	target, e := s.CreateWithFields(ctx, "Interview at Ather", "", "2030-01-01T09:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	input := CaptureInput{Title: "Print resume", BeforeTaskID: target.ID, NoteBody: "Bring two copies"}
	p, e := s.CapturePreview(ctx, input)
	if e != nil || p.Reference == nil || p.Input.DueAt != "" || p.Input.ReminderAt != "" {
		t.Fatal(p, e)
	}
	due := "2030-01-02T09:00:00Z"
	s.Patch(ctx, target.ID, TaskPatch{DueAt: &due})
	if _, _, e = s.CommitCapture(ctx, "before", p.Input); !errors.Is(e, ErrCaptureContext) {
		t.Fatal(e)
	}
	tasks, _ := s.Tasks(ctx)
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	p, e = s.CapturePreview(ctx, input)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec(`CREATE TRIGGER reject_dependency BEFORE INSERT ON task_dependencies BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if _, _, e = s.CommitCapture(ctx, "before", p.Input); e == nil {
		t.Fatal("expected rollback")
	}
	tasks, _ = s.Tasks(ctx)
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	s.db.Exec("DROP TRIGGER reject_dependency")
	v, replay, e := s.CommitCapture(ctx, "before", p.Input)
	if e != nil || replay || len(v.Dependencies) != 1 || v.Dependencies[0].AfterTaskID != target.ID || len(v.Notes) != 1 {
		t.Fatal(v, e)
	}
	id := v.TaskID
	s.Delete(ctx, target.ID)
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	saved, replay, e := s.CommitCapture(ctx, "before", p.Input)
	if e != nil || !replay || saved.TaskID != id {
		t.Fatal(saved, e)
	}
	state, e := s.TaskState(ctx, id)
	if e != nil || state.Dependencies[0].AfterExists || state.Dependencies[0].AfterTitle != target.Title {
		t.Fatal(state, e)
	}
}
func TestDependenciesCycleIdempotencyAndCompletion(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.Create(ctx, "a")
	b, _ := s.Create(ctx, "b")
	c, _ := s.Create(ctx, "c")
	for _, edge := range [][2]string{{a.ID, b.ID}, {b.ID, c.ID}, {a.ID, b.ID}} {
		if _, e = s.SetDependency(ctx, edge[0], edge[1], true); e != nil {
			t.Fatal(e)
		}
	}
	list, e := s.Dependencies(ctx)
	if e != nil || len(list) != 2 {
		t.Fatal(list, e)
	}
	activity, _ := s.Activity(ctx)
	if len(activity) != 5 {
		t.Fatal(activity)
	}
	if _, e = s.SetDependency(ctx, c.ID, a.ID, true); !errors.Is(e, ErrDependencyCycle) {
		t.Fatal(e)
	}
	if _, e = s.SetDependency(ctx, a.ID, a.ID, true); !errors.Is(e, ErrDependencyCycle) {
		t.Fatal(e)
	}
	s.Complete(ctx, a.ID)
	state, e := s.TaskState(ctx, b.ID)
	if e != nil || !state.Dependencies[0].Satisfied {
		t.Fatal(state, e)
	}
	if _, e = s.SetDependency(ctx, a.ID, b.ID, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetDependency(ctx, a.ID, b.ID, false); e != nil {
		t.Fatal(e)
	}
	list, _ = s.Dependencies(ctx)
	if len(list) != 1 {
		t.Fatal(list)
	}
}
func TestUndatedAndCompletedContext(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	target, _ := s.Create(ctx, "Interview")
	p, e := s.CapturePreview(ctx, CaptureInput{Title: "Print resume", BeforeTaskID: target.ID})
	if e != nil || len(p.Warnings) == 0 {
		t.Fatal(p, e)
	}
	s.Complete(ctx, target.ID)
	if _, _, e = s.CommitCapture(ctx, "stale", p.Input); !errors.Is(e, ErrCaptureContext) {
		t.Fatal(e)
	}
	if _, e = s.CapturePreview(ctx, p.Input); !errors.Is(e, ErrDependency) {
		t.Fatal(e)
	}
}

func TestRemoveDependencyAfterSourceDeletion(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	a, _ := s.Create(ctx, "Prerequisite")
	b, _ := s.Create(ctx, "Target")
	if _, e = s.SetDependency(ctx, a.ID, b.ID, true); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, a.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.SetDependency(ctx, a.ID, b.ID, false)
	if e != nil || v.TaskID != b.ID || len(v.Dependencies) != 0 {
		t.Fatal(v, e)
	}
}

func TestDependencyMigrationPreservesCapture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	p, e := PreviewCapture(CaptureInput{Title: "Existing", NoteBody: "Preserved note"})
	if e != nil {
		t.Fatal(e)
	}
	original, _, e := s.CommitCapture(ctx, "legacy", p.Input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DROP TABLE record_relations; ALTER TABLE activity DROP COLUMN relation_id; DROP TABLE task_dependencies; DROP TABLE capture_sessions; PRAGMA user_version=7;"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	replay, replayed, e := s.CommitCapture(ctx, "legacy", p.Input)
	if e != nil || !replayed || replay.TaskID != original.TaskID || len(replay.Notes) != 1 {
		t.Fatal(replay, e)
	}
	state, e := s.TaskState(ctx, original.TaskID)
	if e != nil || state.Dependencies == nil || len(state.Notes) != 1 {
		t.Fatal(state, e)
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 10 {
		t.Fatal(version)
	}
}
