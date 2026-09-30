package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCapturePreview = errors.New("capture fields changed or preview is missing; preview again before confirming")
var ErrCaptureKind = errors.New("capture kind must be task, reminder, or note; fields must match that kind")
var ErrCaptureKey = errors.New("capture confirmation requires an idempotency key")
var ErrCaptureReminder = errors.New("reminder title, timezone, and repeat require reminder_at")

// CaptureInput is the validated boundary for explicit and interpreted proposals.
type CaptureInput struct {
	BeforeTaskID      string `json:"before_task_id"`
	BeforeTaskVersion string `json:"before_task_version"`
	LinkedTaskID      string `json:"linked_task_id"`
	Kind              string `json:"kind"`
	NoteBody          string `json:"note_body"`
	Title             string `json:"title"`
	Details           string `json:"details"`
	DueAt             string `json:"due_at"`
	ReminderAt        string `json:"reminder_at"`
	ReminderTitle     string `json:"reminder_title"`
	Timezone          string `json:"timezone"`
	Repeat            string `json:"repeat"`
	PreviewID         string `json:"preview_id,omitempty"`
}
type CaptureProposal struct {
	Reference *Task        `json:"reference"`
	Input     CaptureInput `json:"input"`
	Effects   []string     `json:"effects"`
	Warnings  []string     `json:"warnings"`
}

func PreviewCapture(input CaptureInput) (CaptureProposal, error) {
	p := CaptureProposal{Effects: []string{}, Warnings: []string{}}
	if input.Kind == "" {
		input.Kind = "task"
	}
	if input.Kind != "task" && input.Kind != "reminder" && input.Kind != "note" {
		return p, ErrCaptureKind
	}
	if (input.BeforeTaskID != "" || input.BeforeTaskVersion != "") && (input.Kind != "task" || !taskIDPattern.MatchString(input.BeforeTaskID) || !versionPattern.MatchString(input.BeforeTaskVersion)) {
		return p, ErrDependency
	}
	if input.LinkedTaskID != "" && (input.Kind == "task" || !taskIDPattern.MatchString(input.LinkedTaskID)) {
		return p, ErrCaptureContext
	}
	if input.BeforeTaskID != "" {
		p.Effects = append(p.Effects, "task.dependency_added")
	}
	if input.Kind != "task" && (input.Details != "" || input.DueAt != "") {
		return p, ErrCaptureKind
	}
	if input.Kind == "note" && (input.Title != "" || input.ReminderAt != "" || input.ReminderTitle != "" || input.Timezone != "" || input.Repeat != "") {
		return p, ErrCaptureKind
	}
	if input.Kind == "reminder" && input.ReminderAt == "" {
		return p, ErrInvalidReminder
	}
	if input.NoteBody != "" || input.Kind == "note" {
		if err := validateNote(input.NoteBody); err != nil {
			return p, err
		}
	}
	if input.Kind == "task" {
		p.Effects = append(p.Effects, "task.create")
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Kind != "note" && (!utf8.ValidString(input.Title) || len([]rune(input.Title)) < 1 || len([]rune(input.Title)) > 500) {
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
		p.Effects = append(p.Effects, "reminder.create", "delivery.queue")
		if input.Kind == "task" || input.LinkedTaskID != "" {
			p.Effects = append(p.Effects, "reminder.link_task")
		}
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
	if input.NoteBody != "" {
		p.Effects = append(p.Effects, "note.create")
		if input.Kind == "task" || input.LinkedTaskID != "" {
			p.Effects = append(p.Effects, "note.link_task")
		}
		if input.ReminderAt != "" {
			p.Effects = append(p.Effects, "note.link_reminder")
		}
	}
	input.PreviewID = fingerprint("capture.v1", input.Title, input.Details, input.DueAt, input.ReminderAt, input.ReminderTitle, input.Timezone, input.Repeat)
	if input.Kind != "task" || input.NoteBody != "" {
		input.PreviewID = fingerprint("capture.v2", input.Kind, input.Title, input.Details, input.DueAt, input.ReminderAt, input.ReminderTitle, input.Timezone, input.Repeat, input.NoteBody)
	}
	if input.BeforeTaskID != "" {
		input.PreviewID = fingerprint("capture.v3", input.Kind, input.Title, input.Details, input.DueAt, input.ReminderAt, input.ReminderTitle, input.Timezone, input.Repeat, input.NoteBody, input.BeforeTaskID, input.BeforeTaskVersion)
	}
	if input.LinkedTaskID != "" {
		input.PreviewID = fingerprint("capture.v4", input.Kind, input.Title, input.Details, input.DueAt, input.ReminderAt, input.ReminderTitle, input.Timezone, input.Repeat, input.NoteBody, input.LinkedTaskID)
		p.Effects = append(p.Effects, "task.link_existing")
	}
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
		if saved.Dependencies == nil {
			saved.Dependencies = []TaskDependency{}
		}
		return saved, true, tx.Commit()
	}

	if input.BeforeTaskID != "" {
		target, e := readTask(ctx, tx, input.BeforeTaskID)
		if errors.Is(e, ErrNotFound) {
			return saved, false, ErrCaptureContext
		}
		if e != nil {
			return saved, false, e
		}
		if target.Status != "open" || taskVersion(target) != input.BeforeTaskVersion {
			return saved, false, ErrCaptureContext
		}
	}
	taskID, reminderID := input.LinkedTaskID, ""
	timestamp := now()
	saved = TaskAction{Dependencies: []TaskDependency{}, Reminders: []Reminder{}, Deliveries: []Delivery{}, Notes: []Note{}}
	linkedTitle := ""
	if taskID != "" {
		var linked Task
		linked, err = readTask(ctx, tx, taskID)
		if err != nil {
			return saved, false, err
		}
		if input.Kind == "reminder" && linked.Status != "open" {
			return saved, false, ErrTaskReminderConflict
		}
		linkedTitle = linked.Title
	}
	if input.Kind == "task" {
		taskID, err = newID()
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO tasks(id,title,status,created_at,updated_at,details,due_at) VALUES(?,?,'open',?,?,?,?)", taskID, input.Title, timestamp, timestamp, input.Details, input.DueAt)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp) VALUES(?,'task.created',?)", taskID, timestamp)
		}
	}
	if err == nil && input.BeforeTaskID != "" {
		err = addDependency(ctx, tx, taskID, input.BeforeTaskID)
	}
	if err == nil && input.ReminderAt != "" {
		reminderID, err = newID()
		anchor := ""
		taskTitle := linkedTitle
		if taskID != "" {
			if input.Kind == "task" {
				taskTitle = input.Title
			}
		}
		if input.Repeat != "" {
			anchor = input.ReminderAt
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO reminders(id,title,status,scheduled_at,timezone,created_at,updated_at,task_id,task_title,repeat,repeat_anchor) VALUES(?,?,'scheduled',?,?,?,?,?,?,?,?)", reminderID, input.ReminderTitle, input.ReminderAt, input.Timezone, timestamp, timestamp, taskID, taskTitle, input.Repeat, anchor)
		}
		if err == nil {
			err = queueDelivery(ctx, tx, reminderID, input.ReminderAt)
		}
		if err == nil {
			err = reminderActivity(ctx, tx, reminderID, "reminder.scheduled", timestamp)
		}
	}
	if err == nil && input.NoteBody != "" {
		var note Note
		note, err = createNoteInTransaction(ctx, tx, input.NoteBody, taskID, reminderID, timestamp)
		if err == nil {
			saved.Notes = append(saved.Notes, note)
		}
	}
	if err == nil && taskID != "" {
		saved, err = taskAction(ctx, tx, taskID, false)
	}
	if err == nil && taskID == "" && reminderID != "" {
		var reminder Reminder
		reminder, err = readReminder(ctx, tx, reminderID)
		if err == nil {
			saved.Reminders = append(saved.Reminders, reminder)
			saved.Deliveries, err = readDeliveries(ctx, tx, "reminder_id=?", reminderID)
		}
	}
	if err == nil {
		err = saveReceipt(ctx, tx, "capture.commit", key, input.PreviewID, saved)
	}
	if err == nil {
		err = tx.Commit()
	}
	return saved, false, err
}
