package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type lookupDecisionFixture struct{ calls int }

func (f *lookupDecisionFixture) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	f.calls++
	choice := "lookup"
	if strings.HasPrefix(state.Text, "Add task") {
		choice = "task"
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, choice)
}

func TestJevConversationLooksUpSavedRecordsAndContinues(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	interview, err := s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", "2026-10-02T04:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateLinkedReminder(t.Context(), "Prepare for interview", "2026-10-01T04:30:00Z", "Asia/Kolkata", interview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateReminder(t.Context(), "Old dentist visit", "2025-01-01T04:30:00Z", "Asia/Kolkata"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CreateNoteRequest(t.Context(), "", "Interviewer Maya prefers a portfolio walkthrough.", "", ""); err != nil {
		t.Fatal(err)
	}
	fixture := &lookupDecisionFixture{}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: fixture, configured: true, secret: "test-secret"})
	send := func(method, path string, body any) (int, map[string]any) {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(payload)))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err, w.Body.String())
		}
		return w.Code, result
	}
	code, current := send("POST", "/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "lookup-1", "text": "When is my interview?", "timezone": "Asia/Kolkata"})
	if code != 201 || current["state"] != "answered" || fixture.calls != 1 || !strings.Contains(current["prompt"].(string), "Fri, 2 Oct 2026 at 10:00 AM IST") {
		t.Fatal(code, current, fixture.calls)
	}
	if !strings.Contains(current["prompt"].(string), "Linked reminder") || len(current["answer"].(map[string]any)["sources"].([]any)) == 0 {
		t.Fatal("answer lacked linked reminder or source", current)
	}
	id := current["id"].(string)
	reply := func(text string) {
		code, next := send("POST", "/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": current["version"], "text": text})
		if code != 200 {
			t.Fatal(code, next)
		}
		current = next
	}
	reply("Show my old reminders")
	if current["state"] != "answered" || !strings.Contains(current["prompt"].(string), "Old dentist visit") || fixture.calls != 2 {
		t.Fatal(current, fixture.calls)
	}
	reply("What did I note about interviewer Maya?")
	if current["state"] != "answered" || !strings.Contains(current["prompt"].(string), "portfolio walkthrough") || fixture.calls != 3 {
		t.Fatal(current, fixture.calls)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal("lookup changed records", tasks)
	}
	reply("Add task Call the recruiter")
	if current["state"] != "awaiting_confirmation" || fixture.calls != 4 {
		t.Fatal("Jev follow-up did not enter capture flow", current)
	}
}

func TestLookupAnswersMissingAndGenericHistoryWithoutGuessing(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state := routing.State{Text: "When is my interview?", Timezone: "UTC", ReferenceAt: "2026-09-29T00:00:00Z"}
	answer, err := answerLookup(t.Context(), s, state)
	if err != nil || !strings.Contains(answer.Text, "couldn't find") || len(answer.Sources) != 0 {
		t.Fatal(answer, err)
	}
	state.Text = "Show my old reminders"
	answer, err = answerLookup(t.Context(), s, state)
	if err != nil || !strings.Contains(answer.Text, "couldn't find") {
		t.Fatal(answer, err)
	}
	for i := 0; i < 10; i++ {
		if _, err := s.CreateReminder(t.Context(), fmt.Sprintf("Old reminder %d", i), "2025-01-01T04:30:00Z", "UTC"); err != nil {
			t.Fatal(err)
		}
	}
	answer, err = answerLookup(t.Context(), s, state)
	if err != nil || len(answer.Sources) != 10 {
		t.Fatal("old reminder list was incomplete", answer, err)
	}
}

func TestUpcomingReminderListIgnoresPoliteWordsAndPlannedTarget(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC().Truncate(time.Second)
	soon, err := s.CreateReminder(t.Context(), "Buy bread", now.Add(24*time.Hour).Format(time.RFC3339), "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateReminder(t.Context(), "Call Maya", now.Add(48*time.Hour).Format(time.RFC3339), "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateReminder(t.Context(), "Old appointment", now.Add(-24*time.Hour).Format(time.RFC3339), "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	dismissed, err := s.CreateReminder(t.Context(), "Dismissed appointment", now.Add(36*time.Hour).Format(time.RFC3339), "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReminderMutation(t.Context(), dismissed.ID, "dismissed", ""); err != nil {
		t.Fatal(err)
	}
	state := routing.State{Text: "Can you list all my upcoming reminders please?", Timezone: "Asia/Kolkata", ReferenceAt: now.Format(time.RFC3339), Context: []provider.ContextRecord{{ID: soon.ID, Kind: "reminder", Title: soon.Title}}}
	for _, lookup := range []func(context.Context, *store.Store, routing.State) (conversationAnswer, error){answerLookup, func(ctx context.Context, s *store.Store, state routing.State) (conversationAnswer, error) {
		return answerPlannedLookup(ctx, s, state, routing.ActionPlan{Action: "lookup", Kind: "reminder", TargetID: soon.ID})
	}} {
		answer, err := lookup(t.Context(), s, state)
		if err != nil || len(answer.Sources) != 2 || !strings.Contains(answer.Text, "Buy bread") || !strings.Contains(answer.Text, "Call Maya") || strings.Contains(answer.Text, "Old appointment") || strings.Contains(answer.Text, "Dismissed appointment") || strings.Index(answer.Text, "Buy bread") > strings.Index(answer.Text, "Call Maya") {
			t.Fatal(answer, err)
		}
	}
}

func TestRoutingLookupDispatchReturnsReadOnlyAnswer(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", "2026-10-02T04:30:00Z"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test-secret"})
	send := func(path string, body any) (int, map[string]any) {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(payload)))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err, w.Body.String())
		}
		return w.Code, result
	}
	code, route := send("/api/v1/routing/mock/evaluate", map[string]any{"version": "1", "request_id": "lookup-dispatch", "text": "When is my interview?", "timezone": "Asia/Kolkata", "fixture": "lookup"})
	if code != 200 || route["selected_channel"] != "lookup" {
		t.Fatal(code, route)
	}
	code, result := send("/api/v1/routing/dispatch", map[string]any{"version": "1", "routing_token": route["routing_token"]})
	if code != 200 || result["state"] != "answered" || !strings.Contains(result["answer"].(map[string]any)["text"].(string), "Fri, 2 Oct 2026 at 10:00 AM IST") {
		t.Fatal(code, result)
	}
	if result["persisted"] != false {
		t.Fatal("lookup changed records", result)
	}
}
