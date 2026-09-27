package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestPersistenceAndCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atlas.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	task, e := s.Create(ctx, " Call Mom ")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 || tasks[0].Title != "Call Mom" || tasks[0].ID != task.ID {
		t.Fatalf("reopened tasks: %+v %v", tasks, e)
	}
	for range 2 {
		if e = s.Complete(ctx, task.ID); e != nil {
			t.Fatal(e)
		}
	}
	activity, e := s.Activity(ctx)
	if e != nil || len(activity) != 2 || activity[0].Action != "task.completed" {
		t.Fatalf("activity: %+v %v", activity, e)
	}
	tasks, e = s.Tasks(ctx)
	if e != nil || tasks[0].Status != "completed" {
		t.Fatal(tasks, e)
	}
	if e = s.Complete(ctx, "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, "  "); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestNewerSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("PRAGMA user_version=11"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(path); e == nil {
		s.Close()
		t.Fatal("accepted future schema")
	}
}
func TestActivityFailureRollsBackTask(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "atlas.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.db.Exec("DROP TABLE activity"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(context.Background(), "rollback"); e == nil {
		t.Fatal("expected activity failure")
	}
	tasks, e := s.Tasks(context.Background())
	if e != nil || len(tasks) != 0 {
		t.Fatal(tasks, e)
	}
}

func TestConcurrentCompletion(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "atlas.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	task, e := s.Create(ctx, "Complete once")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- s.Complete(ctx, task.ID) }()
	}
	for range 8 {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	activity, e := s.Activity(ctx)
	if e != nil || len(activity) != 2 {
		t.Fatal(activity, e)
	}
}

func TestUpdateAndDeleteRollback(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "atlas.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	task, e := s.Create(ctx, "Keep me")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DROP TABLE activity"); e != nil {
		t.Fatal(e)
	}
	title := "Changed"
	if _, e = s.Update(ctx, task.ID, &title, nil); e == nil {
		t.Fatal("expected update failure")
	}
	if e = s.Delete(ctx, task.ID); e == nil {
		t.Fatal("expected delete failure")
	}
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 || tasks[0].Title != "Keep me" {
		t.Fatal(tasks, e)
	}
}

func TestVersionOneMigrationAndDeadlinePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);CREATE TABLE activity(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,action TEXT NOT NULL,timestamp TEXT NOT NULL);INSERT INTO tasks VALUES('old','Existing','open','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');PRAGMA user_version=1;`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 || tasks[0].Details != "" || tasks[0].DueAt != "" {
		t.Fatal(tasks, e)
	}
	details, due := "Important context", "2026-10-01T18:00:00+05:30"
	updated, e := s.Patch(ctx, "old", TaskPatch{Details: &details, DueAt: &due})
	if e != nil || updated.DueAt != "2026-10-01T12:30:00.000000000Z" {
		t.Fatal(updated, e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	tasks, e = s.Tasks(ctx)
	if e != nil || tasks[0].Details != details || tasks[0].DueAt != updated.DueAt {
		t.Fatal(tasks, e)
	}
	title := "Renamed"
	updated, e = s.Patch(ctx, "old", TaskPatch{Title: &title})
	if e != nil || updated.DueAt != tasks[0].DueAt {
		t.Fatal(updated, e)
	}
	empty := ""
	updated, e = s.Patch(ctx, "old", TaskPatch{DueAt: &empty, Details: &empty})
	if e != nil || updated.DueAt != "" || updated.Details != "" {
		t.Fatal(updated, e)
	}
	for _, bad := range []string{"tomorrow", "2026-10-01", "2026-10-01T18:00:00", "2026-02-30T18:00:00Z"} {
		if _, e = s.Patch(ctx, "old", TaskPatch{DueAt: &bad}); !errors.Is(e, ErrInvalidFields) {
			t.Fatal(bad, e)
		}
	}
}
