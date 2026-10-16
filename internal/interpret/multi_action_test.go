package interpret

import (
	"testing"
	"time"
)

func TestTaskNoteReminderClauseOrder(t *testing.T) {
	reference := time.Date(2030, 1, 7, 10, 0, 0, 0, time.UTC)
	cases := []string{
		"I need to prepare for my interview by tomorrow at 6pm, note that bring my portfolio and remind me tomorrow at 5pm",
		"I need to prepare for my interview by tomorrow at 6pm and remind me tomorrow at 5pm, note that bring my portfolio",
		"I need to prepare for my interview by tomorrow at 6pm; note: bring my portfolio; remind me tomorrow at 5pm",
		"I need to prepare for my interview by tomorrow at 6pm, remind me tomorrow at 5pm and note that bring my portfolio",
	}
	for _, sentence := range cases {
		t.Run(sentence, func(t *testing.T) {
			result, err := Interpret(sentence, "UTC", reference)
			if err != nil || result.Draft.Title != "prepare for my interview" || result.Draft.NoteBody != "bring my portfolio" || result.Draft.DueAt != "2030-01-08T18:00:00.000000000Z" || result.Draft.ReminderAt != "2030-01-08T17:00:00.000000000Z" {
				t.Fatalf("draft=%+v questions=%+v err=%v", result.Draft, result.Questions, err)
			}
		})
	}
}

func TestIndianDayMonthAndDotClock(t *testing.T) {
	instant, _, ok := ResolveTime("4 October at 7.30pm", "Asia/Kolkata", "2026-10-02T03:00:00Z", "due_at")
	if !ok || instant != "2026-10-04T14:00:00Z" {
		t.Fatal(instant, ok)
	}
	reference := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	result, err := Interpret("I need to prepare for my interview by 4 October at 7.30pm", "Asia/Kolkata", reference)
	if err != nil || result.Draft.DueAt != "2026-10-04T14:00:00.000000000Z" {
		t.Fatal(result.Draft, result.Questions, err)
	}
}
