package routing

import (
	"testing"
	"time"
)

func TestExplicitClockKeepsEightAMInUserTimezone(t *testing.T) {
	state := State{Text: "Remind me to buy bread tomorrow morning at 8am.", Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-02T09:00:00Z"}
	plan := ActionPlan{Action: "reminder", Kind: "reminder", Title: "Buy bread", ReminderAt: "2026-10-03T08:00:00Z"}
	if ValidatePlan(plan, state) == nil {
		t.Fatal("accepted 8 AM UTC as 8 AM in India")
	}
	plan = normalizeExplicitClock(plan, state)
	instant, err := time.Parse(time.RFC3339Nano, plan.ReminderAt)
	if err != nil {
		t.Fatal(err)
	}
	zone, _ := time.LoadLocation(state.Timezone)
	local := instant.In(zone)
	if local.Hour() != 8 || local.Minute() != 0 || local.Day() != 3 {
		t.Fatal(plan.ReminderAt)
	}
	if err := ValidatePlan(plan, state); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitClockKeepsLinkedReminderOffset(t *testing.T) {
	state := State{Text: "Prepare for my interview tomorrow at 8 AM", Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-02T09:00:00Z"}
	plan := ActionPlan{Action: "task", Kind: "task", DueAt: "2026-10-03T08:00:00Z", ReminderAt: "2026-10-03T07:00:00Z"}
	plan = normalizeExplicitClock(plan, state)
	due, _ := time.Parse(time.RFC3339Nano, plan.DueAt)
	reminder, _ := time.Parse(time.RFC3339Nano, plan.ReminderAt)
	zone, _ := time.LoadLocation(state.Timezone)
	if due.In(zone).Hour() != 8 || due.Sub(reminder) != time.Hour {
		t.Fatal(plan)
	}
}

func TestReminderDueSlotNormalizesToRequestedLocalClock(t *testing.T) {
	state := State{Text: "Remind me to call Neha tomorrow morning at 8am.", Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-02T18:16:57Z"}
	plan := NormalizePlanTiming(ActionPlan{Action: "reminder", Kind: "reminder", Title: "Call Neha", DueAt: "2026-10-03T03:30:00Z"}, state)
	if plan.DueAt != "" || plan.ReminderAt != "2026-10-03T08:00:00+05:30" {
		t.Fatal(plan)
	}
	if err := ValidatePlan(plan, state); err != nil {
		t.Fatal(err)
	}
}
