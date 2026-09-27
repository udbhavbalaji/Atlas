package interpret

import (
	"testing"
	"time"
)

func TestSentenceInterpretation(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct{ text, kind, title, reminder, due, repeat, note string }{
		{"Remind me tomorrow to call Mom at 6pm", "reminder", "call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Tomorrow at 6pm, remind me to call Mom", "reminder", "call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Call Mom tomorrow at six pm", "task", "Call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Call Mom tomorrow evening at 6", "task", "Call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Remind me to call Mom tomorrow morning at 9", "reminder", "call Mom", "2026-09-28T03:30:00Z", "", "", ""},
		{"Could you please remind me to call Mom tomorrow at 6pm", "reminder", "call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"I need to remember that the gate code is 1234", "note", "", "", "", "", "the gate code is 1234"},
		{"Buy milk and make a note: get oat milk", "task", "Buy milk", "", "", "", "get oat milk"},
		{"Call Mom 6pm", "task", "Call Mom", "2026-09-27T12:30:00Z", "", "", ""},
		{"Buy milk; note: get oat milk\nCheck the brand", "task", "Buy milk", "", "", "", "get oat milk\nCheck the brand"},
		{"Buy milk", "task", "Buy milk", "", "", "", ""},
		{"Call Mom tomorrow at 6pm", "task", "Call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Remind me to call Mom tomorrow at 6pm", "reminder", "call Mom", "2026-09-28T12:30:00Z", "", "", ""},
		{"Remind me in 10 minutes to stand up", "reminder", "stand up", "2026-09-27T12:10:00Z", "", "", ""},
		{"Remember to drink water in an hour", "reminder", "drink water", "2026-09-27T13:00:00Z", "", "", ""},
		{"Note: the gate code is 1234 and the meeting is tomorrow at 6pm", "note", "", "", "", "", "the gate code is 1234 and the meeting is tomorrow at 6pm"},
		{"Remember that Mom prefers calls after dinner", "note", "", "", "", "", "Mom prefers calls after dinner"},
		{"Buy milk tomorrow at 6pm; note: get oat milk", "task", "Buy milk", "2026-09-28T12:30:00Z", "", "", "get oat milk"},
		{"Task: finish report by Friday at 5pm; remind me tomorrow at 9am; note: include the sales figures", "task", "finish report", "2026-09-28T03:30:00Z", "2026-10-02T11:30:00Z", "", "include the sales figures"},
		{"Remind me to take medicine every day at 9am", "reminder", "take medicine", "2026-09-28T03:30:00Z", "", "daily", ""},
		{"Remind me to water plants every Monday at 09:00", "reminder", "water plants", "2026-09-28T03:30:00Z", "", "weekly", ""},
		{"Pay rent by 2030-01-01 at 18:00", "task", "Pay rent", "", "2030-01-01T12:30:00Z", "", ""},
		{"Remind me to call José on October 2 at noon; note: ask about 文本", "reminder", "call José", "2026-10-02T06:30:00Z", "", "", "ask about 文本"},
		{"Remind me to check the oven in five seconds", "reminder", "check the oven", "2026-09-27T12:00:05Z", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			r, e := Interpret(c.text, "Asia/Kolkata", now)
			if e != nil || r.Status != "ready" || r.Proposal == nil {
				t.Fatal(r, e)
			}
			i := r.Proposal.Input
			if i.Kind != c.kind || i.Title != c.title || !sameInstant(i.ReminderAt, c.reminder) || !sameInstant(i.DueAt, c.due) || i.Repeat != c.repeat || i.NoteBody != c.note {
				t.Fatal(i, "want", c)
			}
		})
	}
}
func TestAmbiguityNeverProducesCommittableProposal(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, text := range []string{"Call Mom next January", "Call Mom. Email Dad", "Call Mom tomorrow morning at 6pm", "Call Mom tomorrow evening", "Buy milk in half an hour", "Drink water weekdays at 9am", "Call Mom at six", "Call Mom tomorrow at 6", "Remind me to call Mom tomorrow", "Remind me to call Mom", "The door code is 1234", "Remind me to pay rent monthly at 6pm", "Remind me to call Mom tomorrow at 6pm and email Dad", "Remind me to call Mom next week at 6pm", "Remind me to call Mom on 10/11 at 6pm", "Remind me to call Mom tomorrow at 6pm IST", "Don't remind me to call Mom tomorrow at 6pm", "Remind me to call Mom on 2030-02-30 at noon", "Remind me to call Mom every other day at 9am", "Remind me to call Mom tonight at 6pm", "Task: report by tomorrow every day at 6pm"} {
		t.Run(text, func(t *testing.T) {
			r, e := Interpret(text, "UTC", now)
			if e != nil || r.Status != "needs_clarification" || r.Proposal != nil || r.Draft.PreviewID != "" || len(r.Questions) == 0 {
				t.Fatal(r, e)
			}
		})
	}
}
func TestTimezonesDSTAndCalendarIntervals(t *testing.T) {
	for _, text := range []string{"Remind me to call Mom on 2026-03-08 at 2:30am", "Remind me to call Mom on 2026-11-01 at 1:30am"} {
		r, e := Interpret(text, "America/New_York", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		if e != nil || r.Status != "needs_clarification" {
			t.Fatal(r, e)
		}
	}
	// A calendar day across spring DST is 23 elapsed hours; 24 hours remains elapsed time.
	now := time.Date(2026, 3, 7, 17, 0, 0, 0, time.UTC)
	day, e := Interpret("Remind me to call Mom in one day", "America/New_York", now)
	if e != nil || day.Proposal == nil || !sameInstant(day.Draft.ReminderAt, "2026-03-08T16:00:00Z") {
		t.Fatal(day, e)
	}
	elapsed, e := Interpret("Remind me to call Mom in 24 hours", "America/New_York", now)
	if e != nil || elapsed.Proposal == nil || !sameInstant(elapsed.Draft.ReminderAt, "2026-03-08T17:00:00Z") {
		t.Fatal(elapsed, e)
	}
	for _, zone := range []string{"", "Local", "unknown"} {
		if _, e = Interpret("Buy milk", zone, now); e == nil {
			t.Fatal(zone)
		}
	}
	for _, text := range []string{"", " ", string([]byte{0xff})} {
		if _, e = Interpret(text, "UTC", now); e == nil {
			t.Fatal(text)
		}
	}
}

func sameInstant(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	x, e := time.Parse(time.RFC3339Nano, a)
	y, f := time.Parse(time.RFC3339Nano, b)
	return e == nil && f == nil && x.Equal(y)
}
