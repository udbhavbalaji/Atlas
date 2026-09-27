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
