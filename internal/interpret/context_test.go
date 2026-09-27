package interpret

import (
	"atlas/internal/store"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestContextLookupAndPronounDisambiguation(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	interview, _ := s.Create(ctx, "Interview at Ather")
	for _, sentence := range []string{"I need to get my resume printed before that", "Print my resume before my interview at Ather", "Print my resume ahead of the interview; note: use good paper"} {
		r, e := InterpretWithContext(ctx, s, sentence, "UTC", "", time.Now())
		if e != nil || r.Status != "ready" || r.Proposal == nil || r.Draft.BeforeTaskID != interview.ID || r.Draft.ReminderAt != "" || r.Draft.DueAt != "" {
			t.Fatal(r, e)
		}
	}
	second, _ := s.Create(ctx, "Interview at Ather for engineering")
	r, e := InterpretWithContext(ctx, s, "Print resume before interview at Ather", "UTC", "", time.Now())
	if e != nil || r.Status != "needs_clarification" || len(r.Candidates) != 2 || r.Proposal != nil {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Print resume before that", "UTC", second.ID, time.Now())
	if e != nil || r.Status != "ready" || r.Draft.BeforeTaskID != second.ID {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Print resume before interview at OtherCo", "UTC", interview.ID, time.Now())
	if e != nil || r.Status != "needs_clarification" || r.Proposal != nil {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Remember that I must print my resume before the interview", "UTC", "", time.Now())
	if e != nil || r.Draft.Kind != "note" || r.Draft.BeforeTaskID != "" {
		t.Fatal(r, e)
	}
}

func TestContextIncompleteOffsetAndExplicitReminder(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	s.Create(ctx, "Interview at Ather")
	for _, sentence := range []string{"Print resume before", "Print resume one day before interview at Ather"} {
		r, e := InterpretWithContext(ctx, s, sentence, "UTC", "", time.Now())
		if e != nil || r.Status != "needs_clarification" || r.Proposal != nil || len(r.Questions) == 0 {
			t.Fatal(r, e)
		}
	}
	r, e := InterpretWithContext(ctx, s, "Print resume before interview at Ather; remind me tomorrow at 9am; note: use good paper", "UTC", "", time.Now())
	if e != nil || r.Status != "ready" || r.Draft.ReminderAt == "" || r.Draft.NoteBody != "use good paper" || r.Draft.BeforeTaskID == "" {
		t.Fatal(r, e)
	}
}
