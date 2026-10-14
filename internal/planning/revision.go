package planning

import (
	"atlas/internal/routing"
	"strings"
)

// RevisionChoices uses the same Evaluator boundary as conversation corrections.
// A single choice identifies both action and field; siblings are never replanned.
func RevisionChoices(p Plan) []routing.Action {
	out := []routing.Action{{ID: "clarify", Description: "The intended action/field is ambiguous, several fields change, or this is a save, cancel, new request, or unsupported change."}}
	for _, a := range p.Actions {
		fields := []string{}
		switch a.Capability {
		case "task.create":
			fields = []string{"title", "due_at"}
		case "reminder.create":
			fields = []string{"title", "scheduled_at"}
		case "note.create":
			fields = []string{"body"}
		}
		for _, f := range fields {
			out = append(out, routing.Action{ID: a.ID + "." + f, Description: "Change only " + f + " of " + a.ID + " (" + a.Capability + "): " + a.Source})
		}
	}
	return out
}
func RevisionTarget(choice routing.Choice, p Plan) (string, string, bool) {
	if routing.ValidateChoice(choice, RevisionChoices(p)) != nil || !confident(choice) || choice.Choice == "clarify" {
		return "", "", false
	}
	parts := strings.Split(choice.Choice, ".")
	return parts[0], parts[1], true
}
func Revise(p Plan, id, field, value, source string) (Result, error) {
	if _, err := Inspect(p); err != nil {
		return Result{}, err
	}
	if len(source) > 4000 || strings.TrimSpace(value) == "" || !strings.Contains(source, value) {
		return Result{}, routing.ErrRequest
	}
	allowed := false
	for _, c := range RevisionChoices(p) {
		if c.ID == id+"."+field {
			allowed = true
		}
	}
	if !allowed {
		return Result{}, routing.ErrRequest
	}
	p.Actions = append([]Action(nil), p.Actions...)
	p.Revision++
	for i, a := range p.Actions {
		if a.ID != id {
			continue
		}
		a.Source += "\n" + source
		switch field {
		case "title":
			a.Arguments.Title = value
		case "body":
			a.Arguments.Body = value
		case "due_at", "scheduled_at":
			tv := &TimeValue{Literal: value}
			if validInstant(value) {
				tv.At = value
			}
			if field == "due_at" {
				a.Arguments.DueAt = tv
			} else {
				a.Arguments.ScheduledAt = tv
				a.DependsOn = []string{}
			}
		}
		p.Actions[i] = a
	}
	out, err := Inspect(p)
	out.ChangedActionID = id
	return out, err
}
