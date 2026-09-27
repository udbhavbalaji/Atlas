package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCapturePreview = errors.New("capture fields changed or preview is missing; preview again before confirming")
var ErrCaptureKey = errors.New("capture confirmation requires an idempotency key")
var ErrCaptureReminder = errors.New("reminder title, timezone, and repeat require reminder_at")

// CaptureInput is intentionally explicit; no natural-language interpretation occurs.
type CaptureInput struct {
	Title         string `json:"title"`
	Details       string `json:"details"`
	DueAt         string `json:"due_at"`
	ReminderAt    string `json:"reminder_at"`
	ReminderTitle string `json:"reminder_title"`
	Timezone      string `json:"timezone"`
	Repeat        string `json:"repeat"`
	PreviewID     string `json:"preview_id,omitempty"`
}
type CaptureProposal struct {
	Input    CaptureInput `json:"input"`
	Effects  []string     `json:"effects"`
	Warnings []string     `json:"warnings"`
}

func PreviewCapture(input CaptureInput) (CaptureProposal, error) {
	p := CaptureProposal{Effects: []string{"task.create"}, Warnings: []string{}}
	input.Title = strings.TrimSpace(input.Title)
	if !utf8.ValidString(input.Title) || len([]rune(input.Title)) < 1 || len([]rune(input.Title)) > 500 {
		return p, ErrInvalid
	}
	if !utf8.ValidString(input.Details) {
		return p, ErrInvalidFields
	}
	var err error
	input.Details, input.DueAt, err = validateFields(input.Details, input.DueAt)
	if err != nil {
		return p, err
	}
	if input.ReminderAt == "" {
		if input.ReminderTitle != "" || input.Timezone != "" || input.Repeat != "" {
			return p, ErrCaptureReminder
		}
	} else {
		input.ReminderAt, err = reminderTime(input.ReminderAt)
		if err != nil {
			return p, err
		}
		input.ReminderTitle = strings.TrimSpace(input.ReminderTitle)
		if input.ReminderTitle == "" {
			input.ReminderTitle = input.Title
		}
		if !utf8.ValidString(input.ReminderTitle) || len([]rune(input.ReminderTitle)) > 500 {
			return p, ErrInvalid
		}
		if input.Timezone == "" || input.Timezone == "Local" {
			return p, ErrInvalidReminder
		}
		if _, err = time.LoadLocation(input.Timezone); err != nil {
			return p, ErrInvalidReminder
		}
		if input.Repeat != "" && input.Repeat != "daily" && input.Repeat != "weekly" {
			return p, ErrInvalidRepeat
		}
		p.Effects = append(p.Effects, "reminder.create", "reminder.link_task", "delivery.queue")
		at, _ := time.Parse(time.RFC3339Nano, input.ReminderAt)
		if !at.After(time.Now()) {
			p.Warnings = append(p.Warnings, "Reminder time is in the past; Atlas will deliver it when the scheduler next runs.")
		}
	}
	if input.DueAt != "" {
		at, _ := time.Parse(time.RFC3339Nano, input.DueAt)
		if !at.After(time.Now()) {
			p.Warnings = append(p.Warnings, "The task deadline is in the past; this task will be overdue.")
		}
	}
	input.PreviewID = fingerprint("capture.v1", input.Title, input.Details, input.DueAt, input.ReminderAt, input.ReminderTitle, input.Timezone, input.Repeat)
	p.Input = input
	return p, nil
}

// CommitCapture commits the entire proposal and receipt together. The preview ID
// identifies normalized content, not permission or a server-side saved draft.
func (s *Store) CommitCapture(ctx context.Context, key string, input CaptureInput) (TaskAction, bool, error) {
	if key == "" {
		return TaskAction{}, false, ErrCaptureKey
	}
	if !ValidKey(key) {
		return TaskAction{}, false, ErrInvalidKey
	}
	p, err := PreviewCapture(input)
	if err != nil {
		return TaskAction{}, false, err
	}
	if input.PreviewID == "" || input.PreviewID != p.Input.PreviewID {
		return TaskAction{}, false, ErrCapturePreview
	}
	input = p.Input
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskAction{}, false, err
	}
	defer tx.Rollback()
	var saved TaskAction
	replay, err := replayReceipt(ctx, tx, "capture.commit", key, input.PreviewID, &saved)
	if err != nil {
		return saved, false, err
	}
	if replay {
		return saved, true, tx.Commit()
	}
	id, err := newID()
	if err != nil {
		return saved, false, err
	}
	timestamp := now()
	_, err = tx.ExecContext(ctx, "INSERT INTO tasks(id,title,status,created_at,updated_at,details,due_at) VALUES(?,?,'open',?,?,?,?)", id, input.Title, timestamp, timestamp, input.Details, input.DueAt)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.created',?)", id, timestamp)
	}
	if err == nil && input.ReminderAt != "" {
		var reminderID string
		reminderID, err = newID()
		anchor := ""
		if input.Repeat != "" {
			anchor = input.ReminderAt
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO reminders(id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title,repeat,repeat_anchor) VALUES(?,?,'scheduled',?,?,?,?,?,?,?,?)", reminderID, input.ReminderTitle, input.ReminderAt, input.Timezone, timestamp, timestamp, id, input.Title, input.Repeat, anchor)
		}
		if err == nil {
			err = queueDelivery(ctx, tx, reminderID, input.ReminderAt)
		}
		if err == nil {
			err = reminderActivity(ctx, tx, reminderID, "reminder.scheduled", timestamp)
		}
	}
	if err == nil {
		saved, err = taskAction(ctx, tx, id, false)
	}
	if err == nil {
		err = saveReceipt(ctx, tx, "capture.commit", key, input.PreviewID, saved)
	}
	if err == nil {
		err = tx.Commit()
	}
	return saved, false, err
}
