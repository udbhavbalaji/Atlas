package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCreationReceiptsSurviveRestartAndDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	task, replay, e := s.CreateTaskRequest(ctx, "key-1", "Task", "Details", "")
	if e != nil || replay {
		t.Fatal(task, replay, e)
	}
	r, replay, e := s.CreateReminderRequest(ctx, "key-1", "Reminder", "2030-01-01T12:00:00Z", "UTC", task.ID)
	if e != nil || replay {
		t.Fatal(r, replay, e)
	}
	if e = s.Delete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	saved, replay, e := s.CreateTaskRequest(ctx, "key-1", "Task", "Details", "")
	if e != nil || !replay || saved != task {
		t.Fatal(saved, replay, e)
	}
	savedReminder, replay, e := s.CreateReminderRequest(ctx, "key-1", "Reminder", "2030-01-01T12:00:00Z", "UTC", task.ID)
	if e != nil || !replay || savedReminder != r {
		t.Fatal(savedReminder, replay, e)
	}
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 0 {
		t.Fatal(tasks, e)
	}
	reminders, e := s.Reminders(ctx)
	if e != nil || len(reminders) != 1 || reminders[0].CancellationReason != "task.deleted" {
		t.Fatal(reminders, e)
	}
	if _, _, e = s.CreateTaskRequest(ctx, "key-1", "Different", "Details", ""); !errors.Is(e, ErrIdempotencyConflict) {
		t.Fatal(e)
	}
}
func TestConcurrentDuplicateCreationAndRollback(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	type outcome struct {
		r   Reminder
		err error
	}
	out := make(chan outcome, 8)
	for range 8 {
		go func() {
			r, _, err := s.CreateReminderRequest(ctx, "same-reminder", "Reminder", "2030-01-01T12:00:00Z", "UTC", "")
			out <- outcome{r, err}
		}()
	}
	id := ""
	for range 8 {
		result := <-out
		if result.err != nil {
			t.Fatal(result.err)
		}
		if id != "" && id != result.r.ID {
			t.Fatal("duplicate reminders")
		}
		id = result.r.ID
	}
	reminders, e := s.Reminders(ctx)
	if e != nil || len(reminders) != 1 {
		t.Fatal(reminders, e)
	}
	deliveries, e := s.Deliveries(ctx)
	if e != nil || len(deliveries) != 1 {
		t.Fatal(deliveries, e)
	}
	a, e := s.Activity(ctx)
	if e != nil || len(a) != 1 {
		t.Fatal(a, e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_receipt BEFORE INSERT ON request_receipts WHEN NEW.request_key='rollback' BEGIN SELECT RAISE(ABORT,'test'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CreateTaskRequest(ctx, "rollback", "Retry me", "", ""); e == nil {
		t.Fatal("expected rollback")
	}
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 0 {
		t.Fatal(tasks, e)
	}
	if _, e = s.db.Exec("DROP TRIGGER reject_receipt"); e != nil {
		t.Fatal(e)
	}
	if _, replay, e := s.CreateTaskRequest(ctx, "rollback", "Retry me", "", ""); e != nil || replay {
		t.Fatal(replay, e)
	}
}
func TestActionSnapshotsAndRepeatedCompletion(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.Create(ctx, "Task")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateLinkedReminder(ctx, "Reminder", instant(time.Now().Add(time.Hour)), "UTC", task.ID)
	if e != nil {
		t.Fatal(e)
	}
	state, e := s.CompleteTaskState(ctx, task.ID)
	if e != nil || state.Task == nil || state.Task.Status != "completed" || state.Deleted || len(state.Reminders) != 1 || state.Reminders[0].CancellationReason != "task.completed" || state.Deliveries[0].State != "cancelled" {
		t.Fatal(state, e)
	}
	if _, e = s.CompleteTaskState(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	a, e := s.Activity(ctx)
	if e != nil || len(a) != 4 {
		t.Fatal(a, e)
	}
	action, e := s.ReminderMutation(ctx, r.ID, "completed", "")
	if e != nil || action.Task.Status != "completed" || action.Reminder.Status != "completed" || len(action.Deliveries) != 1 {
		t.Fatal(action, e)
	}
	deleted, e := s.DeleteTaskState(ctx, task.ID)
	if e != nil || !deleted.Deleted || deleted.Task != nil || len(deleted.Reminders) != 1 {
		t.Fatal(deleted, e)
	}
	action, e = s.ReminderState(ctx, r.ID)
	if e != nil || action.Task != nil {
		t.Fatal(action, e)
	}
}
