package interpret

import (
	"atlas/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestContinuationChoicesAndResume(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Movie with mummy")
	s.Create(t.Context(), "Movie with friends")
	r, e := InterpretWithContext(t.Context(), s, "Remind me to book movie tickets", "UTC", "", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r = WithContinuation(r, "", 60)
	c := r.Continuation
	if c.Version != "1" || c.State != "awaiting_clarification" || c.Persisted || len(c.Clarifications) != 2 {
		t.Fatal(c)
	}
	var choose ClarificationAnswer
	for _, q := range c.Clarifications {
		if q.Field == "before_task_id" {
			choose = q.Answers[0]
		}
	}
	if choose.CallID != "reinterpret" || choose.InputField != "context_task_id" || len(choose.Choices) != 2 {
		t.Fatal(choose)
	}
	r, e = InterpretWithContext(t.Context(), s, "Remind me to book movie tickets", "UTC", a.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r = WithContinuation(r, a.ID, 60)
	q := r.Continuation.Clarifications[0]
	if q.Code != "context_missing_deadline" {
		t.Fatal(q)
	}
	var omit ClarificationAnswer
	for _, answer := range q.Answers {
		if answer.ID == "save_without_reminder" {
			omit = answer
		}
		if answer.ID == "set_reference_deadline" && answer.FollowupCallID != "reinterpret" {
			t.Fatal(answer)
		}
	}
	if omit.CallID != "preview_draft" || omit.SetFields["timezone"] != "" {
		t.Fatal(omit)
	}
	draft := r.Draft
	draft.Timezone = ""
	draft.ReminderAt = ""
	draft.ReminderTitle = ""
	draft.Repeat = ""
	p, e := s.CapturePreview(t.Context(), draft)
	if e != nil {
		t.Fatal(e)
	}
	ready := WithContinuation(Result{Status: "ready", Proposal: &p, Draft: p.Input}, "", 60)
	if ready.Continuation.State != "awaiting_confirmation" || len(ready.Continuation.Clarifications) != 0 || !ready.Continuation.NextCalls[2].RequiresConfirmation {
		t.Fatal(ready)
	}
}
