package interpret

import (
	"atlas/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestReplyPreservesQuestionsAndReferenceTime(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Date(2089, 1, 1, 8, 0, 0, 0, time.UTC)
	r, e := InterpretWithContext(t.Context(), s, "Something tomorrow at 6", "UTC", "", at)
	if e != nil {
		t.Fatal(e)
	}
	r, _, _, action, e := Reply(t.Context(), s, r, "", 60, "tomorrow at 9am", at)
	if e != nil || action != "" || r.Status != "needs_clarification" {
		t.Fatal(r, action, e)
	}
	if len(r.Questions) != 1 || r.Questions[0].Field != "kind" {
		t.Fatal(r.Questions)
	}
	r, _, _, _, e = Reply(t.Context(), s, r, "", 60, "task", at)
	if e != nil || r.Status != "ready" || r.Draft.Title != "Something" || r.Draft.ReminderAt == "" {
		t.Fatal(r, e)
	}
	s.Create(t.Context(), "Movie with Mummy")
	s.Create(t.Context(), "Movie with Friends")
	r, e = InterpretWithContext(t.Context(), s, "Remind me to book tickets for movie tomorrow at 9am", "UTC", "", at)
	if e != nil {
		t.Fatal(e)
	}
	r, _, _, action, e = Reply(t.Context(), s, r, "", 60, "the movie one", at.AddDate(0, 0, 2))
	if e != nil || action != "unresolved" || r.Proposal != nil {
		t.Fatal(r, action, e)
	}
	r, _, _, _, e = Reply(t.Context(), s, r, "", 60, "the Mummy one", at.AddDate(0, 0, 2))
	if e != nil || r.Status != "ready" || r.Draft.ReminderAt != "2089-01-02T09:00:00.000000000Z" {
		t.Fatal(r, e)
	}
}

func TestInterviewEventAndClockReply(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	r, e := InterpretWithContext(t.Context(), s, "I have an interview at Ather on tuesday", "UTC", "", at)
	if e != nil || !r.Event || r.Draft.Kind != "task" || r.Draft.Title != "interview at Ather" || len(r.Questions) != 1 || r.Questions[0].Code != "missing_clock" || r.Proposal != nil {
		t.Fatal(r, e)
	}
	r, _, _, action, e := Reply(t.Context(), s, r, "", 60, "action", at)
	if e != nil || action == "unresolved" || len(r.Questions) != 1 || r.Questions[0].Field != "due_at" {
		t.Fatal(r, action, e)
	}
	r, _, _, _, e = Reply(t.Context(), s, r, "", 60, "3pm", at.AddDate(0, 0, 1))
	if e != nil || r.Status != "ready" || !sameInstant(r.Draft.DueAt, "2026-09-29T15:00:00Z") || !sameInstant(r.Draft.ReminderAt, r.Draft.DueAt) {
		t.Fatal(r, e)
	}
	saved, _, e := s.CommitCapture(t.Context(), "interview-test", r.Proposal.Input)
	if e != nil || saved.TaskID == "" || len(saved.Reminders) != 1 {
		t.Fatal(saved, e)
	}
	for _, sentence := range []string{"I have an interview on Tuesday at 3pm", "I've got an interview on Tuesday at 3pm"} {
		r, e := Interpret(sentence, "UTC", at)
		if e != nil || r.Status != "ready" || !r.Event || r.Draft.DueAt == "" || r.Draft.ReminderAt == "" {
			t.Fatal(r, e)
		}
	}
	// A date-only non-event clock clarification also keeps its original day.
	r, _ = Interpret("Call Mom on Tuesday", "UTC", at)
	r, _, _, _, e = Reply(t.Context(), s, r, "", 60, "at 9am", at)
	if e != nil || !sameInstant(r.Draft.ReminderAt, "2026-09-29T09:00:00Z") {
		t.Fatal(r, e)
	}
	r, _ = Interpret("I have an account at Ather", "UTC", at)
	if r.Event || r.Status == "ready" {
		t.Fatal(r)
	}
}
func TestActionRepliesAndLegacyInterviewSession(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, reply := range []string{"action", "an action", "it's an action", "this is a task", "add it as a task"} {
		r, _ := Interpret("Something tomorrow at 9am", "UTC", at)
		r, _, _, action, e := Reply(t.Context(), s, r, "", 60, reply, at)
		if e != nil || action == "unresolved" || r.Status != "ready" || r.Draft.Kind != "task" {
			t.Fatal(reply, r, action, e)
		}
	}
	r, _ := Interpret("Something on Tuesday", "UTC", at)
	r.Source = "I have an interview at Ather on tuesday"
	r, _, _, _, e = Reply(t.Context(), s, r, "", 60, "action", at)
	if e != nil || !r.Event || r.Draft.Title != "interview at Ather" || len(r.Questions) != 1 || r.Questions[0].Code != "missing_clock" {
		t.Fatal(r, e)
	}
}
