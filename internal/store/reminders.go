package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const reminderMigration = `
CREATE TABLE reminders(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('scheduled','due','dismissed','completed')),scheduled_at TEXT NOT NULL,timezone TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE deliveries(id TEXT PRIMARY KEY,reminder_id TEXT NOT NULL REFERENCES reminders(id),scheduled_at TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('queued','delivered','acknowledged','cancelled')),delivered_at TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX one_queued_delivery ON deliveries(reminder_id) WHERE state='queued';
CREATE INDEX due_deliveries ON deliveries(state,scheduled_at);
ALTER TABLE activity ADD COLUMN reminder_id TEXT NOT NULL DEFAULT '';
PRAGMA user_version=3;`

const taskReminderMigration = `ALTER TABLE reminders ADD COLUMN task_id TEXT NOT NULL DEFAULT ''; ALTER TABLE reminders ADD COLUMN task_title TEXT NOT NULL DEFAULT ''; ALTER TABLE reminders ADD COLUMN cancellation_reason TEXT NOT NULL DEFAULT ''; CREATE INDEX task_reminders ON reminders(task_id,status); PRAGMA user_version=4;`

var ErrTaskReminderConflict = errors.New("reminders can only be linked to open tasks")
var ErrInvalidReminder = errors.New("provide a title, an RFC3339 scheduled_at with timezone, and a valid IANA timezone")
var ErrReminderNotFound = errors.New("reminder not found")
var ErrReminderConflict = errors.New("completed reminders cannot be rescheduled or dismissed; only due reminders can be dismissed")
var ErrSnoozeTime = errors.New("snooze time must be in the future")

type Reminder struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	TaskID             string `json:"task_id"`
	TaskTitle          string `json:"task_title"`
	CancellationReason string `json:"cancellation_reason"`
	Status             string `json:"status"`
	ScheduledAt        string `json:"scheduled_at"`
	Timezone           string `json:"timezone"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}
type Delivery struct {
	ID          string `json:"id"`
	ReminderID  string `json:"reminder_id"`
	ScheduledAt string `json:"scheduled_at"`
	State       string `json:"state"`
	DeliveredAt string `json:"delivered_at"`
}

func newID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func instant(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func reminderTime(value string) (string, error) {
	_, value, err := validateFields("", value)
	if err != nil || value == "" {
		return "", ErrInvalidReminder
	}
	return value, nil
}
func reminderActivity(ctx context.Context, tx *sql.Tx, id, action, timestamp string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO activity(task_id,reminder_id,action,timestamp) VALUES('',?,?,?)", id, action, timestamp)
	return err
}
func queueDelivery(ctx context.Context, tx *sql.Tx, id, scheduled string) error {
	deliveryID, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO deliveries(id,reminder_id,scheduled_at,state) VALUES(?,?,?,'queued')", deliveryID, id, scheduled)
	return err
}
func (s *Store) CreateReminder(ctx context.Context, title, scheduled, timezone string) (Reminder, error) {
	return s.CreateLinkedReminder(ctx, title, scheduled, timezone, "")
}
func (s *Store) CreateLinkedReminder(ctx context.Context, title, scheduled, timezone, taskID string) (Reminder, error) {
	title = strings.TrimSpace(title)
	if len([]rune(title)) == 0 || len([]rune(title)) > 500 {
		return Reminder{}, ErrInvalid
	}
	scheduled, err := reminderTime(scheduled)
	if err != nil {
		return Reminder{}, err
	}
	if timezone == "" || timezone == "Local" {
		return Reminder{}, ErrInvalidReminder
	}
	if _, err = time.LoadLocation(timezone); err != nil {
		return Reminder{}, ErrInvalidReminder
	}
	id, err := newID()
	if err != nil {
		return Reminder{}, err
	}
	r := Reminder{ID: id, Title: title, Status: "scheduled", ScheduledAt: scheduled, Timezone: timezone, CreatedAt: now()}
	r.UpdatedAt = r.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback()
	if taskID != "" {
		var taskStatus string
		err = tx.QueryRowContext(ctx, "SELECT title,status FROM tasks WHERE id=?", taskID).Scan(&r.TaskTitle, &taskStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return Reminder{}, ErrNotFound
		}
		if err != nil {
			return Reminder{}, err
		}
		if taskStatus != "open" {
			return Reminder{}, ErrTaskReminderConflict
		}
		r.TaskID = taskID
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO reminders(id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title) VALUES(?,?,?,?,?,?,?,?,?)", r.ID, r.Title, r.Status, r.ScheduledAt, r.Timezone, r.CreatedAt, r.UpdatedAt, r.TaskID, r.TaskTitle)
	if err == nil {
		err = queueDelivery(ctx, tx, id, scheduled)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, "reminder.scheduled", r.CreatedAt)
	}
	if err == nil {
		err = tx.Commit()
	}
	return r, err
}
func (s *Store) Reminders(ctx context.Context) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title,cancellation_reason FROM reminders ORDER BY scheduled_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Reminder{}
	for rows.Next() {
		var r Reminder
		if err = rows.Scan(&r.ID, &r.Title, &r.Status, &r.ScheduledAt, &r.Timezone, &r.CreatedAt, &r.UpdatedAt, &r.TaskID, &r.TaskTitle, &r.CancellationReason); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Store) Deliveries(ctx context.Context) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,reminder_id,scheduled_at,state,delivered_at FROM deliveries ORDER BY scheduled_at DESC,id LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err = rows.Scan(&d.ID, &d.ReminderID, &d.ScheduledAt, &d.State, &d.DeliveredAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// ProcessDue publishes into the durable webpage inbox in the same transaction as
// delivery state and activity. No external side effect occurs before commit.
func (s *Store) ProcessDue(ctx context.Context, at time.Time) error {
	timestamp := instant(at)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.reminder_id FROM deliveries d JOIN reminders r ON r.id=d.reminder_id WHERE d.state='queued' AND r.status='scheduled' AND r.scheduled_at=d.scheduled_at AND (r.task_id='' OR EXISTS(SELECT 1 FROM tasks t WHERE t.id=r.task_id AND t.status='open')) AND d.scheduled_at<=? ORDER BY d.scheduled_at LIMIT 100`, timestamp)
	if err != nil {
		return err
	}
	type item struct{ id, reminder string }
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.reminder); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET state='delivered',delivered_at=? WHERE id=?", timestamp, i.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE reminders SET status='due',updated_at=? WHERE id=?", timestamp, i.reminder); err != nil {
			return err
		}
		if err = reminderActivity(ctx, tx, i.reminder, "reminder.delivered", timestamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SnoozeReminder(ctx context.Context, id, scheduled string) error {
	scheduled, err := reminderTime(scheduled)
	if err != nil {
		return err
	}
	if scheduled <= now() {
		return ErrSnoozeTime
	}
	return s.changeReminder(ctx, id, "scheduled", scheduled)
}
func (s *Store) CompleteReminder(ctx context.Context, id string) error {
	return s.changeReminder(ctx, id, "completed", "")
}
func (s *Store) DismissReminder(ctx context.Context, id string) error {
	return s.changeReminder(ctx, id, "dismissed", "")
}
func (s *Store) changeReminder(ctx context.Context, id, target, scheduled string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, "SELECT status FROM reminders WHERE id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReminderNotFound
	}
	if err != nil {
		return err
	}
	if status == target && target != "scheduled" {
		return tx.Commit()
	}
	if status == "completed" || (target == "dismissed" && status != "due") {
		return ErrReminderConflict
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "UPDATE deliveries SET state=CASE WHEN state='queued' THEN 'cancelled' ELSE 'acknowledged' END WHERE reminder_id=? AND state IN ('queued','delivered')", id)
	if err != nil {
		return err
	}
	action := "reminder." + target
	if target == "scheduled" {
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status='scheduled',scheduled_at=?,updated_at=? WHERE id=?", scheduled, timestamp, id)
		if err == nil {
			err = queueDelivery(ctx, tx, id, scheduled)
		}
		action = "reminder.snoozed"
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status=?,updated_at=? WHERE id=?", target, timestamp, id)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, action, timestamp)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}

// Task changes and linked delivery cancellation share the caller's transaction.
// Keep reminder records and task title snapshots for inspection after deletion.
func cancelTaskReminders(ctx context.Context, tx *sql.Tx, taskID, reason, timestamp string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM reminders WHERE task_id=? AND status!='completed'", taskID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		_, err = tx.ExecContext(ctx, "UPDATE deliveries SET state=CASE WHEN state='queued' THEN 'cancelled' ELSE 'acknowledged' END WHERE reminder_id=? AND state IN ('queued','delivered')", id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status='completed',cancellation_reason=?,updated_at=? WHERE id=?", reason, timestamp, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,reminder_id,action,timestamp) VALUES(?,?,?,?)", taskID, id, "reminder.cancelled."+reason, timestamp)
		if err != nil {
			return err
		}
	}
	return nil
}
