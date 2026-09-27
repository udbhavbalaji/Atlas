package interpret

import (
	"atlas/internal/store"
	"context"
	"regexp"
	"strings"
	"time"
)

// Reply applies a supported clarification to the saved draft. Unknown replies
// keep the question open rather than silently replacing the original request.
func Reply(ctx context.Context, s *store.Store, r Result, selected string, lead int, text string, at time.Time) (Result, string, int, string, error) {
	reference, err := time.Parse(time.RFC3339Nano, r.ReferenceAt)
	if err != nil {
		return r, selected, lead, "", err
	}
	answer := strings.Trim(strings.ToLower(strings.TrimSpace(text)), ".! ")
	if answer == "yes" || answer == "save it" || answer == "confirm" || answer == "yes save it" {
		return r, selected, lead, "confirm", nil
	}
	if answer == "cancel" || answer == "never mind" {
		return r, selected, lead, "cancel", nil
	}
	if strings.HasPrefix(strings.ToLower(text), "replace: ") {
		n, e := InterpretWithContextLead(ctx, s, strings.TrimSpace(text[9:]), r.Timezone, "", at, lead)
		return n, "", lead, "", e
	}
	switch answer {
	case "action", "an action", "it's an action", "it is an action", "this is an action", "this is a task", "add it as a task", "task and reminder":
		answer = "task"
	}
	// Upgrade an existing conversation that predated event recognition.
	if answer == "task" && !r.Event && eventPrefix.MatchString(r.Source) {
		n, e := InterpretWithContextLead(ctx, s, r.Source, r.Timezone, selected, reference, lead)
		return n, selected, lead, "", e
	}
	query := strings.TrimSuffix(strings.TrimPrefix(answer, "the "), " one")
	matches := []store.Task{}
	for _, t := range r.Candidates {
		if t.ID == answer || matchesTask(query, t) {
			matches = append(matches, t)
		}
	}
	if len(matches) == 1 {
		selected = matches[0].ID
		n, e := InterpretWithContextLead(ctx, s, r.Source, r.Timezone, selected, reference, lead)
		return n, selected, lead, "", e
	}
	for phrase, value := range map[string]int{"15 minutes before": 15, "one hour before": 60, "1 hour before": 60, "three hours before": 180, "3 hours before": 180, "one day before": 1440, "1 day before": 1440} {
		if answer == phrase {
			n, e := InterpretWithContextLead(ctx, s, r.Source, r.Timezone, selected, reference, value)
			return n, selected, value, "", e
		}
	}
	field := "reminder_at"
	for _, q := range r.Questions {
		if q.Field == "before_task_id" {
			return r, selected, lead, "unresolved", nil
		}
		if q.Field == "due_at" {
			field = "due_at"
		}
	}
	draft := r.Draft
	draft.PreviewID = ""
	changed := false
	handled := map[string]bool{"input": true}
	if answer == "task" || answer == "reminder" || answer == "note" {
		handled["kind"] = true
		draft.Kind = answer
		if answer == "note" {
			draft = store.CaptureInput{Kind: "note", NoteBody: r.Source}
			for _, q := range r.Questions {
				handled[q.Field] = true
			}
		}
		changed = true
	} else if answer == "without reminder" || answer == "no reminder" {
		if draft.Kind != "task" {
			return r, selected, lead, "unresolved", nil
		}
		handled["reminder_at"] = true
		draft.ReminderAt = ""
		draft.ReminderTitle = ""
		draft.Timezone = ""
		draft.Repeat = ""
		changed = true
	} else {
		loc, e := time.LoadLocation(r.Timezone)
		if e != nil {
			return r, selected, lead, "", e
		}
		timing := strings.TrimPrefix(answer, "at ")
		timeReference := at.In(loc)
		// A clock-only reply fills the original day, using its original reference.
		clockOnly := regexp.MustCompile(`(?i)^(?:at\s+)?(?:\d{1,2}(?::\d{2})?\s*(?:am|pm)?|noon|midnight)$`).MatchString(answer)
		if clockOnly {
			for _, q := range r.Questions {
				if q.Field == field && q.Code == "missing_clock" {
					source := r.Source
					if field == "due_at" {
						if m := linkedClause.FindStringIndex(source); m != nil {
							source = source[:m[0]]
						}
					}
					if m := noteClause.FindStringIndex(source); m != nil {
						source = source[:m[0]]
					}
					if m := timingStart.FindStringIndex(source); m != nil {
						timing = source[m[0]:] + " at " + timing
						timeReference = reference.In(loc)
					}
				}
			}
		}
		value, repeat, assumptions, questions := parseTime(timing, timeReference, loc, field)
		if value != "" && len(questions) == 0 {
			handled[field] = true
			if field == "due_at" {
				draft.DueAt = value
				if r.Event && draft.ReminderAt == "" && !linkedClause.MatchString(r.Source) {
					draft.ReminderAt = value
					draft.Timezone = r.Timezone
					r.Assumptions = append(r.Assumptions, "The event time sets the task deadline and a linked reminder at that time. Review before saving.")
				}
			} else {
				draft.ReminderAt = value
				draft.Timezone = r.Timezone
				draft.Repeat = repeat
			}
			r.Assumptions = append(r.Assumptions, assumptions...)
			changed = true
		}
	}
	if !changed {
		return r, selected, lead, "unresolved", nil
	}
	remaining := []Question{}
	for _, q := range r.Questions {
		if !handled[q.Field] {
			remaining = append(remaining, q)
		}
	}
	r.Questions = remaining
	r.Proposal = nil
	n, e := finish(r, draft)
	if e == nil && n.Status == "ready" {
		p, err := s.CapturePreview(ctx, n.Draft)
		if err != nil {
			return r, selected, lead, "", err
		}
		n.Proposal = &p
		n.Draft = p.Input
	}
	return n, selected, lead, "", e
}
