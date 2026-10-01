package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"context"
	"strings"
	"testing"
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
