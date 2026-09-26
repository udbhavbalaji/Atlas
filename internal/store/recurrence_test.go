package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func parseInstant(t *testing.T, s string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCalendarRecurrence(t *testing.T) {
	for _, tc := range []struct{ anchor, repeat, zone, after, want string }{
		{"2026-03-07T02:30:00-05:00", "daily", "America/New_York", "2026-03-07T12:00:00Z", "2026-03-09T06:30:00Z"},
		{"2026-10-31T01:30:00-04:00", "daily", "America/New_York", "2026-10-31T12:00:00Z", "2026-11-01T05:30:00Z"},
		{"2026-10-31T01:30:00-04:00", "daily", "America/New_York", "2026-11-01T06:00:00Z", "2026-11-02T06:30:00Z"},
		{"2026-03-01T09:00:00-05:00", "weekly", "America/New_York", "2026-03-01T15:00:00Z", "2026-03-08T13:00:00Z"},
		{"2026-01-01T09:00:00+05:30", "daily", "Asia/Kolkata", "2026-02-12T12:00:00Z", "2026-02-13T03:30:00Z"},
		{"2026-04-04T02:15:00+11:00", "daily", "Australia/Lord_Howe", "2026-04-04T00:00:00Z", "2026-04-04T15:45:00Z"},
	} {
		got, e := nextRepeat(tc.anchor, tc.repeat, tc.zone, parseInstant(t, tc.after))
		if e != nil || got != instant(parseInstant(t, tc.want)) {
			t.Fatal(tc, got, e)
		}
	}
}
func TestRecurringLifecycleDurabilityAndRetries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	at := parseInstant(t, "2030-01-01T09:00:00Z")
	r, _, e := s.CreateRepeatingReminderRequest(ctx, "repeat-key", "Daily", instant(at), "UTC", "", "daily")
	if e != nil {
		t.Fatal(e)
	}
	if r.OccurrenceID == "" {
		t.Fatal("missing occurrence")
	}
	if e = s.ProcessDue(ctx, at.Add(72*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.finishOccurrenceAt(ctx, r.ID, "wrong", "complete", at); !errors.Is(e, ErrOccurrenceConflict) {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e := s.finishOccurrenceAt(ctx, r.ID, r.OccurrenceID, "complete", at.Add(72*time.Hour))
	if e != nil || state.Reminder.ScheduledAt != instant(at.Add(96*time.Hour)) || len(state.Deliveries) != 2 {
		t.Fatal(state, e)
	}
	for range 2 {
		again, e := s.finishOccurrenceAt(ctx, r.ID, r.OccurrenceID, "complete", at.Add(72*time.Hour))
		if e != nil || again.Reminder.OccurrenceID != state.Reminder.OccurrenceID || len(again.Deliveries) != 2 {
			t.Fatal(again, e)
		}
	}
	saved, replay, e := s.CreateRepeatingReminderRequest(ctx, "repeat-key", "Daily", instant(at), "UTC", "", "daily")
	if e != nil || !replay || saved != r {
		t.Fatal(saved, replay, e)
	}
	if _, _, e = s.CreateRepeatingReminderRequest(ctx, "repeat-key", "Daily", instant(at), "UTC", "", "weekly"); !errors.Is(e, ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	// Snooze moves the current delivery but preserves the original local clock.
	snoozed, e := s.ReminderMutation(ctx, r.ID, "scheduled", instant(at.Add(100*time.Hour)))
	if e != nil || snoozed.Reminder.RepeatAnchor != r.RepeatAnchor {
		t.Fatal(snoozed, e)
	}
	if e = s.ProcessDue(ctx, at.Add(100*time.Hour)); e != nil {
		t.Fatal(e)
	}
	state, e = s.finishOccurrenceAt(ctx, r.ID, snoozed.Reminder.OccurrenceID, "dismiss", at.Add(100*time.Hour))
	if e != nil || state.Reminder.ScheduledAt != instant(at.Add(120*time.Hour)) {
		t.Fatal(state, e)
	}
	if e = s.CompleteReminder(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(200*time.Hour)); e != nil {
		t.Fatal(e)
	}
	final, e := s.ReminderState(ctx, r.ID)
	if e != nil || final.Reminder.Status != "completed" {
		t.Fatal(final, e)
	}
}
func TestOccurrenceRollbackAndTaskCancellation(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now().UTC()
	task, e := s.Create(ctx, "Linked")
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := s.CreateRepeatingReminderRequest(ctx, "", "Repeat", instant(at), "UTC", task.ID, "weekly")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`CREATE TRIGGER reject_repeat BEFORE INSERT ON activity WHEN NEW.action='reminder.occurrence.complete' BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.finishOccurrenceAt(ctx, r.ID, r.OccurrenceID, "complete", at); e == nil {
		t.Fatal("expected rollback")
	}
	state, e := s.ReminderState(ctx, r.ID)
	if e != nil || state.Reminder.Status != "due" || len(state.Deliveries) != 1 || state.Deliveries[0].State != "delivered" {
		t.Fatal(state, e)
	}
	if e = s.Complete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.finishOccurrenceAt(ctx, r.ID, r.OccurrenceID, "complete", at); e != nil {
		t.Fatal(e)
	}
	state, e = s.ReminderState(ctx, r.ID)
	if e != nil || state.Reminder.Status != "completed" || len(state.Deliveries) != 1 {
		t.Fatal(state, e)
	}
}

func TestMigrationFromFivePreservesOneTimeReminder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,details TEXT NOT NULL DEFAULT '',due_at TEXT NOT NULL DEFAULT ''); CREATE TABLE activity(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,action TEXT NOT NULL,timestamp TEXT NOT NULL);` + reminderMigration + taskReminderMigration + requestMigration + `INSERT INTO reminders(id,title,status,scheduled_at,timezone,created_at,updated_at) VALUES('old','Old reminder','due','2020-01-01T09:00:00Z','UTC','2020-01-01T09:00:00Z','2020-01-01T09:00:00Z'); INSERT INTO deliveries VALUES('old-occurrence','old','2020-01-01T09:00:00Z','delivered','2020-01-01T09:00:00Z');`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e := s.ReminderState(context.Background(), "old")
	if e != nil || state.Reminder.Repeat != "" || state.Reminder.OccurrenceID != "old-occurrence" || len(state.Deliveries) != 1 {
		t.Fatal(state, e)
	}
	if e = s.DismissReminder(context.Background(), "old"); e != nil {
		t.Fatal(e)
	}
}
