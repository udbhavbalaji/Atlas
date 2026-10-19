package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrBatchEditChanged = errors.New("reviewed bulk edit changed; review again")

// EditBatchStatusRequest verifies the complete reviewed set, then changes it
// atomically. A receipt makes recovery safe after a lost HTTP response.
func (s *Store) EditBatchStatusRequest(ctx context.Context, key, kind, status string, targets []BatchTarget) error {
	if len(targets) == 0 || kind != "task" && kind != "reminder" || kind == "task" && status != "open" && status != "completed" || kind == "reminder" && status != "completed" && status != "dismissed" {
		return ErrBatchEditChanged
	}
	encoded, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	hash := fingerprint("batch.edit.status.v1", kind, status, string(encoded))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var receipt struct{}
	if replayed, e := replayReceipt(ctx, tx, "batch_edit", key, hash, &receipt); e != nil {
		return e
	} else if replayed {
		return nil
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.Kind != kind || target.ID == "" || seen[target.ID] {
			return ErrBatchEditChanged
		}
		seen[target.ID] = true
		var updated, current, repeat string
		if kind == "task" {
			err = tx.QueryRowContext(ctx, "SELECT updated_at,status FROM tasks WHERE id=?", target.ID).Scan(&updated, &current)
		} else {
			err = tx.QueryRowContext(ctx, "SELECT updated_at,status,repeat FROM reminders WHERE id=?", target.ID).Scan(&updated, &current, &repeat)
		}
		if err != nil || updated != target.UpdatedAt || kind == "reminder" && (repeat != "" || current == "completed" && status != "completed") {
			return ErrBatchEditChanged
		}
		if kind == "task" {
			rows, e := tx.QueryContext(ctx, "SELECT id,updated_at FROM reminders WHERE task_id=? AND status IN ('scheduled','due')", target.ID)
			if e != nil {
				return e
			}
			linked := map[string]bool{}
			for rows.Next() {
				var id, version string
				if e = rows.Scan(&id, &version); e != nil {
					rows.Close()
					return e
				}
				linked[id+":"+version] = true
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(linked) != len(target.LinkedReminders) {
				return ErrBatchEditChanged
			}
			for _, value := range target.LinkedReminders {
				if !linked[value] {
					return ErrBatchEditChanged
				}
			}
		}
	}
	for _, target := range targets {
		stamp := now()
		if kind == "task" {
			_, err = tx.ExecContext(ctx, "UPDATE tasks SET status=?,updated_at=? WHERE id=? AND status<>?", status, stamp, target.ID, status)
			if err == nil && status == "completed" {
				err = cancelTaskReminders(ctx, tx, target.ID, "task.completed", stamp)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.updated',?)", target.ID, stamp)
			}
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE deliveries SET state=CASE WHEN state='queued' THEN 'cancelled' ELSE 'acknowledged' END WHERE reminder_id=? AND state IN ('queued','delivered')", target.ID)
			if err == nil {
				_, err = tx.ExecContext(ctx, "UPDATE reminders SET status=?,updated_at=? WHERE id=?", status, stamp, target.ID)
			}
			if err == nil {
				err = reminderActivity(ctx, tx, target.ID, "reminder."+status, stamp)
			}
		}
		if err != nil {
			return err
		}
	}
	if err = saveReceipt(ctx, tx, "batch_edit", key, hash, receipt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EditBatchNoteRequest(ctx context.Context, key, field, value string, targets []BatchTarget) error {
	if len(targets) == 0 || field != "title" && field != "body" {
		return ErrBatchEditChanged
	}
	if field == "title" {
		if strings.TrimSpace(value) == "" || len([]rune(value)) > 500 || !utf8.ValidString(value) {
			return ErrInvalid
		}
	} else if err := validateNote(value); err != nil {
		return err
	}
	encoded, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	hash := fingerprint("batch.edit.note.v1", field, value, string(encoded))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var receipt struct{}
	if replayed, e := replayReceipt(ctx, tx, "batch_edit", key, hash, &receipt); e != nil {
		return e
	} else if replayed {
		return nil
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.Kind != "note" || target.ID == "" || seen[target.ID] {
			return ErrBatchEditChanged
		}
		seen[target.ID] = true
		var updated string
		if err = tx.QueryRowContext(ctx, "SELECT updated_at FROM notes WHERE id=?", target.ID).Scan(&updated); err != nil || updated != target.UpdatedAt {
			return ErrBatchEditChanged
		}
	}
	for _, target := range targets {
		stamp := now()
		_, err = tx.ExecContext(ctx, "UPDATE notes SET "+field+"=?,updated_at=? WHERE id=?", value, stamp, target.ID)
		if err == nil {
			err = noteActivity(ctx, tx, target.ID, "", "", "note.updated", stamp)
		}
		if err != nil {
			return err
		}
	}
	if err = saveReceipt(ctx, tx, "batch_edit", key, hash, receipt); err != nil {
		return err
	}
	return tx.Commit()
}
