package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLinkedScheduleWorkflowMovesEntireGroupAtomically(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	task, err := s.Create(ctx, "Interview with Ather")
	if err != nil {
		t.Fatal(err)
	}
	due := "2030-01-10T17:00:00Z"
	if _, err = s.PatchTaskState(ctx, task.ID, TaskPatch{DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	primary, err := s.CreateLinkedReminder(ctx, "Prepare", "2030-01-10T16:00:00Z", "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.CreateLinkedReminder(ctx, "Charge laptop", "2030-01-09T17:00:00Z", "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, changes, err := s.ReminderMutationWorkflow(ctx, primary.ID, "scheduled", "2030-01-10T18:00:00Z", true)
	if err != nil || len(changes) != 3 || changes[0].Derived || !changes[1].Derived || !changes[2].Derived {
		t.Fatal(changes, err)
	}
	state, err := s.TaskState(ctx, task.ID)
	if err != nil || state.Task.DueAt != instantMust(t, "2030-01-10T19:00:00Z") {
		t.Fatal(state, err)
	}
	byID := map[string]Reminder{}
	for _, reminder := range state.Reminders {
		byID[reminder.ID] = reminder
	}
	if byID[primary.ID].ScheduledAt != instantMust(t, "2030-01-10T18:00:00Z") || byID[sibling.ID].ScheduledAt != instantMust(t, "2030-01-09T19:00:00Z") {
		t.Fatal(byID)
	}
	newDue := "2030-01-10T20:00:00Z"
	state, err = s.PatchTaskState(ctx, task.ID, TaskPatch{DueAt: &newDue})
	if err != nil || state.Task.DueAt != instantMust(t, newDue) {
		t.Fatal(state, err)
	}
	byID = map[string]Reminder{}
	for _, reminder := range state.Reminders {
		byID[reminder.ID] = reminder
	}
	if byID[primary.ID].ScheduledAt != instantMust(t, "2030-01-10T19:00:00Z") || byID[sibling.ID].ScheduledAt != instantMust(t, "2030-01-09T20:00:00Z") {
		t.Fatal(byID)
	}
}

func TestLinkedScheduleWorkflowRollsBackEveryChange(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	task, err := s.Create(ctx, "Rollback interview")
	if err != nil {
		t.Fatal(err)
	}
	due := "2030-02-10T17:00:00Z"
	if _, err = s.PatchTaskState(ctx, task.ID, TaskPatch{DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	reminder, err := s.CreateLinkedReminder(ctx, "Rollback reminder", "2030-02-10T16:00:00Z", "UTC", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_linked_deadline BEFORE UPDATE OF due_at ON tasks BEGIN SELECT RAISE(ABORT,'test'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ReminderMutationWorkflow(ctx, reminder.ID, "scheduled", "2030-02-10T18:00:00Z", true); err == nil {
		t.Fatal("expected linked workflow rollback")
	}
	state, err := s.ReminderState(ctx, reminder.ID)
	if err != nil || state.Reminder.ScheduledAt != instantMust(t, "2030-02-10T16:00:00Z") || state.Task == nil || state.Task.DueAt != instantMust(t, due) {
		t.Fatal(state, err)
	}
	if len(state.Deliveries) != 1 || state.Deliveries[0].State != "queued" || state.Deliveries[0].ScheduledAt != state.Reminder.ScheduledAt {
		t.Fatal(state.Deliveries)
	}
}

func instantMust(t *testing.T, value string) string {
	t.Helper()
	parsed, err := reminderTime(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
