package httpapi

import (
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

// This fixture only selects an action. Field extraction, persistence, linked
// effects, and lookup all run through the real conversation and store code.
type conversationRouteFixture struct{}

func (conversationRouteFixture) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	choice := "task"
	lower := strings.ToLower(state.Text)
	switch {
	case strings.HasPrefix(lower, "reschedule"), strings.HasPrefix(lower, "complete"), strings.HasPrefix(lower, "dismiss"):
		choice = "edit"
	case strings.HasPrefix(lower, "when"), strings.HasPrefix(lower, "what"):
		choice = "lookup"
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, choice)
}

type narrowDecisionFixture struct{ calls int }

func (f *narrowDecisionFixture) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	f.calls++
	if len(actions) == len(routing.Registry()) {
		e, err := (routing.Mock{}).Evaluate(ctx, state, actions, "edit")
		for id := range e.Decision.Probabilities {
			e.Decision.Probabilities[id] = 0
		}
		e.Decision.Probabilities["edit"] = 0.58
		e.Decision.Probabilities["task"] = 0.31
		e.Decision.Probabilities["clarify"] = 0.05
		e.Decision.Probabilities["unsupported"] = 0.06
		return e, err
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, "edit")
}

func TestAmbiguousBroadJevDecisionGetsOneFocusedDecision(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fixture := &narrowDecisionFixture{}
	service := routingService{jev: fixture, configured: true, secret: "test"}
	result, err := evaluateRouting(t.Context(), s, service, "jev", routing.Request{Version: "1", RequestID: "focused-edit", Text: "Reschedule my interview to 4 October at 7pm", Timezone: "Asia/Kolkata"})
	if err != nil || fixture.calls != 2 || result.State != "routed" || result.SelectedChannel != "edit" {
		t.Fatal(result, fixture.calls, err)
	}
}

func TestConversationContinuesAfterSavingAndUsesLinkedRecords(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: conversationRouteFixture{}, configured: true, secret: "test"})
	send := func(path string, body any) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code >= 300 {
			t.Fatalf("POST %s: %d %s", path, w.Code, w.Body.String())
		}
		return out
	}
	due := time.Now().UTC().AddDate(0, 0, 1).Truncate(24 * time.Hour).Add(18 * time.Hour)
	first := "I need to prepare for my interview by tomorrow at 6pm, note that bring my portfolio and remind me tomorrow at 5pm"
	c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "end-to-end", "text": first, "timezone": "UTC"})
	if c["state"] != "awaiting_confirmation" {
		t.Fatalf("create proposal: %#v", c)
	}
	id := c["id"].(string)
	reply := func(text string) {
		t.Helper()
		c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": text})
	}
	reply("yes")
	if c["state"] != "saved" || c["next_request"].(map[string]any)["terminal"] != false {
		t.Fatalf("saved session cannot continue: %#v", c)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal("task not saved", tasks)
	}
	state, err := s.TaskState(t.Context(), tasks[0].ID)
	if err != nil || len(state.Reminders) != 1 || len(state.Notes) != 1 {
		t.Fatal("linked records missing", state, c["proposal"], err)
	}
	reply("When is my interview?")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "interview") {
		t.Fatalf("lookup after save: %#v", c)
	}
	newDue := due.Add(24 * time.Hour)
	reply("Reschedule my interview to " + newDue.Format(time.RFC3339))
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "from") {
		t.Fatalf("edit after lookup: %#v", c)
	}
	state, err = s.TaskState(t.Context(), tasks[0].ID)
	if err != nil || state.Task.DueAt != newDue.Format("2006-01-02T15:04:05.000000000Z") || len(state.Reminders) != 1 || len(state.Notes) != 1 {
		t.Fatal("linked edit did not land", state, err)
	}
	reply("Complete my interview reminder")
	if c["state"] != "answered" {
		t.Fatalf("complete reminder: %#v", c)
	}
	reply("Complete my interview task")
	if c["state"] != "answered" {
		t.Fatalf("complete task: %#v", c)
	}
	state, err = s.TaskState(t.Context(), tasks[0].ID)
	if err != nil || state.Task.Status != "completed" || state.Reminders[0].Status != "completed" {
		t.Fatal("completion did not persist", state, err)
	}
	reply("What happened with my interview?")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "interview") {
		t.Fatalf("lookup after completion: %#v", c)
	}
}
