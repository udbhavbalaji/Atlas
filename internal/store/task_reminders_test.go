package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkedTaskReminderLifecycle(t *testing.T) {
	for _, operation := range []string{"complete", "patch", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "db")
			s, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			task, e := s.Create(ctx, "Linked task")
			if e != nil {
				t.Fatal(e)
			}
			at := time.Now()
			for _, offset := range []time.Duration{0, time.Hour} {
				if _, e = s.CreateLinkedReminder(ctx, "Reminder", instant(at.Add(offset)), "UTC", task.ID); e != nil {
					t.Fatal(e)
				}
			}
			standalone, e := s.CreateReminder(ctx, "Standalone", instant(at.Add(time.Hour)), "UTC")
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ProcessDue(ctx, at.Add(time.Second)); e != nil {
				t.Fatal(e)
			}
			s.Close()
			s, e = Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			switch operation {
			case "complete":
				e = s.Complete(ctx, task.ID)
			case "patch":
				status := "completed"
				_, e = s.Patch(ctx, task.ID, TaskPatch{Status: &status})
			case "delete":
				e = s.Delete(ctx, task.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ProcessDue(ctx, at.Add(2*time.Hour)); e != nil {
				t.Fatal(e)
			}
			reminders, e := s.Reminders(ctx)
			if e != nil {
				t.Fatal(e)
			}
			for _, r := range reminders {
				if r.ID == standalone.ID {
					if r.Status != "due" {
						t.Fatal(r)
					}
					continue
				}
				reason := "task.completed"
				if operation == "delete" {
					reason = "task.deleted"
				}
				if r.Status != "completed" || r.CancellationReason != reason || r.TaskID != task.ID || r.TaskTitle != "Linked task" {
					t.Fatal(r)
				}
				if e = s.SnoozeReminder(ctx, r.ID, instant(at.Add(3*time.Hour))); !errors.Is(e, ErrReminderConflict) {
					t.Fatal(e)
				}
			}
			deliveries, e := s.Deliveries(ctx)
			if e != nil {
				t.Fatal(e)
			}
			counts := map[string]int{}
			for _, d := range deliveries {
				counts[d.State]++
			}
			if counts["cancelled"] != 1 || counts["acknowledged"] != 1 || counts["delivered"] != 1 {
				t.Fatal(counts)
			}
			if operation != "delete" {
				status := "open"
				if _, e = s.Patch(ctx, task.ID, TaskPatch{Status: &status}); e != nil {
					t.Fatal(e)
				}
				if _, e = s.CreateLinkedReminder(ctx, "New reminder", instant(at.Add(4*time.Hour)), "UTC", task.ID); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestLinkedReminderValidationAndRollback(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now().Add(time.Hour)
	if _, e = s.CreateLinkedReminder(ctx, "x", instant(at), "UTC", "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	task, e := s.Create(ctx, "Atomic completion")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateLinkedReminder(ctx, "x", instant(at), "UTC", task.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`CREATE TRIGGER reject_cancellation BEFORE INSERT ON activity WHEN NEW.action LIKE 'reminder.cancelled.%' BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Complete(ctx, task.ID); e == nil {
		t.Fatal("expected rollback")
	}
	status := "completed"
	if _, e = s.Patch(ctx, task.ID, TaskPatch{Status: &status}); e == nil {
		t.Fatal("expected patch rollback")
	}
	if e = s.Delete(ctx, task.ID); e == nil {
		t.Fatal("expected delete rollback")
	}
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 || tasks[0].Status != "open" {
		t.Fatal(tasks, e)
	}
	reminders, e := s.Reminders(ctx)
	if e != nil || reminders[0].Status != "scheduled" {
		t.Fatal(reminders, e)
	}
	deliveries, e := s.Deliveries(ctx)
	if e != nil || deliveries[0].State != "queued" {
		t.Fatal(deliveries, e)
	}
	if _, e = s.db.Exec("DROP TRIGGER reject_cancellation"); e != nil {
		t.Fatal(e)
	}
	if e = s.Complete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateLinkedReminder(ctx, "x", instant(at), "UTC", task.ID); !errors.Is(e, ErrTaskReminderConflict) {
		t.Fatal(e)
	}
	if e = s.SnoozeReminder(ctx, r.ID, instant(at)); !errors.Is(e, ErrReminderConflict) {
		t.Fatal(e)
	}
}

func TestLinkCreationAndTaskCompletionRace(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.Create(ctx, "Race")
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().Add(time.Hour)
	result := make(chan error, 2)
	go func() { _, err := s.CreateLinkedReminder(ctx, "x", instant(at), "UTC", task.ID); result <- err }()
	go func() { result <- s.Complete(ctx, task.ID) }()
	for range 2 {
		if e = <-result; e != nil && !errors.Is(e, ErrTaskReminderConflict) {
			t.Fatal(e)
		}
	}
	if e = s.ProcessDue(ctx, at.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	list, e := s.Reminders(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range list {
		if r.Status != "completed" {
			t.Fatal(r)
		}
	}
	d, e := s.Deliveries(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, delivery := range d {
		if delivery.State != "cancelled" {
			t.Fatal(delivery)
		}
	}
}

func TestVersionThreeLinkMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,details TEXT NOT NULL DEFAULT '',due_at TEXT NOT NULL DEFAULT '');CREATE TABLE activity(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,action TEXT NOT NULL,timestamp TEXT NOT NULL);`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(reminderMigration); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO tasks VALUES('old-task','Existing task','open','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','Existing details','');INSERT INTO reminders VALUES('old-reminder','Standalone','scheduled','2030-01-01T00:00:00.000000000Z','UTC','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');INSERT INTO deliveries VALUES('old-delivery','old-reminder','2030-01-01T00:00:00.000000000Z','queued','');`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	tasks, e := s.Tasks(ctx)
	if e != nil || len(tasks) != 1 || tasks[0].Details != "Existing details" {
		t.Fatal(tasks, e)
	}
	r, e := s.Reminders(ctx)
	if e != nil || len(r) != 1 || r[0].TaskID != "" || r[0].CancellationReason != "" {
		t.Fatal(r, e)
	}
	if e = s.ProcessDue(ctx, time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)); e != nil {
		t.Fatal(e)
	}
	d, e := s.Deliveries(ctx)
	if e != nil || len(d) != 1 || d[0].ID != "old-delivery" || d[0].State != "delivered" {
		t.Fatal(d, e)
	}
}
