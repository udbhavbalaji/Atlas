package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReminderRestartSnoozeAndComplete(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atlas.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC()
	r, e := s.CreateReminder(ctx, "Call Mom", instant(at), "Asia/Kolkata")
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for range 2 {
		if e = s.ProcessDue(ctx, at.Add(time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	deliveries, e := s.Deliveries(ctx)
	if e != nil || len(deliveries) != 1 || deliveries[0].State != "delivered" {
		t.Fatal(deliveries, e)
	}
	activity, e := s.Activity(ctx)
	if e != nil || len(activity) != 2 {
		t.Fatal(activity, e)
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(30*time.Minute)); e != nil {
		t.Fatal(e)
	}
	reminders, e := s.Reminders(ctx)
	if e != nil || reminders[0].Status != "scheduled" {
		t.Fatal(reminders, e)
	}
	if e = s.ProcessDue(ctx, at.Add(2*time.Hour)); e != nil {
		t.Fatal(e)
	}
	deliveries, e = s.Deliveries(ctx)
	if e != nil || len(deliveries) != 2 {
		t.Fatal(deliveries, e)
	}
	states := map[string]int{}
	for _, d := range deliveries {
		states[d.State]++
	}
	if states["delivered"] != 1 || states["acknowledged"] != 1 {
		t.Fatal(states)
	}
	for range 2 {
		if e = s.CompleteReminder(ctx, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(3*time.Hour))); !errors.Is(e, ErrReminderConflict) {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(4*time.Hour)); e != nil {
		t.Fatal(e)
	}
	reminders, e = s.Reminders(ctx)
	if e != nil || reminders[0].Status != "completed" {
		t.Fatal(reminders, e)
	}
}
func TestReminderCancelAndDismiss(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now()
	r, e := s.CreateReminder(ctx, "Cancel", instant(at.Add(time.Hour)), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DismissReminder(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteReminder(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(2*time.Hour)); e != nil {
		t.Fatal(e)
	}
	d, e := s.Deliveries(ctx)
	if e != nil || len(d) != 1 || d[0].State != "cancelled" {
		t.Fatal(d, e)
	}
	r, e = s.CreateReminder(ctx, "Dismiss", instant(at), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = s.DismissReminder(ctx, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(-time.Hour))); !errors.Is(e, ErrSnoozeTime) {
		t.Fatal(e)
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteReminder(ctx, "missing"); !errors.Is(e, ErrReminderNotFound) {
		t.Fatal(e)
	}
	for _, zone := range []string{"", "Local", "Invalid/Zone"} {
		if _, e = s.CreateReminder(ctx, "x", instant(at), zone); !errors.Is(e, ErrInvalidReminder) {
			t.Fatal(zone, e)
		}
	}
}
func TestReminderDeliveryRollbackAndConcurrentTicks(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now()
	_, e = s.CreateReminder(ctx, "Deliver once", instant(at), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_delivery_log BEFORE INSERT ON activity WHEN NEW.action='reminder.delivered' BEGIN SELECT RAISE(ABORT,'test failure'); END;`); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, at.Add(time.Second)); e == nil {
		t.Fatal("expected rollback")
	}
	d, e := s.Deliveries(ctx)
	if e != nil || d[0].State != "queued" {
		t.Fatal(d, e)
	}
	if _, e = s.db.Exec("DROP TRIGGER reject_delivery_log"); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- s.ProcessDue(ctx, at.Add(time.Second)) }()
	}
	for range 8 {
		if e = <-results; e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.Activity(ctx)
	if e != nil || len(a) != 2 {
		t.Fatal(a, e)
	}
}
func TestSnoozeRollback(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now()
	r, e := s.CreateReminder(ctx, "Keep delivery", instant(at.Add(time.Hour)), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DROP TABLE activity"); e != nil {
		t.Fatal(e)
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(2*time.Hour))); e == nil {
		t.Fatal("expected rollback")
	}
	d, e := s.Deliveries(ctx)
	if e != nil || len(d) != 1 || d[0].State != "queued" || d[0].ScheduledAt != r.ScheduledAt {
		t.Fatal(d, e)
	}
}

func TestReminderTimezoneOffsets(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// The two occurrences of 1:30 AM during DST fall-back are separate instants.
	for _, input := range []string{"2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"} {
		if _, e = s.CreateReminder(ctx, "DST occurrence", input, "America/New_York"); e != nil {
			t.Fatal(e)
		}
	}
	tick, e := time.Parse(time.RFC3339, "2026-11-01T06:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(ctx, tick); e != nil {
		t.Fatal(e)
	}
	list, e := s.Reminders(ctx)
	if e != nil || len(list) != 2 || list[0].Status != "due" || list[1].Status != "scheduled" {
		t.Fatal(list, e)
	}
}
