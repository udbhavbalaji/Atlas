package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const recurrenceMigration = `ALTER TABLE reminders ADD COLUMN repeat TEXT NOT NULL DEFAULT '' CHECK(repeat IN ('','daily','weekly')); ALTER TABLE reminders ADD COLUMN repeat_anchor TEXT NOT NULL DEFAULT ''; ALTER TABLE reminders ADD COLUMN occurrence_id TEXT NOT NULL DEFAULT ''; UPDATE reminders SET occurrence_id=COALESCE((SELECT id FROM deliveries WHERE reminder_id=reminders.id AND state IN ('queued','delivered') ORDER BY scheduled_at DESC LIMIT 1),''); PRAGMA user_version=6;`

var ErrInvalidRepeat = errors.New("repeat must be empty, daily, or weekly")
var ErrOccurrenceConflict = errors.New("only the current due occurrence of a repeating reminder can be acknowledged")

// nextRepeat keeps the original local clock and weekday. Missing local times are
// skipped; ambiguous times choose the earlier instant. Only a future occurrence
// is queued, so downtime never produces a burst of old notifications.
func nextRepeat(anchor, repeat, zone string, after time.Time) (string, error) {
	if repeat != "daily" && repeat != "weekly" {
		return "", ErrInvalidRepeat
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", err
	}
	base, err := time.Parse(time.RFC3339Nano, anchor)
	if err != nil {
		return "", err
	}
	base = base.In(loc)
	step := 1
	if repeat == "weekly" {
		step = 7
	}
	// Use calendar days in UTC as a date-only counter, independent of DST.
	start := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.UTC)
	localAfter := after.In(loc)
	dateAfter := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), 0, 0, 0, 0, time.UTC)
	days := int((dateAfter.Unix() - start.Unix()) / 86400)
	if days < step {
		days = step
	}
	days = (days / step) * step
	if days < step {
		days = step
	}
	for attempts := 0; attempts < 370; attempts++ {
		date := start.AddDate(0, 0, days)
		days += step
		wall := time.Date(date.Year(), date.Month(), date.Day(), base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), time.UTC)
		offsets := map[int]bool{}
		for h := -48; h <= 48; h += 6 {
			_, offset := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
			offsets[offset] = true
		}
		var earliest time.Time
		for offset := range offsets {
			candidate := wall.Add(-time.Duration(offset) * time.Second)
			local := candidate.In(loc)
			if local.Year() == date.Year() && local.Month() == date.Month() && local.Day() == date.Day() && local.Hour() == base.Hour() && local.Minute() == base.Minute() && local.Second() == base.Second() {
				if earliest.IsZero() || candidate.Before(earliest) {
					earliest = candidate
				}
			}
		}
		if !earliest.IsZero() && earliest.After(after) {
			return instant(earliest), nil
		}
	}
	return "", errors.New("could not find a future recurrence")
}

func (s *Store) FinishOccurrence(ctx context.Context, id, occurrenceID, action string) (ReminderAction, error) {
	return s.finishOccurrenceAt(ctx, id, occurrenceID, action, time.Now())
}
func (s *Store) finishOccurrenceAt(ctx context.Context, id, occurrenceID, action string, at time.Time) (ReminderAction, error) {
	if action != "complete" && action != "dismiss" {
		return ReminderAction{}, ErrOccurrenceConflict
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
	var state string
	err = tx.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE id=? AND reminder_id=?", occurrenceID, id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ReminderAction{}, ErrOccurrenceConflict
	}
	if err != nil {
		return ReminderAction{}, err
	}
	// An acknowledged delivery is the durable receipt: retry cannot advance twice.
	if state == "acknowledged" && r.Repeat != "" {
		v, e := reminderAction(ctx, tx, id)
		if e != nil {
			return v, e
		}
		return v, tx.Commit()
	}
	if r.Repeat == "" || r.Status != "due" || r.OccurrenceID != occurrenceID || state != "delivered" {
		return ReminderAction{}, ErrOccurrenceConflict
	}
	scheduled, err := time.Parse(time.RFC3339Nano, r.ScheduledAt)
	if err != nil {
		return ReminderAction{}, err
	}
	if scheduled.After(at) {
		at = scheduled
	}
	next, err := nextRepeat(r.RepeatAnchor, r.Repeat, r.Timezone, at)
	if err != nil {
		return ReminderAction{}, err
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "UPDATE deliveries SET state='acknowledged' WHERE id=?", occurrenceID)
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE reminders SET status='scheduled',scheduled_at=?,updated_at=? WHERE id=?", next, timestamp, id)
	}
	if err == nil {
		err = queueDelivery(ctx, tx, id, next)
	}
	if err == nil {
		err = reminderActivity(ctx, tx, id, "reminder.occurrence."+action, timestamp)
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
