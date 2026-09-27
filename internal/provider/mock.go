package provider

import (
	"atlas/internal/store"
	"context"
)

// Mock uses explicit fixtures/answers. It does not understand the input sentence.
type Mock struct{}

func (Mock) Name() string { return "mock-fixtures" }
func (Mock) Propose(ctx context.Context, r Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	out := Response{Version: Version, Status: "ready", Questions: []Question{}, ContextRequests: []ContextRequest{}, Explanation: "MOCK: scenario-driven fixture, not Jev or English interpretation. Text is used verbatim as the demo title/body."}
	switch r.Scenario {
	case "unavailable":
		return out, ErrUnavailable
	case "invalid":
		out.Version = "invalid"
		out.Draft = &store.CaptureInput{Kind: "delete_all"}
		return out, nil
	case "note":
		out.Draft = &store.CaptureInput{Kind: "note", NoteBody: r.Text}
		return out, nil
	case "task", "context":
	default:
		return out, ErrRequest
	}
	draft := store.CaptureInput{Kind: "task", Title: r.Text}
	ask := func(id, field, prompt string, choices []Choice) {
		out.Questions = append(out.Questions, Question{id, field, prompt, true, choices})
	}
	if r.Scenario == "context" {
		if len(r.Context) == 0 {
			out.Status = "needs_context"
			out.ContextRequests = []ContextRequest{{r.ContextQuery, 5}}
			return out, nil
		}
		choices := []Choice{}
		matched := false
		for _, t := range r.Context[0].Records {
			choices = append(choices, Choice{t.ID, t.Title})
			if t.ID == r.Answers.ContextTaskID {
				draft.BeforeTaskID = t.ID
				matched = true
			}
		}
		if !matched {
			ask("context", "context_task_id", "Choose an open task from the bounded context results. If none match, change the mock context query.", choices)
		}
	}
	if r.Answers.Reminder != "add" && r.Answers.Reminder != "skip" {
		ask("optional_reminder", "reminder", "Would you like a reminder, or save without one?", []Choice{{"add", "Add reminder"}, {"skip", "Skip reminder"}})
	} else if r.Answers.Reminder == "add" {
		if r.Answers.ReminderAt == "" {
			ask("reminder_time", "reminder_at", "Choose an explicit reminder time.", []Choice{})
		} else {
			draft.ReminderAt = r.Answers.ReminderAt
			draft.Timezone = r.Timezone
		}
	}
	if r.Answers.Note != "add" && r.Answers.Note != "skip" {
		ask("optional_note", "note", "Would you like to attach a note?", []Choice{{"add", "Add note"}, {"skip", "Skip note"}})
	} else if r.Answers.Note == "add" {
		if r.Answers.NoteBody == "" {
			ask("note_body", "note_body", "Enter the note to attach.", []Choice{})
		} else {
			draft.NoteBody = r.Answers.NoteBody
		}
	}
	if len(out.Questions) > 0 {
		out.Status = "needs_clarification"
	}
	out.Draft = &draft
	return out, nil
}
