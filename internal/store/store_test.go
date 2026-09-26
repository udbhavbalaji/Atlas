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
	if _, e = db.Exec("PRAGMA user_version=2"); e != nil {
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
