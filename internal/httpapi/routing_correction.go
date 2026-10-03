package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var spokenClock = regexp.MustCompile(`(?i)\b(?:by|at)\s+(\d{1,2}(?:[:.]\d{2})?\s*(?:a\.?m\.?|p\.?m\.?)?)\b`)
var statedValue = regexp.MustCompile(`(?i)\b(?:to|as|that)\s+(.+)$`)

// Jev selects which part of an existing proposal a natural-language follow-up
// changes. Atlas still parses and validates the value before revising the draft.
func (c *routingConversation) naturalCorrection(ctx context.Context, service routingService, text string) bool {
	if c.Route.Provider == "mock" || service.jev == nil || c.Channel != "tasks" && c.Channel != "reminders" && c.Channel != "notes" {
		return false
	}
	c.CorrectionAttempted = true
	actions := []routing.Action{
		{ID: "save", Description: "The user accepts the current pending proposal and wants it saved."},
		{ID: "cancel", Description: "The user wants to abandon the current pending proposal."},
		{ID: "deadline", Description: "The user is specifying when the pending task must be finished or an event must happen; revise the task deadline."},
		{ID: "reminder", Description: "The user is specifying when Atlas should notify them; revise the reminder time."},
		{ID: "both", Description: "The user explicitly wants both the task deadline and reminder time changed."},
		{ID: "title", Description: "The user wants a different title for the pending task or reminder."},
		{ID: "details", Description: "The user wants to change the task details."},
		{ID: "note_body", Description: "The user wants to change the pending note text."},
		{ID: "skip_reminder", Description: "The user wants no reminder on the pending task."},
		{ID: "skip_note", Description: "The user wants no attached note on the pending task or reminder."},
		{ID: "repeat", Description: "The user wants the pending reminder to recur daily, weekly, or not at all."},
		{ID: "new_request", Description: "The user has started a separate request instead of changing or accepting the pending proposal."},
		{ID: "clarify", Description: "The requested change cannot be identified from the current draft and follow-up."},
	}
	state := routing.State{Text: fmt.Sprintf("Pending %s draft: title %q, deadline %q, reminder %q. Latest user follow-up: %q", c.Channel, c.Draft.Fields.Title, c.Draft.Fields.DueAt, c.Draft.Fields.ReminderAt, text), Timezone: c.Route.Input.Timezone, ReferenceAt: c.Route.Input.ReferenceAt}
	evaluation, err := service.jev.Evaluate(ctx, state, actions, "")
	if err != nil {
		c.Warnings = append(c.Warnings, "I could not interpret that correction right now. Please name the field and new value.")
		return false
	}
	selected := evaluation.Decision.Choice
	winning := evaluation.Decision.Probabilities[selected]
	second := 0.0
	for choice, probability := range evaluation.Decision.Probabilities {
		if choice != selected && probability > second {
			second = probability
		}
	}
	if winning < 0.65 || winning-second < 0.15 || selected == "clarify" || selected == "new_request" {
		c.Warnings = append(c.Warnings, "I could not tell which part of the draft to change. Please say, for example, ‘change deadline to 7:45 am’.")
		return false
	}
	if selected == "save" {
		c.State = "confirming"
		return true
	}
	if selected == "cancel" {
		c.State = "cancelled"
		c.Proposal = nil
		return true
	}
	if selected == "skip_reminder" && c.Channel == "tasks" || selected == "skip_note" && c.Channel != "notes" {
		c.Warnings = nil
		if selected == "skip_reminder" {
			return c.correction("reminder", "skip")
		}
		return c.correction("note", "skip")
	}
	if selected == "title" && c.Channel != "notes" || selected == "details" && c.Channel == "tasks" || selected == "note_body" {
		match := statedValue.FindStringSubmatch(text)
		if len(match) == 2 {
			field := selected
			if selected == "note_body" && c.Channel == "notes" {
				field = "note_body"
			}
			c.Warnings = nil
			return c.correction(field, strings.TrimSpace(strings.TrimRight(match[1], ".!")))
		}
		c.Warnings = append(c.Warnings, "I understood which text you want changed. Please state its new value with ‘change "+strings.ReplaceAll(selected, "_", " ")+" to …’.")
		return false
	}
	if selected == "repeat" && c.Channel != "notes" {
		for _, repeat := range []string{"daily", "weekly", "none"} {
			if strings.Contains(strings.ToLower(text), repeat) {
				c.Warnings = nil
				return c.correction("repeat", repeat)
			}
		}
		c.Warnings = append(c.Warnings, "Should the reminder repeat daily, weekly, or not at all?")
		return false
	}
	if selected == "deadline" && c.Channel != "tasks" {
		c.Warnings = append(c.Warnings, "This is a reminder draft. Please say when you want the reminder.")
		return false
	}
	clock := spokenClock.FindStringSubmatch(text)
	if len(clock) < 2 {
		c.Warnings = append(c.Warnings, "I understood which time you want changed, but could not read the new clock time. Please include am or pm.")
		return false
	}
	phrase := strings.TrimSpace(clock[1])
	lower := strings.ToLower(text)
	if strings.Contains(lower, "tomorrow") {
		phrase = "tomorrow at " + phrase
	} else if strings.Contains(lower, "today") {
		phrase = "today at " + phrase
	} else {
		anchor := c.Draft.Fields.DueAt
		if anchor == "" {
			anchor = c.Draft.Fields.ReminderAt
		}
		if at, parseErr := time.Parse(time.RFC3339Nano, anchor); parseErr == nil {
			if loc, zoneErr := time.LoadLocation(c.Route.Input.Timezone); zoneErr == nil {
				phrase = at.In(loc).Format("2006-01-02") + " at " + phrase
			}
		}
	}
	field := "reminder_at"
	if selected == "deadline" || selected == "both" {
		field = "due_at"
	}
	instant, _, ok := interpret.ResolveTime(phrase, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, field)
	if !ok {
		c.Warnings = append(c.Warnings, "I could not resolve that time safely. Please include a date and am or pm.")
		return false
	}
	if selected == "deadline" || selected == "both" {
		c.Draft.Fields.DueAt = instant
	}
	c.Warnings = nil
	if selected == "reminder" || selected == "both" {
		c.Draft.Fields.ReminderAt = instant
		if c.Channel == "tasks" {
			c.Draft.Reminder = "add"
		}
	} else if reminder, parseErr := time.Parse(time.RFC3339Nano, c.Draft.Fields.ReminderAt); parseErr == nil {
		if deadline, deadlineErr := time.Parse(time.RFC3339Nano, instant); deadlineErr == nil && !reminder.Before(deadline) {
			c.Draft.Fields.ReminderAt = ""
			c.Draft.Reminder = "skip"
			c.Warnings = append(c.Warnings, "I removed the earlier reminder because it would have occurred after the new deadline. You can add a new reminder before saving.")
		}
	}
	c.Draft.Fields.PreviewID = ""
	c.Draft.Fields.BeforeTaskVersion = ""
	return true
}

func (c *routingConversation) naturalChoiceAnswer(ctx context.Context, service routingService, text string) bool {
	if c.Route.Provider == "mock" || service.jev == nil || c.Question == nil || c.Question.ValueType != "choice" {
		return false
	}
	if c.Question.Field != "reminder" && c.Question.Field != "note" {
		return false
	}
	actions := []routing.Action{
		{ID: "add", Description: "The user wants the offered " + c.Question.Field + " included."},
		{ID: "skip", Description: "The user declines the offered " + c.Question.Field + "."},
		{ID: "clarify", Description: "The user did not answer whether to add the offered " + c.Question.Field + "."},
	}
	state := routing.State{Text: fmt.Sprintf("Atlas asked: %q. User replied: %q", c.Question.Prompt, text), Timezone: c.Route.Input.Timezone, ReferenceAt: c.Route.Input.ReferenceAt}
	evaluation, err := service.jev.Evaluate(ctx, state, actions, "")
	if err != nil || evaluation.Decision.Choice == "clarify" {
		return false
	}
	choice := evaluation.Decision.Choice
	if evaluation.Decision.Probabilities[choice] < 0.65 {
		return false
	}
	return c.answer(c.Question.Field, choice)
}

func (c *routingConversation) naturalRouteChoice(ctx context.Context, service routingService, text string) string {
	if c.Route.Provider == "mock" || service.jev == nil {
		return ""
	}
	actions := []routing.Action{
		{ID: "task", Description: "The user chooses a task or action to do."},
		{ID: "reminder", Description: "The user chooses a reminder or notification."},
		{ID: "note", Description: "The user chooses to retain information as a note."},
		{ID: "lookup", Description: "The user wants to look up existing saved information."},
		{ID: "edit", Description: "The user wants to change an existing saved record."},
		{ID: "delete", Description: "The user wants to delete an existing saved record."},
		{ID: "clarify", Description: "The user has not specified which Atlas action they want."},
	}
	state := routing.State{Text: fmt.Sprintf("Original request: %q. Atlas asked which action to take. User clarification: %q", c.Route.Input.Text, text), Timezone: c.Route.Input.Timezone, ReferenceAt: c.Route.Input.ReferenceAt, Context: c.Route.Input.Context}
	evaluation, err := service.jev.Evaluate(ctx, state, actions, "")
	if err != nil || evaluation.Decision.Choice == "clarify" {
		return ""
	}
	choice := evaluation.Decision.Choice
	if evaluation.Decision.Probabilities[choice] < 0.65 {
		return ""
	}
	return selectedChannel(choice)
}
