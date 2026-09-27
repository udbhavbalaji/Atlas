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

func TestRelatedReminderTiming(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	interview, _ := s.CreateWithFields(ctx, "Interview at Ather", "", "2030-01-03T09:00:00Z")
	movie, _ := s.Create(ctx, "Go for a movie with mummy")
	for _, sentence := range []string{"Remind me to print resume before interview at Ather", "Remind me to print resume"} {
		r, e := InterpretWithContext(ctx, s, sentence, "UTC", "", now)
		if e != nil || r.Status != "ready" || r.Draft.Kind != "task" || r.Draft.BeforeTaskID != interview.ID || !equalInstant(r.Draft.ReminderAt, "2030-01-03T08:00:00Z") || len(r.Questions) != 0 {
			t.Fatal(r, e)
		}
	}
	r, e := InterpretWithContext(ctx, s, "Remind me to print resume one day before interview at Ather", "UTC", "", now)
	if e != nil || r.Status != "ready" || !equalInstant(r.Draft.ReminderAt, "2030-01-02T09:00:00Z") {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Remind me to book movie tickets", "UTC", "", now)
	if e != nil || r.Status != "needs_clarification" || r.Reference == nil || r.Reference.ID != movie.ID || r.Draft.BeforeTaskID != movie.ID || r.Draft.Kind != "task" || r.Proposal != nil || len(r.Questions) != 1 || r.Questions[0].Code != "context_missing_deadline" {
		t.Fatal(r, e)
	}
	due := "2030-01-04T18:00:00Z"
	s.Patch(ctx, movie.ID, store.TaskPatch{DueAt: &due})
	r, e = InterpretWithContextLead(ctx, s, "Remind me to book movie tickets", "UTC", "", now, 180)
	if e != nil || r.Status != "ready" || !equalInstant(r.Draft.ReminderAt, "2030-01-04T15:00:00Z") || r.Draft.BeforeTaskID != movie.ID {
		t.Fatal(r, e)
	}
	saved, _, e := s.CommitCapture(ctx, "movie-context", r.Draft)
	if e != nil || len(saved.Dependencies) != 1 || len(saved.Reminders) != 1 || saved.Reminders[0].TaskID != saved.TaskID {
		t.Fatal(saved, e)
	}
	// An explicit delivery time overrides the lead-time suggestion.
	r, e = InterpretWithContext(ctx, s, "Remind me to book movie tickets tomorrow at 9am", "UTC", movie.ID, now)
	if e != nil || r.Status != "ready" || !equalInstant(r.Draft.ReminderAt, "2030-01-02T09:00:00Z") {
		t.Fatal(r, e)
	}
}

func TestRelatedAmbiguityPastAndDST(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	first, _ := s.CreateWithFields(ctx, "Movie with mummy", "", "2030-01-02T18:00:00Z")
	s.Create(ctx, "Movie with friends")
	r, e := InterpretWithContext(ctx, s, "Remind me to book movie tickets", "UTC", "", now)
	if e != nil || r.Proposal != nil || len(r.Candidates) != 2 {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Remind me to book movie tickets", "UTC", first.ID, now)
	if e != nil || r.Status != "ready" || r.Draft.BeforeTaskID != first.ID {
		t.Fatal(r, e)
	}
	r, e = InterpretWithContext(ctx, s, "Remind me to book movie tickets", "UTC", first.ID, now.Add(48*time.Hour))
	if e != nil || r.Proposal != nil || len(r.Questions) != 1 || r.Questions[0].Code != "context_time_past" {
		t.Fatal(r, e)
	}
	event, _ := s.CreateWithFields(ctx, "Interview", "", "2030-03-11T06:30:00Z") // 2:30am New York; preceding day is DST gap.
	r, e = InterpretWithContext(ctx, s, "Remind me to print resume one day before interview", "America/New_York", event.ID, now)
	if e != nil || r.Proposal != nil || len(r.Questions) != 1 || r.Questions[0].Code != "dst_context_time" {
		t.Fatal(r, e)
	}
	for _, sentence := range []string{"Remember that movie tickets cost more", "Remind me to drink water in 10 minutes", "Book movie tickets before"} {
		r, e = InterpretWithContext(ctx, s, sentence, "UTC", "", now)
		if e != nil || r.Reference != nil || r.Draft.BeforeTaskID != "" {
			t.Fatal(r, e)
		}
	}
}

func equalInstant(actual, expected string) bool {
	a, e := time.Parse(time.RFC3339Nano, actual)
	b, f := time.Parse(time.RFC3339Nano, expected)
	return e == nil && f == nil && a.Equal(b)
}
