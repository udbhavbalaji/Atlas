package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestExistingReminderTaskLink(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.Create(ctx, "Shopping")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateReminder(ctx, "Shopping time", instant(time.Now().Add(time.Hour)), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	state, e := s.SetReminderTask(ctx, r.ID, task.ID)
	if e != nil || state.Reminder.TaskID != task.ID || state.Task == nil || state.Task.ID != task.ID || state.Reminder.ScheduledAt != r.ScheduledAt {
		t.Fatal(state, e)
	}
	a, e := s.Activity(ctx)
	if e != nil {
		t.Fatal(e)
	}
	count := len(a)
	if _, e = s.SetReminderTask(ctx, r.ID, task.ID); e != nil {
		t.Fatal(e)
	}
	a, e = s.Activity(ctx)
	if e != nil || len(a) != count {
		t.Fatal(a, e)
	}
	if _, e = s.SetReminderTask(ctx, r.ID, "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	state, e = s.SetReminderTask(ctx, r.ID, "")
	if e != nil || state.Task != nil || state.Reminder.TaskID != "" || state.Reminder.TaskTitle != "" {
		t.Fatal(state, e)
	}
	if _, e = s.SetReminderTask(ctx, r.ID, task.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Complete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	state, e = s.ReminderState(ctx, r.ID)
	if e != nil || state.Reminder.CancellationReason != "task.completed" || state.Deliveries[0].State != "cancelled" {
		t.Fatal(state, e)
	}
	if _, e = s.SetReminderTask(ctx, r.ID, ""); !errors.Is(e, ErrReminderConflict) {
		t.Fatal(e)
	}
	standalone, e := s.CreateReminder(ctx, "Keep standalone", instant(time.Now().Add(time.Hour)), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetReminderTask(ctx, standalone.ID, task.ID); !errors.Is(e, ErrTaskReminderConflict) {
		t.Fatal(e)
	}
	other, e := s.Create(ctx, "Other")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`CREATE TRIGGER reject_link BEFORE INSERT ON activity WHEN NEW.action='reminder.task_linked' BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetReminderTask(ctx, standalone.ID, other.ID); e == nil {
		t.Fatal("expected rollback")
	}
	state, e = s.ReminderState(ctx, standalone.ID)
	if e != nil || state.Reminder.TaskID != "" {
		t.Fatal(state, e)
	}
}
