package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
)

const requestMigration = `CREATE TABLE request_receipts(operation TEXT NOT NULL,request_key TEXT NOT NULL,fingerprint TEXT NOT NULL,response TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(operation,request_key)); PRAGMA user_version=5;`

var ErrIdempotencyConflict = errors.New("idempotency key was already used with different input")
var ErrInvalidKey = errors.New("idempotency key must be 1 to 128 ASCII letters, digits, dots, underscores, or hyphens")
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func ValidKey(key string) bool { return key == "" || keyPattern.MatchString(key) }
func fingerprint(values ...string) string {
	b, _ := json.Marshal(values)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func replayReceipt(ctx context.Context, tx *sql.Tx, operation, key, hash string, out any) (bool, error) {
	if key == "" {
		return false, nil
	}
	if !ValidKey(key) {
		return false, ErrInvalidKey
	}
	var savedHash, response string
	err := tx.QueryRowContext(ctx, "SELECT fingerprint,response FROM request_receipts WHERE operation=? AND request_key=?", operation, key).Scan(&savedHash, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if savedHash != hash {
		return false, ErrIdempotencyConflict
	}
	return true, json.Unmarshal([]byte(response), out)
}
func saveReceipt(ctx context.Context, tx *sql.Tx, operation, key, hash string, value any) error {
	if key == "" {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO request_receipts VALUES(?,?,?,?,?)", operation, key, hash, string(b), now())
	return err
}

type TaskAction struct {
	TaskID     string     `json:"task_id"`
	Task       *Task      `json:"task"`
	Deleted    bool       `json:"deleted"`
	Reminders  []Reminder `json:"reminders"`
	Deliveries []Delivery `json:"deliveries"`
}
type ReminderAction struct {
	Reminder   Reminder   `json:"reminder"`
	Task       *Task      `json:"task"`
	Deliveries []Delivery `json:"deliveries"`
}

const taskColumns = "id,title,status,created_at,updated_at,details,due_at"
const reminderColumns = "id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title,cancellation_reason"

func readTask(ctx context.Context, tx *sql.Tx, id string) (Task, error) {
	var t Task
	err := tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=?", id).Scan(&t.ID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.Details, &t.DueAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return t, err
}
func readReminder(ctx context.Context, tx *sql.Tx, id string) (Reminder, error) {
	var r Reminder
	err := tx.QueryRowContext(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE id=?", id).Scan(&r.ID, &r.Title, &r.Status, &r.ScheduledAt, &r.Timezone, &r.CreatedAt, &r.UpdatedAt, &r.TaskID, &r.TaskTitle, &r.CancellationReason)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrReminderNotFound
	}
	return r, err
}
func readDeliveries(ctx context.Context, tx *sql.Tx, where string, id string) ([]Delivery, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,reminder_id,scheduled_at,state,delivered_at FROM deliveries WHERE "+where+" ORDER BY scheduled_at,id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err = rows.Scan(&d.ID, &d.ReminderID, &d.ScheduledAt, &d.State, &d.DeliveredAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}
func taskAction(ctx context.Context, tx *sql.Tx, id string, deleted bool) (TaskAction, error) {
	result := TaskAction{TaskID: id, Deleted: deleted, Reminders: []Reminder{}, Deliveries: []Delivery{}}
	if !deleted {
		t, err := readTask(ctx, tx, id)
		if err != nil {
			return result, err
		}
		result.Task = &t
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE task_id=? ORDER BY scheduled_at,id", id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var r Reminder
		if err = rows.Scan(&r.ID, &r.Title, &r.Status, &r.ScheduledAt, &r.Timezone, &r.CreatedAt, &r.UpdatedAt, &r.TaskID, &r.TaskTitle, &r.CancellationReason); err != nil {
			rows.Close()
			return result, err
		}
		result.Reminders = append(result.Reminders, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Deliveries, err = readDeliveries(ctx, tx, "reminder_id IN (SELECT id FROM reminders WHERE task_id=?)", id)
	return result, err
}
func reminderAction(ctx context.Context, tx *sql.Tx, id string) (ReminderAction, error) {
	r, err := readReminder(ctx, tx, id)
	if err != nil {
		return ReminderAction{}, err
	}
	result := ReminderAction{Reminder: r}
	if r.TaskID != "" {
		t, e := readTask(ctx, tx, r.TaskID)
		if e == nil {
			result.Task = &t
		} else if !errors.Is(e, ErrNotFound) {
			return result, e
		}
	}
	result.Deliveries, err = readDeliveries(ctx, tx, "reminder_id=?", id)
	return result, err
}
func (s *Store) TaskState(ctx context.Context, id string) (TaskAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskAction{}, err
	}
	defer tx.Rollback()
	v, err := taskAction(ctx, tx, id, false)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
func (s *Store) ReminderState(ctx context.Context, id string) (ReminderAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReminderAction{}, err
	}
	defer tx.Rollback()
	v, err := reminderAction(ctx, tx, id)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
