package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// BatchTarget is the exact record reviewed before a destructive batch action.
type BatchTarget struct {
	Kind            string   `json:"kind"`
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	UpdatedAt       string   `json:"updated_at"`
	LinkedReminders []string `json:"linked_reminders,omitempty"`
}

var ErrBatchChanged = errors.New("reviewed records changed; review the batch again")

// DeleteBatch checks every reviewed record and applies all deletions in one
// transaction. Newly created records are never included implicitly.
func (s *Store) DeleteBatch(ctx context.Context, targets []BatchTarget) error {
	if len(targets) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, target := range targets {
		var table string
		switch target.Kind {
		case "task":
			table = "tasks"
		case "reminder":
			table = "reminders"
		case "note":
			table = "notes"
		default:
			return fmt.Errorf("invalid batch kind: %w", ErrBatchChanged)
		}
		key := target.Kind + ":" + target.ID
		if target.ID == "" || seen[key] {
			return ErrBatchChanged
		}
		seen[key] = true
		var updated string
		if err = tx.QueryRowContext(ctx, "SELECT updated_at FROM "+table+" WHERE id=?", target.ID).Scan(&updated); err != nil || updated != target.UpdatedAt {
			return ErrBatchChanged
		}
		if target.Kind == "task" {
			rows, queryErr := tx.QueryContext(ctx, "SELECT id,updated_at FROM reminders WHERE task_id=? AND status IN ('scheduled','due')", target.ID)
			if queryErr != nil {
				return queryErr
			}
			linked := []string{}
			for rows.Next() {
				var id, version string
				if err = rows.Scan(&id, &version); err != nil {
					rows.Close()
					return err
				}
				linked = append(linked, id+":"+version)
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			sort.Strings(linked)
			if len(linked) != len(target.LinkedReminders) {
				return ErrBatchChanged
			}
			for i := range linked {
				if linked[i] != target.LinkedReminders[i] {
					return ErrBatchChanged
				}
			}
		}
	}
	// Remove deliveries before reminders because deliveries has a restrictive FK.
	for _, kind := range []string{"reminder", "task", "note"} {
		for _, target := range targets {
			if target.Kind != kind {
				continue
			}
			timestamp := now()
			switch kind {
			case "reminder":
				if _, err = tx.ExecContext(ctx, "DELETE FROM deliveries WHERE reminder_id=?", target.ID); err == nil {
					_, err = tx.ExecContext(ctx, "DELETE FROM reminders WHERE id=?", target.ID)
				}
				if err == nil {
					err = reminderActivity(ctx, tx, target.ID, "reminder.deleted", timestamp)
				}
			case "task":
				_, err = tx.ExecContext(ctx, "DELETE FROM tasks WHERE id=?", target.ID)
				if err == nil {
					err = cancelTaskReminders(ctx, tx, target.ID, "task.deleted", timestamp)
				}
				if err == nil {
					_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,?,?)", target.ID, "task.deleted", timestamp)
				}
			case "note":
				_, err = tx.ExecContext(ctx, "DELETE FROM notes WHERE id=?", target.ID)
				if err == nil {
					err = noteActivity(ctx, tx, target.ID, "", "", "note.deleted", timestamp)
				}
			}
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
