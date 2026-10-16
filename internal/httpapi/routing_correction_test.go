package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type correctionFixture struct{ choice string }

func (f correctionFixture) Evaluate(_ context.Context, _ routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	probabilities := map[string]float64{}
	for _, action := range actions {
		probabilities[action.ID] = 0
	}
	probabilities[f.choice] = 1
	return routing.Evaluation{Decision: routing.Choice{Type: "choice", Choice: f.choice, Confidence: 1, Probabilities: probabilities}, Model: "fixture"}, nil
}

func TestNaturalChoiceAnswerUsesJevForUnscriptedReply(t *testing.T) {
	c := routingConversation{Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "UTC", ReferenceAt: "2026-10-01T14:36:03Z"}}, Question: &channelQuestion{Field: "note", ValueType: "choice", Prompt: "Attach a note?", Choices: []provider.Choice{{Value: "add", Label: "Add"}, {Value: "skip", Label: "Skip"}}}}
	if !c.naturalChoiceAnswer(t.Context(), routingService{jev: correctionFixture{choice: "skip"}}, "I don't need one") || c.Draft.Note != "skip" {
		t.Fatalf("choice not interpreted: %+v", c)
	}
}

func TestNaturalRouteChoiceUsesOriginalRequestAndClarification(t *testing.T) {
	c := routingConversation{Route: routing.Result{Provider: "jev", Input: routing.State{Text: "Call Maya tomorrow", Timezone: "UTC", ReferenceAt: "2026-10-01T14:36:03Z"}}}
	if selected := c.naturalRouteChoice(t.Context(), routingService{jev: correctionFixture{choice: "reminder"}}, "I meant a notification"); selected != "reminders" {
		t.Fatal(selected)
	}
}

func TestNaturalCorrectionRevisesDeadlineAndRemovesLateReminder(t *testing.T) {
	c := routingConversation{Channel: "tasks", Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-01T14:36:03Z"}}}
	c.Draft.Fields.Title = "drop mummy"
	c.Draft.Fields.ReminderAt = "2026-10-02T03:30:00Z" // 9:00 am local
	c.Draft.Reminder = "add"
	if !c.naturalCorrection(t.Context(), routingService{jev: correctionFixture{choice: "deadline"}}, "i need to drop her by 7:45 am") {
		t.Fatal(c.Warnings)
	}
	if c.Draft.Fields.DueAt != "2026-10-02T02:15:00Z" || c.Draft.Fields.ReminderAt != "" || c.Draft.Reminder != "skip" {
		t.Fatalf("incorrect revised draft: %+v", c.Draft)
	}
	if !strings.Contains(c.Warnings[0], "after the new deadline") {
		t.Fatal(c.Warnings)
	}
}

func TestNaturalCorrectionUnderstandsMakeTheTime(t *testing.T) {
	for _, test := range []struct {
		text, want, warning string
	}{
		{"make the time 9am", "2026-10-16T03:30:00Z", "already set for"},
		{"make the time 9 am", "2026-10-16T03:30:00Z", "already set for"},
		{"make it 10am", "2026-10-16T04:30:00Z", ""},
		{"9 am", "2026-10-16T03:30:00Z", "already set for"},
		{"change the time to 10 am", "2026-10-16T04:30:00Z", ""},
	} {
		t.Run(test.text, func(t *testing.T) {
			c := routingConversation{State: "awaiting_confirmation", Channel: "reminders", Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-15T00:00:00Z"}}}
			c.Draft.Fields.Title = "text Dhriti"
			c.Draft.Fields.ReminderAt = "2026-10-16T03:30:00Z"
			if !c.naturalCorrection(t.Context(), routingService{jev: correctionFixture{choice: "reminder"}}, test.text) {
				t.Fatal(c.Warnings)
			}
			if c.Draft.Fields.ReminderAt != test.want || test.warning != "" && (len(c.Warnings) == 0 || !strings.Contains(c.Warnings[0], test.warning)) || test.warning == "" && len(c.Warnings) != 0 {
				t.Fatalf("unexpected time or warning: %+v", c)
			}
		})
	}
}

func TestNaturalCorrectionClarifiesMultipleClockTimes(t *testing.T) {
	c := routingConversation{Channel: "reminders", Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-15T00:00:00Z"}}}
	c.Draft.Fields.ReminderAt = "2026-10-16T03:30:00Z"
	if c.naturalCorrection(t.Context(), routingService{jev: correctionFixture{choice: "reminder"}}, "time 9am or 10am") {
		t.Fatal("ambiguous clock was applied")
	}
	if c.Draft.Fields.ReminderAt != "2026-10-16T03:30:00Z" || len(c.Warnings) == 0 {
		t.Fatal(c.Draft, c.Warnings)
	}
}

func TestReminderConfirmationUsesLocalTime(t *testing.T) {
	c := routingConversation{State: "awaiting_confirmation", Route: routing.Result{Input: routing.State{Timezone: "Asia/Kolkata"}}, Proposal: &store.CaptureProposal{Input: store.CaptureInput{Kind: "reminder", Title: "text Dhriti", ReminderAt: "2026-10-16T03:30:00Z"}}}
	prompt := c.prompt()
	if !strings.Contains(prompt, "Fri, 16 Oct 2026 at 9:00 AM IST") || strings.Contains(prompt, "03:30:00Z") {
		t.Fatal(prompt)
	}
}

func TestReminderTimeReplyKeepsConversationReviewable(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	zone, _ := time.LoadLocation("Asia/Kolkata")
	day := time.Now().In(zone).AddDate(0, 0, 2)
	at := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, zone).UTC().Format(time.RFC3339)
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "reminder", Kind: "reminder", Title: "text Dhriti", ReminderAt: at}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: correctionFixture{choice: "reminder"}, free: planner, configured: true, secret: "test"})
	send := func(path string, body any) (int, map[string]any) {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "free", "version": "1", "request_id": "reminder-time-reply", "text": "Remind me to text Dhriti", "timezone": "Asia/Kolkata"})
	if code != 201 || c["state"] != "awaiting_confirmation" {
		t.Fatal(code, c)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "make the time 9 am"})
	if code != 200 || c["state"] != "awaiting_confirmation" || !strings.Contains(c["prompt"].(string), "9:00 AM IST") || !strings.Contains(c["prompt"].(string), "already set for") || strings.Contains(c["prompt"].(string), "03:30:00Z") {
		t.Fatal(code, c)
	}
	reminders, err := s.Reminders(t.Context())
	if err != nil || len(reminders) != 0 {
		t.Fatal("reply saved without confirmation", reminders, err)
	}
}

func TestNaturalCorrectionKeepsDraftOnUnclearDecision(t *testing.T) {
	c := routingConversation{Channel: "tasks", Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-01T14:36:03Z"}}}
	c.Draft.Fields.ReminderAt = "2026-10-02T03:30:00Z"
	if c.naturalCorrection(t.Context(), routingService{jev: correctionFixture{choice: "clarify"}}, "something else") {
		t.Fatal("unclear correction was applied")
	}
	if c.Draft.Fields.DueAt != "" || c.Draft.Fields.ReminderAt != "2026-10-02T03:30:00Z" || len(c.Warnings) == 0 {
		t.Fatalf("draft changed or feedback missing: %+v", c)
	}
}

func TestNaturalCorrectionHandlesOtherPendingDraftDecisions(t *testing.T) {
	for _, test := range []struct {
		name, channel, choice, text, wantState, wantTitle, wantDetails, wantNote string
	}{
		{"save", "tasks", "save", "go ahead", "confirming", "drop mummy", "", ""},
		{"cancel", "tasks", "cancel", "forget this", "cancelled", "drop mummy", "", ""},
		{"title", "tasks", "title", "rename it to Drop Mum at school", "awaiting_confirmation", "Drop Mum at school", "", ""},
		{"details", "tasks", "details", "add details that use the west gate", "awaiting_confirmation", "drop mummy", "use the west gate", ""},
		{"note", "notes", "note_body", "change the note to Bring the blue folder", "awaiting_confirmation", "drop mummy", "", "Bring the blue folder"},
		{"skip reminder", "tasks", "skip_reminder", "no reminder please", "awaiting_confirmation", "drop mummy", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := routingConversation{State: "awaiting_confirmation", Channel: test.channel, Route: routing.Result{Provider: "jev", Input: routing.State{Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-01T14:36:03Z"}}}
			c.Draft.Fields.Title = "drop mummy"
			c.Draft.Fields.ReminderAt = "2026-10-02T03:30:00Z"
			c.Draft.Reminder = "add"
			if !c.naturalCorrection(t.Context(), routingService{jev: correctionFixture{choice: test.choice}}, test.text) {
				t.Fatal(c.Warnings)
			}
			if c.State != test.wantState || c.Draft.Fields.Title != test.wantTitle || c.Draft.Fields.Details != test.wantDetails || c.Draft.Fields.NoteBody != test.wantNote {
				t.Fatalf("unexpected correction: %+v", c)
			}
			if test.choice == "skip_reminder" && (c.Draft.Reminder != "skip" || c.Draft.Fields.ReminderAt != "") {
				t.Fatalf("reminder still pending: %+v", c.Draft)
			}
		})
	}
}
