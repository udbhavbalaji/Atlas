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
	Repeat             string `json:"repeat"`
	RepeatAnchor       string `json:"repeat_anchor"`
	OccurrenceID       string `json:"occurrence_id"`
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

// WorkflowChange reports every schedule field changed by one atomic linked
// workflow. Derived changes are relationship effects rather than fields the
// caller named directly.
type WorkflowChange struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Field   string `json:"field"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Derived bool   `json:"derived"`
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
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET occurrence_id=? WHERE id=?", deliveryID, id)
	}
	return err
}
func (s *Store) CreateReminder(ctx context.Context, title, scheduled, timezone string) (Reminder, error) {
	return s.CreateLinkedReminder(ctx, title, scheduled, timezone, "")
}
func (s *Store) CreateLinkedReminder(ctx context.Context, title, scheduled, timezone, taskID string) (Reminder, error) {
	r, _, err := s.CreateReminderRequest(ctx, "", title, scheduled, timezone, taskID)
	return r, err
}
func (s *Store) CreateReminderRequest(ctx context.Context, key, title, scheduled, timezone, taskID string) (Reminder, bool, error) {
	return s.CreateRepeatingReminderRequest(ctx, key, title, scheduled, timezone, taskID, "")
}
func (s *Store) CreateRepeatingReminderRequest(ctx context.Context, key, title, scheduled, timezone, taskID, repeat string) (Reminder, bool, error) {
	if repeat != "" && repeat != "daily" && repeat != "weekly" {
		return Reminder{}, false, ErrInvalidRepeat
	}
	hash := fingerprint(title, scheduled, timezone, taskID)
	if repeat != "" {
		hash = fingerprint(title, scheduled, timezone, taskID, repeat)
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) == 0 || len([]rune(title)) > 500 {
		return Reminder{}, false, ErrInvalid
	}
	scheduled, err := reminderTime(scheduled)
	if err != nil {
		return Reminder{}, false, err
	}
	if timezone == "" || timezone == "Local" {
		return Reminder{}, false, ErrInvalidReminder
	}
	if _, err = time.LoadLocation(timezone); err != nil {
		return Reminder{}, false, ErrInvalidReminder
	}
	id, err := newID()
	if err != nil {
		return Reminder{}, false, err
	}
	r := Reminder{ID: id, Title: title, Status: "scheduled", ScheduledAt: scheduled, Timezone: timezone, CreatedAt: now()}
	r.UpdatedAt = r.CreatedAt
	r.Repeat = repeat
	if repeat != "" {
		r.RepeatAnchor = scheduled
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Reminder{}, false, err
	}
	defer tx.Rollback()
	var saved Reminder
	replay, err := replayReceipt(ctx, tx, "reminders.create", key, hash, &saved)
	if err != nil {
		return Reminder{}, false, err
	}
	if replay {
		return saved, true, tx.Commit()
	}
	if taskID != "" {
		var taskStatus string
		err = tx.QueryRowContext(ctx, "SELECT title,status FROM tasks WHERE id=?", taskID).Scan(&r.TaskTitle, &taskStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return Reminder{}, false, ErrNotFound
		}
		if err != nil {
			return Reminder{}, false, err
		}
		if taskStatus != "open" {
			return Reminder{}, false, ErrTaskReminderConflict
		}
		r.TaskID = taskID
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO reminders(id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title,repeat,repeat_anchor) VALUES(?,?,?,?,?,?,?,?,?,?,?)", r.ID, r.Title, r.Status, r.ScheduledAt, r.Timezone, r.CreatedAt, r.UpdatedAt, r.TaskID, r.TaskTitle, r.Repeat, r.RepeatAnchor)
	if err == nil {
		err = queueDelivery(ctx, tx, id, scheduled)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, "reminder.scheduled", r.CreatedAt)
	}
	if err == nil {
		r, err = readReminder(ctx, tx, id)
		if err == nil {
			err = saveReceipt(ctx, tx, "reminders.create", key, hash, r)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	return r, false, err
}
func (s *Store) Reminders(ctx context.Context) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+reminderColumns+" FROM reminders ORDER BY scheduled_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Reminder{}
	for rows.Next() {
		var r Reminder
		if err = rows.Scan(&r.ID, &r.Title, &r.Status, &r.ScheduledAt, &r.Timezone, &r.CreatedAt, &r.UpdatedAt, &r.TaskID, &r.TaskTitle, &r.CancellationReason, &r.Repeat, &r.RepeatAnchor, &r.OccurrenceID); err != nil {
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
	_, err := s.ReminderMutation(ctx, id, target, scheduled)
	return err
}
func (s *Store) ReminderMutation(ctx context.Context, id, target, scheduled string) (ReminderAction, error) {
	v, _, err := s.ReminderMutationWorkflow(ctx, id, target, scheduled, true)
	return v, err
}

// ReminderMutationWorkflow applies the requested reminder transition and any
// required linked deadline movement in one transaction. A non-repeating
// reminder linked to an open task with a deadline forms a schedule group: when
// either side moves, the group keeps each reminder's existing offset.
func (s *Store) ReminderMutationWorkflow(ctx context.Context, id, target, scheduled string, propagate bool) (ReminderAction, []WorkflowChange, error) {
	if target != "scheduled" && target != "completed" && target != "dismissed" {
		return ReminderAction{}, nil, ErrReminderConflict
	}
	if target == "scheduled" {
		v, e := reminderTime(scheduled)
		if e != nil {
			return ReminderAction{}, nil, e
		}
		scheduled = v
		if scheduled <= now() {
			return ReminderAction{}, nil, ErrSnoozeTime
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReminderAction{}, nil, err
	}
	defer tx.Rollback()
	r, err := readReminder(ctx, tx, id)
	if err != nil {
		return ReminderAction{}, nil, err
	}
	if r.Repeat != "" && target == "dismissed" {
		return ReminderAction{}, nil, ErrOccurrenceConflict
	}
	if r.Status == target && target != "scheduled" {
		v, e := reminderAction(ctx, tx, id)
		if e != nil {
			return v, nil, e
		}
		return v, nil, tx.Commit()
	}
	if r.Status == "completed" || (target == "dismissed" && r.Status != "due") {
		return ReminderAction{}, nil, ErrReminderConflict
	}
	timestamp := now()
	changes := []WorkflowChange{}
	action := "reminder." + target
	if target == "scheduled" {
		err = rescheduleReminderInTransaction(ctx, tx, r, scheduled, timestamp)
		changes = append(changes, WorkflowChange{Kind: "reminder", ID: r.ID, Title: r.Title, Field: "scheduled_at", Old: r.ScheduledAt, New: scheduled})
		action = "reminder.snoozed"
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE deliveries SET state=CASE WHEN state='queued' THEN 'cancelled' ELSE 'acknowledged' END WHERE reminder_id=? AND state IN ('queued','delivered')", id)
		if err != nil {
			return ReminderAction{}, nil, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status=?,updated_at=? WHERE id=?", target, timestamp, id)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, action, timestamp)
	}
	if err == nil && target == "scheduled" && propagate && r.TaskID != "" && r.Repeat == "" {
		oldAt, oldErr := time.Parse(time.RFC3339Nano, r.ScheduledAt)
		newAt, newErr := time.Parse(time.RFC3339Nano, scheduled)
		if oldErr != nil || newErr != nil {
			err = ErrInvalidReminder
		} else {
			var derived []WorkflowChange
			derived, err = shiftLinkedTaskSchedule(ctx, tx, r.TaskID, r.ID, newAt.Sub(oldAt), timestamp)
			changes = append(changes, derived...)
		}
	}
	if err != nil {
		return ReminderAction{}, nil, err
	}
	v, err := reminderAction(ctx, tx, id)
	if err == nil {
		err = tx.Commit()
	}
	return v, changes, err
}

func rescheduleReminderInTransaction(ctx context.Context, tx *sql.Tx, r Reminder, scheduled, timestamp string) error {
	_, err := tx.ExecContext(ctx, "UPDATE deliveries SET state=CASE WHEN state='queued' THEN 'cancelled' ELSE 'acknowledged' END WHERE reminder_id=? AND state IN ('queued','delivered')", r.ID)
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status='scheduled',scheduled_at=?,updated_at=? WHERE id=?", scheduled, timestamp, r.ID)
	}
	if err == nil {
		err = queueDelivery(ctx, tx, r.ID, scheduled)
	}
	return err
}

func shiftLinkedTaskSchedule(ctx context.Context, tx *sql.Tx, taskID, exceptReminderID string, delta time.Duration, timestamp string) ([]WorkflowChange, error) {
	task, err := readTask(ctx, tx, taskID)
	if err != nil || task.Status != "open" || task.DueAt == "" || delta == 0 {
		return nil, err
	}
	oldDue, err := time.Parse(time.RFC3339Nano, task.DueAt)
	if err != nil {
		return nil, ErrInvalidFields
	}
	newDue := instant(oldDue.Add(delta))
	_, err = tx.ExecContext(ctx, "UPDATE tasks SET due_at=?,updated_at=? WHERE id=?", newDue, timestamp, taskID)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.updated',?)", taskID, timestamp)
	}
	if err != nil {
		return nil, err
	}
	changes := []WorkflowChange{{Kind: "task", ID: task.ID, Title: task.Title, Field: "due_at", Old: task.DueAt, New: newDue, Derived: true}}
	reminderChanges, err := shiftLinkedReminderSchedules(ctx, tx, taskID, exceptReminderID, delta, timestamp)
	if err != nil {
		return nil, err
	}
	return append(changes, reminderChanges...), nil
}

func shiftLinkedReminderSchedules(ctx context.Context, tx *sql.Tx, taskID, exceptReminderID string, delta time.Duration, timestamp string) ([]WorkflowChange, error) {
	reminders, err := linkedScheduleReminders(ctx, tx, taskID, exceptReminderID)
	if err != nil {
		return nil, err
	}
	changes := []WorkflowChange{}
	for _, sibling := range reminders {
		oldAt, parseErr := time.Parse(time.RFC3339Nano, sibling.ScheduledAt)
		if parseErr != nil {
			return nil, ErrInvalidReminder
		}
		newAt := instant(oldAt.Add(delta))
		if err = rescheduleReminderInTransaction(ctx, tx, sibling, newAt, timestamp); err == nil {
			err = reminderActivity(ctx, tx, sibling.ID, "reminder.snoozed", timestamp)
		}
		if err != nil {
			return nil, err
		}
		changes = append(changes, WorkflowChange{Kind: "reminder", ID: sibling.ID, Title: sibling.Title, Field: "scheduled_at", Old: sibling.ScheduledAt, New: newAt, Derived: true})
	}
	return changes, nil
}

func linkedScheduleReminders(ctx context.Context, tx *sql.Tx, taskID, exceptID string) ([]Reminder, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE task_id=? AND id<>? AND repeat='' AND status IN ('scheduled','due') ORDER BY scheduled_at,id", taskID, exceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Reminder{}
	for rows.Next() {
		var r Reminder
		if err = rows.Scan(&r.ID, &r.Title, &r.Status, &r.ScheduledAt, &r.Timezone, &r.CreatedAt, &r.UpdatedAt, &r.TaskID, &r.TaskTitle, &r.CancellationReason, &r.Repeat, &r.RepeatAnchor, &r.OccurrenceID); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// RenameReminder changes the label without disturbing its occurrence or delivery.
func (s *Store) RenameReminder(ctx context.Context, id, title string) (ReminderAction, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 500 {
		return ReminderAction{}, ErrInvalidReminder
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReminderAction{}, err
	}
	defer tx.Rollback()
	r, err := readReminder(ctx, tx, id)
	if err != nil {
		return ReminderAction{}, err
	}
	if r.Title != title {
		at := now()
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET title=?,updated_at=? WHERE id=?", title, at, id)
		if err == nil {
			err = reminderActivity(ctx, tx, id, "reminder.renamed", at)
		}
	}
	if err != nil {
		return ReminderAction{}, err
	}
	v, err := reminderAction(ctx, tx, id)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}

// DeleteReminder also cancels outstanding delivery, then records the deletion.
func (s *Store) DeleteReminder(ctx context.Context, id string) (ReminderAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReminderAction{}, err
	}
	defer tx.Rollback()
	if _, err = readReminder(ctx, tx, id); err != nil {
		return ReminderAction{}, err
	}
	// The delivery table has a restrictive foreign key to reminders.
	_, err = tx.ExecContext(ctx, "DELETE FROM deliveries WHERE reminder_id=?", id)
	if err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM reminders WHERE id=?", id)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, "reminder.deleted", now())
	}
	if err != nil {
		return ReminderAction{}, err
	}
	v := ReminderAction{ReminderID: id, Deleted: true}
	return v, tx.Commit()
}

// Task changes and linked delivery cancellation share the caller's transaction.
// Keep reminder records and task title snapshots for inspection after deletion.
func cancelTaskReminders(ctx context.Context, tx *sql.Tx, taskID, reason, timestamp string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM reminders WHERE task_id=? AND status IN ('scheduled','due')", taskID)
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

// SetReminderTask links an active reminder directly, independent of notes.
func (s *Store) SetReminderTask(ctx context.Context, id, taskID string) (ReminderAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReminderAction{}, err
	}
	defer tx.Rollback()
	r, err := readReminder(ctx, tx, id)
	if err != nil {
		return ReminderAction{}, err
	}
	if r.TaskID == taskID {
		v, e := reminderAction(ctx, tx, id)
		if e != nil {
			return v, e
		}
		return v, tx.Commit()
	}
	if r.Status == "completed" {
		return ReminderAction{}, ErrReminderConflict
	}
	title := ""
	if taskID != "" {
		task, e := readTask(ctx, tx, taskID)
		if e != nil {
			return ReminderAction{}, e
		}
		if task.Status != "open" {
			return ReminderAction{}, ErrTaskReminderConflict
		}
		title = task.Title
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "UPDATE reminders SET task_id=?,task_title=?,updated_at=? WHERE id=?", taskID, title, timestamp, id)
	action := "reminder.task_unlinked"
	if taskID != "" {
		action = "reminder.task_linked"
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, action, timestamp)
	}
	if err != nil {
		return ReminderAction{}, err
	}
	v, err := reminderAction(ctx, tx, id)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
