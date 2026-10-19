package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This corpus is the routing safety gate for the existing record types. It
// checks action conservation before any model or store mutation is involved.
func TestOrchestrationActionConservationCorpus(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		steps int
	}{
		{"task and reminder capture", "Create a task for my interview, remind me an hour before, and note that I should bring my portfolio", 0},
		{"natural task and attachments", "I need to prepare for my interview by tomorrow at 6pm, note that bring my portfolio and remind me tomorrow at 5pm", 0},
		{"explicit note body", "Make a note of this: buy milk and create a list", 0},
		{"single deletion", "Delete all tasks and reminders", 0},
		{"task then note", "Create a task to buy milk; create a note about the recipe", 2},
		{"read then delete", "Find my interview. Delete the old reminder", 2},
		{"punctuated delete then create", "Delete all tasks. Add a reminder", 2},
		{"comma separated delete then create", "Delete all tasks, add a reminder", 2},
		{"three actions", "Find my interview; edit the task; delete the old note", 3},
		{"unrecognized clause remains unsplit", "Delete all tasks; maybe something else", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(workflowClauses(tc.text)); got != tc.steps {
				t.Fatalf("%q: got %d steps, want %d", tc.text, got, tc.steps)
			}
		})
	}
}

// Run deliberately with ATLAS_LIVE_ORCHESTRATION=1 and a configured key.
// Six one-shot Jev decisions probe the user-facing action boundaries without
// writing records or invoking a free extraction model.
func TestLiveJevOrchestrationChoices(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_ORCHESTRATION") != "1" {
		t.Skip("set ATLAS_LIVE_ORCHESTRATION=1 for six live Jev decisions")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	contextRecords := []provider.ContextRecord{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "task", Title: "Interview with Maya", Status: "open", DueAt: "2026-10-20T09:00:00+05:30"}, {ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "reminder", Title: "Submit portfolio", Status: "scheduled", DueAt: "2026-10-19T09:00:00+05:30"}}
	cases := []struct{ text, want string }{
		{"Create a task to buy milk tomorrow", "task"},
		{"When is my interview with Maya?", "lookup"},
		{"Move my interview with Maya to Friday at 7 pm", "edit"},
		{"Change the Submit portfolio reminder to repeat weekly", "edit"},
		{"Delete my Submit portfolio reminder", "delete"},
		{"Send an email to Maya", "unsupported"},
	}
	jev := routing.Jev{APIKey: key}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			result, err := jev.Evaluate(ctx, routing.State{Text: tc.text, Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-16T12:00:00Z", Context: contextRecords}, routing.Registry(), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("choice=%s confidence=%.3f input_tokens=%d output_tokens=%d", result.Decision.Choice, result.Decision.Confidence, result.Usage.InputTokens, result.Usage.OutputTokens)
			if result.Decision.Choice != tc.want {
				t.Fatalf("%q: want %s, got %s", tc.text, tc.want, result.Decision.Choice)
			}
		})
	}
}

// This small read-only conversation checks that an ordered request invokes
// Jev once per started step and retains the planned second action.
func TestLiveJevOrderedWorkflow(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_ORCHESTRATION") != "1" {
		t.Skip("opt-in live Jev check")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Create(t.Context(), "Interview preparation"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateReminder(t.Context(), "Submit portfolio", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: routing.Jev{APIKey: key}, configured: true, secret: "live-workflow"})
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "live-ordered-read", "text": "Find my tasks; find my reminders", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_workflow_start" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	for _, reply := range []string{"start", "continue"} {
		code, c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": int(c["version"].(float64)), "text": reply})
		if code != 200 || c["state"] != "answered" {
			t.Fatalf("%s: %d %v", reply, code, c)
		}
		if c["channel"] != "lookup" {
			t.Fatal(c["channel"])
		}
	}
	if workflow, ok := c["workflow"].(map[string]any); !ok || int(workflow["index"].(float64)) != 1 {
		t.Fatal(c["workflow"])
	}
}

// One Jev decision checks an end-to-end conversation crossing a local
// relation read and a model-routed lookup. Bulk edit and occurrence actions
// below use the same provider session boundary without extra model calls.
func TestLiveJevConversationGaps(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_ORCHESTRATION") != "1" {
		t.Skip("opt-in live Jev check")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Live relation alpha")
	b, _ := s.Create(t.Context(), "Live relation beta")
	if _, err = s.SetRelation(t.Context(), "task", a.ID, "task", b.ID, true); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	r, _, err := s.CreateRepeatingReminderRequest(t.Context(), "live-gap-repeat", "Live repeat", at.Format(time.RFC3339), "UTC", "", "daily")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessDue(t.Context(), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: routing.Jev{APIKey: key}, configured: true, secret: "live-gaps"})
	send := batchTestSend(t, mux)
	reply := func(c map[string]any, value string) map[string]any {
		t.Helper()
		code, next := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": value})
		if code != 200 {
			t.Fatal(code, next)
		}
		return next
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "live-gaps-workflow", "text": "List relations; find my tasks", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_workflow_start" {
		t.Fatal(code, c)
	}
	c = reply(c, "start")
	if c["state"] != "answered" || c["channel"] != "relation_list" {
		t.Fatal(c)
	}
	c = reply(c, "continue")
	if c["state"] != "answered" || c["channel"] != "lookup" {
		t.Fatal(c)
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "live-gaps-edit", "text": "Complete all tasks", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_batch_edit_confirmation" {
		t.Fatal(code, c)
	}
	c = reply(c, "yes")
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "live-gaps-occurrence", "text": "Dismiss occurrence of Live repeat", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_occurrence_confirmation" {
		t.Fatal(code, c)
	}
	c = reply(c, "yes")
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	state, err := s.ReminderState(t.Context(), r.ID)
	if err != nil || state.Reminder.Status != "scheduled" || len(state.Deliveries) != 2 {
		t.Fatal(state, err)
	}
}
