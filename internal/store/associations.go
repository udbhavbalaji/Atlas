package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// AssociationChange is the exact pair and operation reviewed by the user.
// From and To carry record versions so a later confirmation cannot apply to
// changed targets. The request and receipt commit in the same transaction.
type AssociationChange struct {
	Operation string      `json:"operation"`
	Attach    bool        `json:"attach"`
	From      BatchTarget `json:"from"`
	To        BatchTarget `json:"to"`
}

var ErrAssociationChanged = errors.New("reviewed relationship records changed; review again")

func associationTable(kind string) string {
	switch kind {
	case "task":
		return "tasks"
	case "reminder":
		return "reminders"
	case "note":
		return "notes"
	}
	return ""
}

func validAssociationChange(change AssociationChange) bool {
	from, to := change.From, change.To
	if associationTable(from.Kind) == "" || associationTable(to.Kind) == "" || !taskIDPattern.MatchString(from.ID) || !taskIDPattern.MatchString(to.ID) || from.Kind == to.Kind && from.ID == to.ID || from.UpdatedAt == "" || to.UpdatedAt == "" {
		return false
	}
	switch change.Operation {
	case "note_link":
		return from.Kind == "note" && (to.Kind == "task" || to.Kind == "reminder")
	case "reminder_task":
		return from.Kind == "reminder" && to.Kind == "task"
	case "dependency":
		return from.Kind == "task" && to.Kind == "task"
	case "relation":
		return true
	}
	return false
}

func checkAssociationTarget(ctx context.Context, tx *sql.Tx, target BatchTarget) error {
	var version string
	if err := tx.QueryRowContext(ctx, "SELECT updated_at FROM "+associationTable(target.Kind)+" WHERE id=?", target.ID).Scan(&version); err != nil || version != target.UpdatedAt {
		return ErrAssociationChanged
	}
	return nil
}

func (s *Store) ApplyAssociationRequest(ctx context.Context, key string, change AssociationChange) error {
	if !validAssociationChange(change) {
		return ErrAssociationChanged
	}
	encoded, err := json.Marshal(change)
	if err != nil {
		return err
	}
	hash := fingerprint("association.change.v1", string(encoded))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var receipt struct{}
	if replayed, replayErr := replayReceipt(ctx, tx, "association_change", key, hash, &receipt); replayErr != nil {
		return replayErr
	} else if replayed {
		return nil
	}
	if err = checkAssociationTarget(ctx, tx, change.From); err != nil {
		return err
	}
	if err = checkAssociationTarget(ctx, tx, change.To); err != nil {
		return err
	}
	switch change.Operation {
	case "note_link":
		err = applyReviewedNoteLink(ctx, tx, change)
	case "reminder_task":
		err = applyReviewedReminderTask(ctx, tx, change)
	case "dependency":
		err = applyReviewedDependency(ctx, tx, change)
	case "relation":
		err = applyReviewedRelation(ctx, tx, change)
	}
	if err != nil {
		return err
	}
	if err = saveReceipt(ctx, tx, "association_change", key, hash, receipt); err != nil {
		return err
	}
	return tx.Commit()
}

func applyReviewedNoteLink(ctx context.Context, tx *sql.Tx, change AssociationChange) error {
	var result sql.Result
	var err error
	if change.Attach {
		title, targetErr := noteTarget(ctx, tx, change.To.Kind, change.To.ID)
		if targetErr != nil {
			return targetErr
		}
		result, err = tx.ExecContext(ctx, "INSERT INTO note_links VALUES(?,?,?,?) ON CONFLICT DO NOTHING", change.From.ID, change.To.Kind, change.To.ID, title)
	} else {
		result, err = tx.ExecContext(ctx, "DELETE FROM note_links WHERE note_id=? AND target_type=? AND target_id=?", change.From.ID, change.To.Kind, change.To.ID)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	at := now()
	if _, err = tx.ExecContext(ctx, "UPDATE notes SET updated_at=? WHERE id=?", at, change.From.ID); err != nil {
		return err
	}
	action := "note.unlinked"
	if change.Attach {
		action = "note.linked"
	}
	return noteActivity(ctx, tx, change.From.ID, change.To.Kind, change.To.ID, action, at)
}

func applyReviewedReminderTask(ctx context.Context, tx *sql.Tx, change AssociationChange) error {
	r, err := readReminder(ctx, tx, change.From.ID)
	if err != nil {
		return err
	}
	if r.Status == "completed" {
		return ErrReminderConflict
	}
	requested := ""
	title := ""
	if change.Attach {
		task, readErr := readTask(ctx, tx, change.To.ID)
		if readErr != nil {
			return readErr
		}
		if task.Status != "open" {
			return ErrTaskReminderConflict
		}
		requested, title = task.ID, task.Title
	} else if r.TaskID != change.To.ID {
		return ErrAssociationChanged
	}
	if r.TaskID == requested {
		return nil
	}
	at := now()
	if _, err = tx.ExecContext(ctx, "UPDATE reminders SET task_id=?,task_title=?,updated_at=? WHERE id=?", requested, title, at, r.ID); err != nil {
		return err
	}
	action := "reminder.task_unlinked"
	if change.Attach {
		action = "reminder.task_linked"
	}
	return reminderActivity(ctx, tx, r.ID, action, at)
}

func applyReviewedDependency(ctx context.Context, tx *sql.Tx, change AssociationChange) error {
	if change.Attach {
		return addDependency(ctx, tx, change.From.ID, change.To.ID)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM task_dependencies WHERE before_task_id=? AND after_task_id=?", change.From.ID, change.To.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.dependency_removed',?)", change.From.ID, now())
	}
	return err
}

func applyReviewedRelation(ctx context.Context, tx *sql.Tx, change AssociationChange) error {
	id, fromKind, fromID, toKind, toID, err := relationPair(change.From.Kind, change.From.ID, change.To.Kind, change.To.ID)
	if err != nil {
		return err
	}
	var result sql.Result
	if change.Attach {
		from, fromErr := relationRecord(ctx, tx, fromKind, fromID)
		if fromErr != nil {
			return fromErr
		}
		to, toErr := relationRecord(ctx, tx, toKind, toID)
		if toErr != nil {
			return toErr
		}
		result, err = tx.ExecContext(ctx, "INSERT INTO record_relations VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", id, fromKind, fromID, toKind, toID, from.Title, to.Title, now())
	} else {
		result, err = tx.ExecContext(ctx, "DELETE FROM record_relations WHERE id=?", id)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		action := "relation.removed"
		if change.Attach {
			action = "relation.added"
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp,relation_id) VALUES('',?,?,?)", action, now(), id)
	}
	return err
}
