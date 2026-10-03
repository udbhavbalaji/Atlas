package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
)

const dependencyMigration = `CREATE TABLE task_dependencies(before_task_id TEXT NOT NULL,after_task_id TEXT NOT NULL,before_title TEXT NOT NULL,after_title TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(before_task_id,after_task_id),CHECK(before_task_id<>after_task_id)); CREATE INDEX dependencies_after ON task_dependencies(after_task_id); PRAGMA user_version=8;`

var ErrDependency = errors.New("dependency requires two distinct existing tasks and an open target task")
var ErrDependencyCycle = errors.New("this dependency would create a cycle")
var ErrCaptureContext = errors.New("the referenced task changed, completed, or disappeared; preview again before confirming")
var taskIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var versionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type TaskDependency struct {
	BeforeTaskID string `json:"before_task_id"`
	AfterTaskID  string `json:"after_task_id"`
	BeforeTitle  string `json:"before_title"`
	AfterTitle   string `json:"after_title"`
	BeforeStatus string `json:"before_status"`
	AfterStatus  string `json:"after_status"`
	BeforeExists bool   `json:"before_exists"`
	AfterExists  bool   `json:"after_exists"`
	AfterDueAt   string `json:"after_due_at"`
	Satisfied    bool   `json:"satisfied"`
	CreatedAt    string `json:"created_at"`
}

func taskVersion(t Task) string {
	return fingerprint("task.context.v1", t.ID, t.Title, t.Status, t.DueAt, t.UpdatedAt)
}
func readDependencies(ctx context.Context, tx *sql.Tx, id string) ([]TaskDependency, error) {
	rows, e := tx.QueryContext(ctx, `SELECT d.before_task_id,d.after_task_id,COALESCE(b.title,d.before_title),COALESCE(a.title,d.after_title),COALESCE(b.status,''),COALESCE(a.status,''),b.id IS NOT NULL,a.id IS NOT NULL,COALESCE(a.due_at,''),d.created_at FROM task_dependencies d LEFT JOIN tasks b ON b.id=d.before_task_id LEFT JOIN tasks a ON a.id=d.after_task_id WHERE ?='' OR d.before_task_id=? OR d.after_task_id=? ORDER BY d.created_at,d.before_task_id,d.after_task_id`, id, id, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	list := []TaskDependency{}
	for rows.Next() {
		var d TaskDependency
		if e = rows.Scan(&d.BeforeTaskID, &d.AfterTaskID, &d.BeforeTitle, &d.AfterTitle, &d.BeforeStatus, &d.AfterStatus, &d.BeforeExists, &d.AfterExists, &d.AfterDueAt, &d.CreatedAt); e != nil {
			return nil, e
		}
		d.Satisfied = d.BeforeExists && d.BeforeStatus == "completed"
		list = append(list, d)
	}
	return list, rows.Err()
}
func (s *Store) Dependencies(ctx context.Context) ([]TaskDependency, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	list, e := readDependencies(ctx, tx, "")
	if e == nil {
		e = tx.Commit()
	}
	return list, e
}
func addDependency(ctx context.Context, tx *sql.Tx, before, after string) error {
	if before == after {
		return ErrDependencyCycle
	}
	b, e := readTask(ctx, tx, before)
	if e != nil {
		return e
	}
	a, e := readTask(ctx, tx, after)
	if e != nil {
		return e
	}
	if a.Status != "open" {
		return ErrDependency
	}
	var cycle bool
	e = tx.QueryRowContext(ctx, `WITH RECURSIVE reachable(id) AS (SELECT after_task_id FROM task_dependencies WHERE before_task_id=? UNION SELECT d.after_task_id FROM task_dependencies d JOIN reachable r ON d.before_task_id=r.id) SELECT EXISTS(SELECT 1 FROM reachable WHERE id=?)`, after, before).Scan(&cycle)
	if e != nil {
		return e
	}
	if cycle {
		return ErrDependencyCycle
	}
	result, e := tx.ExecContext(ctx, "INSERT INTO task_dependencies VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING", before, after, b.Title, a.Title, now())
	if e != nil {
		return e
	}
	count, e := result.RowsAffected()
	if e == nil && count > 0 {
		_, e = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.dependency_added',?)", before, now())
	}
	return e
}
func (s *Store) SetDependency(ctx context.Context, before, after string, attach bool) (TaskAction, error) {
	if !taskIDPattern.MatchString(before) || !taskIDPattern.MatchString(after) {
		return TaskAction{}, ErrDependency
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return TaskAction{}, e
	}
	defer tx.Rollback()
	actionID := before
	if attach {
		e = addDependency(ctx, tx, before, after)
	} else {
		if _, e = readTask(ctx, tx, before); errors.Is(e, ErrNotFound) {
			if _, e = readTask(ctx, tx, after); e == nil {
				actionID = after
			}
		}
		if e == nil {
			var result sql.Result
			result, e = tx.ExecContext(ctx, "DELETE FROM task_dependencies WHERE before_task_id=? AND after_task_id=?", before, after)
			if e == nil {
				var count int64
				count, e = result.RowsAffected()
				if e == nil && count > 0 {
					_, e = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.dependency_removed',?)", before, now())
				}
			}
		}
	}
	var v TaskAction
	if e == nil {
		v, e = taskAction(ctx, tx, actionID, false)
	}
	if e == nil {
		e = tx.Commit()
	}
	return v, e
}

// CapturePreview binds the proposal to the current target snapshot without writes.
func (s *Store) CapturePreview(ctx context.Context, input CaptureInput) (CaptureProposal, error) {
	if input.LinkedTaskID != "" {
		if input.BeforeTaskID != "" || !taskIDPattern.MatchString(input.LinkedTaskID) {
			return CaptureProposal{}, ErrCaptureContext
		}
		target, e := s.TaskState(ctx, input.LinkedTaskID)
		if e != nil {
			return CaptureProposal{}, e
		}
		if input.Kind == "reminder" && target.Task.Status != "open" {
			return CaptureProposal{}, ErrTaskReminderConflict
		}
		p, e := PreviewCapture(input)
		if e == nil {
			p.Reference = target.Task
		}
		return p, e
	}
	if input.BeforeTaskID == "" {
		return PreviewCapture(input)
	}
	if !taskIDPattern.MatchString(input.BeforeTaskID) {
		return CaptureProposal{}, ErrDependency
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return CaptureProposal{}, e
	}
	defer tx.Rollback()
	target, e := readTask(ctx, tx, input.BeforeTaskID)
	if e != nil {
		return CaptureProposal{}, e
	}
	if target.Status != "open" {
		return CaptureProposal{}, ErrDependency
	}
	input.BeforeTaskVersion = taskVersion(target)
	p, e := PreviewCapture(input)
	if e == nil {
		p.Reference = &target
		if target.DueAt == "" {
			p.Warnings = append(p.Warnings, "The referenced task has no deadline. This saves an ordering relationship; no time is invented.")
		}
		e = tx.Commit()
	}
	return p, e
}
