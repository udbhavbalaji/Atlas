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
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}
type Activity struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Action    string `json:"action"`
	Timestamp string `json:"timestamp"`
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
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err == nil && version > 1 {
		err = errors.New("database schema is newer than this Atlas version")
	}
	if err == nil && version == 0 {
		_, err = tx.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('open','completed')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE TABLE activity(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,action TEXT NOT NULL,timestamp TEXT NOT NULL);
 PRAGMA user_version=1;`)
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
	title = strings.TrimSpace(title)
	if len([]rune(title)) == 0 || len([]rune(title)) > 500 {
		return Task{}, ErrInvalid
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Task{}, err
	}
	t := Task{ID: hex.EncodeToString(bytes[:]), Title: title, Status: "open", CreatedAt: now()}
	t.UpdatedAt = t.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO tasks(id,title,status,created_at,updated_at) VALUES(?,?,?,?,?)", t.ID, t.Title, t.Status, t.CreatedAt, t.UpdatedAt)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", t.ID, "task.created", t.CreatedAt)
	}
	if err == nil {
		err = tx.Commit()
	}
	return t, err
}
func (s *Store) Tasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,title,status,created_at,updated_at FROM tasks ORDER BY created_at DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (s *Store) Complete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id=?", id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status == "completed" {
		return tx.Commit()
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "UPDATE tasks SET status='completed',updated_at=? WHERE id=?", timestamp, id)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", id, "task.completed", timestamp)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
func (s *Store) Activity(ctx context.Context) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,task_id,action,timestamp FROM activity ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Activity{}
	for rows.Next() {
		var a Activity
		if err = rows.Scan(&a.ID, &a.TaskID, &a.Action, &a.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
