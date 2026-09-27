package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

var ErrInvalid = errors.New("title must contain 1 to 500 characters")
var ErrNotFound = errors.New("task not found")

type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Details   string `json:"details"`
	DueAt     string `json:"due_at"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}
type Activity struct {
	NoteID     string `json:"note_id"`
	ID         int64  `json:"id"`
	TaskID     string `json:"task_id"`
	ReminderID string `json:"reminder_id"`
	Action     string `json:"action"`
	Timestamp  string `json:"timestamp"`
}
type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	// DDL and version marker commit together. Reject newer schemas before changing them.
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err == nil && version > 8 {
		err = errors.New("database schema is newer than this Atlas version")
	}
	if err == nil && version == 0 {
		_, err = tx.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('open','completed')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE TABLE activity(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,action TEXT NOT NULL,timestamp TEXT NOT NULL);
 PRAGMA user_version=1;`)
		if err == nil {
			version = 1
		}
	}
	if err == nil && version == 1 {
		_, err = tx.Exec(`ALTER TABLE tasks ADD COLUMN details TEXT NOT NULL DEFAULT ''; ALTER TABLE tasks ADD COLUMN due_at TEXT NOT NULL DEFAULT ''; PRAGMA user_version=2;`)
		if err == nil {
			version = 2
		}
	}
	if err == nil && version == 2 {
		_, err = tx.Exec(reminderMigration)
		if err == nil {
			version = 3
		}
	}
	if err == nil && version == 3 {
		_, err = tx.Exec(taskReminderMigration)
		if err == nil {
			version = 4
		}
	}
	if err == nil && version == 4 {
		_, err = tx.Exec(requestMigration)
		if err == nil {
			version = 5
		}
	}
	if err == nil && version == 5 {
		_, err = tx.Exec(recurrenceMigration)
		if err == nil {
			version = 6
		}
	}
	if err == nil && version == 6 {
		_, err = tx.Exec(noteMigration)
		if err == nil {
			version = 7
		}
	}
	if err == nil && version == 7 {
		_, err = tx.Exec(dependencyMigration)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func now() string             { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (s *Store) Create(ctx context.Context, title string) (Task, error) {
	return s.CreateWithFields(ctx, title, "", "")
}
func (s *Store) CreateWithFields(ctx context.Context, title, details, dueAt string) (Task, error) {
	t, _, err := s.CreateTaskRequest(ctx, "", title, details, dueAt)
	return t, err
}
func (s *Store) CreateTaskRequest(ctx context.Context, key, title, details, dueAt string) (Task, bool, error) {
	hash := fingerprint(title, details, dueAt)
	var err error
	details, dueAt, err = validateFields(details, dueAt)
	if err != nil {
		return Task{}, false, err
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) == 0 || len([]rune(title)) > 500 {
		return Task{}, false, ErrInvalid
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Task{}, false, err
	}
	t := Task{ID: hex.EncodeToString(bytes[:]), Title: title, Status: "open", Details: details, DueAt: dueAt, CreatedAt: now()}
	t.UpdatedAt = t.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()
	var saved Task
	replay, err := replayReceipt(ctx, tx, "tasks.create", key, hash, &saved)
	if err != nil {
		return Task{}, false, err
	}
	if replay {
		return saved, true, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tasks(id,title,status,created_at,updated_at,details,due_at) VALUES(?,?,?,?,?,?,?)", t.ID, t.Title, t.Status, t.CreatedAt, t.UpdatedAt, t.Details, t.DueAt)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", t.ID, "task.created", t.CreatedAt)
	}
	if err == nil {
		err = saveReceipt(ctx, tx, "tasks.create", key, hash, t)
	}
	if err == nil {
		err = tx.Commit()
	}
	return t, false, err
}
func (s *Store) Tasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,title,status,created_at,updated_at,details,due_at FROM tasks ORDER BY created_at DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.Details, &t.DueAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (s *Store) Complete(ctx context.Context, id string) error {
	_, err := s.CompleteTaskState(ctx, id)
	return err
}
func (s *Store) CompleteTaskState(ctx context.Context, id string) (TaskAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskAction{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id=?", id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return TaskAction{}, ErrNotFound
	} else if err != nil {
		return TaskAction{}, err
	}
	if status == "completed" {
		v, e := taskAction(ctx, tx, id, false)
		if e != nil {
			return v, e
		}
		return v, tx.Commit()
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "UPDATE tasks SET status='completed',updated_at=? WHERE id=?", timestamp, id)
	if err == nil {
		err = cancelTaskReminders(ctx, tx, id, "task.completed", timestamp)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", id, "task.completed", timestamp)
	}
	if err != nil {
		return TaskAction{}, err
	}
	v, err := taskAction(ctx, tx, id, false)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
func (s *Store) Activity(ctx context.Context) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,task_id,action,timestamp,reminder_id,note_id FROM activity ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Activity{}
	for rows.Next() {
		var a Activity
		if err = rows.Scan(&a.ID, &a.TaskID, &a.Action, &a.Timestamp, &a.ReminderID, &a.NoteID); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// Update changes only supplied fields and records one activity entry per actual change.
func (s *Store) Update(ctx context.Context, id string, title, status *string) (Task, error) {
	return s.Patch(ctx, id, TaskPatch{Title: title, Status: status})
}

type TaskPatch struct {
	Title   *string `json:"title"`
	Status  *string `json:"status"`
	Details *string `json:"details"`
	DueAt   *string `json:"due_at"`
}

func (s *Store) Patch(ctx context.Context, id string, p TaskPatch) (Task, error) {
	v, err := s.PatchTaskState(ctx, id, p)
	if err != nil {
		return Task{}, err
	}
	return *v.Task, nil
}
func (s *Store) PatchTaskState(ctx context.Context, id string, p TaskPatch) (TaskAction, error) {
	title, status := p.Title, p.Status
	if p.Details != nil {
		v, _, err := validateFields(*p.Details, "")
		if err != nil {
			return TaskAction{}, err
		}
		p.Details = &v
	}
	if p.DueAt != nil {
		_, v, err := validateFields("", *p.DueAt)
		if err != nil {
			return TaskAction{}, err
		}
		p.DueAt = &v
	}
	if title == nil && status == nil && p.Details == nil && p.DueAt == nil {
		return TaskAction{}, ErrInvalidUpdate
	}
	if title != nil {
		v := strings.TrimSpace(*title)
		if len([]rune(v)) == 0 || len([]rune(v)) > 500 {
			return TaskAction{}, ErrInvalid
		}
		title = &v
	}
	if status != nil && *status != "open" && *status != "completed" {
		return TaskAction{}, ErrInvalidUpdate
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskAction{}, err
	}
	defer tx.Rollback()
	var t Task
	err = tx.QueryRowContext(ctx, "SELECT id,title,status,created_at,updated_at,details,due_at FROM tasks WHERE id=?", id).Scan(&t.ID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.Details, &t.DueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskAction{}, ErrNotFound
	}
	if err != nil {
		return TaskAction{}, err
	}
	changed := false
	if title != nil && t.Title != *title {
		t.Title = *title
		changed = true
	}
	if status != nil && t.Status != *status {
		t.Status = *status
		changed = true
	}
	if p.Details != nil && t.Details != *p.Details {
		t.Details = *p.Details
		changed = true
	}
	if p.DueAt != nil && t.DueAt != *p.DueAt {
		t.DueAt = *p.DueAt
		changed = true
	}
	if changed {
		t.UpdatedAt = now()
		_, err = tx.ExecContext(ctx, "UPDATE tasks SET title=?,status=?,updated_at=?,details=?,due_at=? WHERE id=?", t.Title, t.Status, t.UpdatedAt, t.Details, t.DueAt, id)
		if err == nil && t.Status == "completed" {
			err = cancelTaskReminders(ctx, tx, id, "task.completed", t.UpdatedAt)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", id, "task.updated", t.UpdatedAt)
		}
	}
	if err != nil {
		return TaskAction{}, err
	}
	v, err := taskAction(ctx, tx, id, false)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}

var ErrInvalidUpdate = errors.New("provide a task field; status must be open or completed")

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.DeleteTaskState(ctx, id)
	return err
}
func (s *Store) DeleteTaskState(ctx context.Context, id string) (TaskAction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskAction{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM tasks WHERE id=?", id)
	if err != nil {
		return TaskAction{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return TaskAction{}, err
	}
	if count == 0 {
		return TaskAction{}, ErrNotFound
	}
	timestamp := now()
	if err = cancelTaskReminders(ctx, tx, id, "task.deleted", timestamp); err != nil {
		return TaskAction{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", id, "task.deleted", timestamp)
	if err != nil {
		return TaskAction{}, err
	}
	v, err := taskAction(ctx, tx, id, true)
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}

var ErrInvalidFields = errors.New("details must be at most 10000 characters; deadline must be RFC3339 with timezone or empty")

func validateFields(details, due string) (string, string, error) {
	if len([]rune(details)) > 10000 {
		return "", "", ErrInvalidFields
	}
	if due != "" {
		t, err := time.Parse(time.RFC3339Nano, due)
		if err != nil || t.Year() < 1 || t.Year() > 9999 {
			return "", "", ErrInvalidFields
		}
		if t.UTC().Year() < 1 || t.UTC().Year() > 9999 {
			return "", "", ErrInvalidFields
		}
		due = t.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	return details, due, nil
}
