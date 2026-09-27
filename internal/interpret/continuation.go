package interpret

import "atlas/internal/store"

// Continuation describes allowed next requests. It does not execute them or
// authorize confirmation. Clients apply answers to the given request fields.
type Continuation struct {
	Version              string                 `json:"version"`
	State                string                 `json:"state"`
	ConfirmationRequired bool                   `json:"confirmation_required"`
	Persisted            bool                   `json:"persisted"`
	Clarifications       []ClarificationRequest `json:"clarifications"`
	NextCalls            []ContinuationCall     `json:"next_calls"`
}
type ContinuationCall struct {
	ID                   string   `json:"id"`
	Method               string   `json:"method"`
	Path                 string   `json:"path"`
	Body                 any      `json:"body"`
	RequiredHeaders      []string `json:"required_headers"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
}
type ClarificationChoice struct {
	Value any         `json:"value"`
	Label string      `json:"label"`
	Task  *store.Task `json:"task,omitempty"`
}
type ClarificationAnswer struct {
	ID             string                `json:"id"`
	ValueType      string                `json:"value_type"`
	InputField     string                `json:"input_field"`
	CallID         string                `json:"call_id"`
	SetFields      map[string]any        `json:"set_fields"`
	FollowupCallID string                `json:"followup_call_id,omitempty"`
	Choices        []ClarificationChoice `json:"choices"`
}
type ClarificationRequest struct {
	ID               string                `json:"id"`
	Code             string                `json:"code"`
	Field            string                `json:"field"`
	Prompt           string                `json:"prompt"`
	Required         bool                  `json:"required"`
	ChoicesTruncated bool                  `json:"choices_truncated"`
	Answers          []ClarificationAnswer `json:"answers"`
}

func WithContinuation(r Result, selected string, lead int) Result {
	if lead == 0 {
		lead = 60
	}
	c := Continuation{Version: "1", State: "awaiting_clarification", ConfirmationRequired: true, Persisted: false, Clarifications: []ClarificationRequest{}, NextCalls: []ContinuationCall{}}
	request := map[string]any{"text": r.Source, "timezone": r.Timezone, "context_task_id": selected, "reminder_lead_minutes": lead}
	c.NextCalls = append(c.NextCalls, ContinuationCall{"reinterpret", "POST", "/api/v1/capture/interpret", request, []string{}, false})
	draft := r.Draft
	draft.PreviewID = ""
	c.NextCalls = append(c.NextCalls, ContinuationCall{"preview_draft", "POST", "/api/v1/capture/preview", draft, []string{}, false})
	if r.Status == "ready" && r.Proposal != nil {
		c.State = "awaiting_confirmation"
		c.NextCalls = append(c.NextCalls, ContinuationCall{"confirm", "POST", "/api/v1/capture/commit", r.Proposal.Input, []string{"Idempotency-Key"}, true})
	}
	for _, q := range r.Questions {
		item := ClarificationRequest{ID: q.Field + ":" + q.Code, Code: q.Code, Field: q.Field, Prompt: q.Message, Required: true, Answers: []ClarificationAnswer{}, ChoicesTruncated: false}
		answer := func(id, typ, field, call string, set map[string]any, choices []ClarificationChoice) {
			item.Answers = append(item.Answers, ClarificationAnswer{ID: id, ValueType: typ, InputField: field, CallID: call, SetFields: set, Choices: choices})
		}
		switch q.Field {
		case "before_task_id":
			choices := []ClarificationChoice{}
			for _, t := range r.Candidates {
				copy := t
				choices = append(choices, ClarificationChoice{t.ID, t.Title, &copy})
			}
			item.ChoicesTruncated = r.CandidatesTruncated
			answer("choose_context", "task_id", "context_task_id", "reinterpret", map[string]any{}, choices)
			answer("rewrite_reference", "text", "text", "reinterpret", map[string]any{}, []ClarificationChoice{})
		case "reminder_at":
			answer("set_delivery_time", "rfc3339_timestamp", "reminder_at", "preview_draft", map[string]any{"timezone": r.Timezone}, []ClarificationChoice{})
			answer("rewrite_time", "text", "text", "reinterpret", map[string]any{}, []ClarificationChoice{})
			if r.Draft.Kind == "task" {
				answer("save_without_reminder", "none", "", "preview_draft", map[string]any{"reminder_at": "", "reminder_title": "", "timezone": "", "repeat": ""}, []ClarificationChoice{})
			}
			if q.Code == "context_time_past" {
				answer("choose_lead_time", "integer", "reminder_lead_minutes", "reinterpret", map[string]any{}, []ClarificationChoice{{15, "15 minutes", nil}, {60, "1 hour", nil}, {180, "3 hours", nil}, {1440, "1 day", nil}})
			}
			if q.Code == "context_missing_deadline" && r.Reference != nil {
				id := "set_reference_deadline"
				c.NextCalls = append(c.NextCalls, ContinuationCall{id, "PATCH", "/api/v1/tasks/" + r.Reference.ID, map[string]any{}, []string{}, true})
				answer(id, "rfc3339_timestamp", "due_at", id, map[string]any{}, []ClarificationChoice{})
				item.Answers[len(item.Answers)-1].FollowupCallID = "reinterpret"
			}
		case "kind":
			answer("choose_kind", "enum", "kind", "preview_draft", map[string]any{}, []ClarificationChoice{{"task", "Task", nil}, {"reminder", "Reminder", nil}, {"note", "Note", nil}})
		case "due_at":
			answer("set_deadline", "rfc3339_timestamp", "due_at", "preview_draft", map[string]any{}, []ClarificationChoice{})
		default:
			answer("rewrite_sentence", "text", "text", "reinterpret", map[string]any{}, []ClarificationChoice{})
		}
		c.Clarifications = append(c.Clarifications, item)
	}
	r.Continuation = &c
	return r
}
