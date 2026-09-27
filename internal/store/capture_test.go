package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func captureFixture(t *testing.T) CaptureInput {
	t.Helper()
	p, e := PreviewCapture(CaptureInput{Title: " Call Mom ", Details: "Discuss plans", DueAt: "2030-01-02T18:00:00+05:30", ReminderAt: "2030-01-01T18:00:00+05:30", Timezone: "Asia/Kolkata", Repeat: "daily"})
	if e != nil {
		t.Fatal(e)
	}
	return p.Input
}
func TestCaptureAtomicRollbackAndRetry(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	input := captureFixture(t)
	tasks, _ := s.Tasks(ctx)
	if len(tasks) != 0 {
		t.Fatal("preview wrote task")
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_capture BEFORE INSERT ON request_receipts WHEN NEW.operation='capture.commit' BEGIN SELECT RAISE(ABORT,'test'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CommitCapture(ctx, "capture-1", input); e == nil {
		t.Fatal("expected receipt failure")
	}
	for _, table := range []string{"tasks", "reminders", "deliveries", "activity", "request_receipts"} {
		var count int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal(table, count, e)
		}
	}
	if _, e = s.db.Exec("DROP TRIGGER reject_capture"); e != nil {
		t.Fatal(e)
	}
	result, replay, e := s.CommitCapture(ctx, "capture-1", input)
	if e != nil || replay || result.Task.Title != "Call Mom" || len(result.Reminders) != 1 || len(result.Deliveries) != 1 || result.Reminders[0].TaskID != result.TaskID || result.Reminders[0].RepeatAnchor != input.ReminderAt || result.Reminders[0].OccurrenceID != result.Deliveries[0].ID {
		t.Fatal(result, replay, e)
	}
	a, e := s.Activity(ctx)
	if e != nil || len(a) != 2 {
		t.Fatal(a, e)
	}
	saved, replay, e := s.CommitCapture(ctx, "capture-1", input)
	if e != nil || !replay || !reflect.DeepEqual(saved, result) {
		t.Fatal(saved, replay, e)
	}
	changed := input
	changed.Title = "Different"
	if _, _, e = s.CommitCapture(ctx, "new-key", changed); !errors.Is(e, ErrCapturePreview) {
		t.Fatal(e)
	}
	p, e := PreviewCapture(changed)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CommitCapture(ctx, "capture-1", p.Input); !errors.Is(e, ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	state, e := s.CompleteTaskState(ctx, result.TaskID)
	if e != nil || state.Reminders[0].CancellationReason != "task.completed" || state.Deliveries[0].State != "cancelled" {
		t.Fatal(state, e)
	}
}
func TestCaptureConcurrentReplayPersists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	input := captureFixture(t)
	type outcome struct {
		state TaskAction
		err   error
	}
	out := make(chan outcome, 8)
	for range 8 {
		go func() { v, _, e := s.CommitCapture(ctx, "same", input); out <- outcome{v, e} }()
	}
	id := ""
	for range 8 {
		r := <-out
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id != "" && id != r.state.TaskID {
			t.Fatal("duplicate task")
		}
		id = r.state.TaskID
	}
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 {
		t.Fatal(tasks, e)
	}
	r, e := s.Reminders(ctx)
	if e != nil || len(r) != 1 {
		t.Fatal(r, e)
	}
	if e = s.Delete(ctx, id); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v, replay, e := s.CommitCapture(ctx, "same", input)
	if e != nil || !replay || v.TaskID != id || v.Task.Status != "open" {
		t.Fatal(v, replay, e)
	}
}
func TestCaptureValidationAndTaskOnly(t *testing.T) {
	for _, input := range []CaptureInput{{}, {Title: "Task", Repeat: "daily"}, {Title: "Task", Timezone: "UTC"}, {Title: "Task", ReminderAt: "bad"}, {Title: "Task", ReminderAt: "2030-01-01T00:00:00Z", Timezone: "Local"}, {Title: "Task", ReminderAt: "2030-01-01T00:00:00Z", Timezone: "UTC", Repeat: "monthly"}} {
		if _, e := PreviewCapture(input); e == nil {
			t.Fatal(input)
		}
	}
	p, e := PreviewCapture(CaptureInput{Title: "Task"})
	if e != nil || len(p.Effects) != 1 || len(p.Warnings) != 0 {
		t.Fatal(p, e)
	}
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, _, e = s.CommitCapture(ctx, "", p.Input); !errors.Is(e, ErrCaptureKey) {
		t.Fatal(e)
	}
	input := p.Input
	input.PreviewID = ""
	if _, _, e = s.CommitCapture(ctx, "key", input); !errors.Is(e, ErrCapturePreview) {
		t.Fatal(e)
	}
	v, _, e := s.CommitCapture(ctx, "key", p.Input)
	if e != nil || v.Task == nil || len(v.Reminders) != 0 || len(v.Deliveries) != 0 || v.Notes == nil {
		t.Fatal(v, e)
	}
	past, e := PreviewCapture(CaptureInput{Title: "Task", DueAt: "2000-01-01T00:00:00Z", ReminderAt: "2000-01-01T00:00:00Z", Timezone: "UTC"})
	if e != nil || len(past.Warnings) != 2 {
		t.Fatal(past, e)
	}
}
