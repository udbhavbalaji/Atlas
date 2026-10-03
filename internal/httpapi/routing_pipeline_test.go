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

type stagedJev struct {
	category, tool string
	calls          int
	firstContext   []provider.ContextRecord
}

func (e *stagedJev) Evaluate(_ context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	e.calls++
	choice := e.category
	if actions[0].ID == "create" {
		e.firstContext = append([]provider.ContextRecord(nil), state.Context...)
	} else {
		choice = e.tool
	}
	probabilities := map[string]float64{}
	for _, action := range actions {
		probabilities[action.ID] = .1 / float64(len(actions)-1)
	}
	probabilities[choice] = .9
	return routing.Evaluation{Decision: routing.Choice{Type: "choice", Choice: choice, Confidence: .9, Probabilities: probabilities}, Model: "jev-fixture", ModelCalls: 1}, nil
}

type canonicalResponder struct{ calls int }

func (r *canonicalResponder) Respond(_ context.Context, facts map[string]any) (string, string, error) {
	r.calls++
	return facts["canonical_answer"].(string), "response-fixture", nil
}

type unavailablePlanner struct{}

func (unavailablePlanner) Plan(context.Context, routing.State) (routing.PlanResult, error) {
	return routing.PlanResult{}, routing.ErrUnavailable
}

type clarifyingNoteToolJev struct{}

func (clarifyingNoteToolJev) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	choice := "read"
	if actions[0].ID == "lookup_task" {
		choice = "lookup_note"
		if state.Text == "What does it say?" {
			choice = "clarify"
		}
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, choice)
}

func TestJevLookupUsesSavedRecordsWhenFreeModelsUnavailable(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: &stagedJev{category: "read", tool: "lookup_task"}, free: unavailablePlanner{}, configured: true, secret: "test"})
	data, _ := json.Marshal(map[string]any{"provider": "jev", "version": "1", "request_id": "lookup-fallback", "text": "When is my interview?", "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/routing", bytes.NewReader(data)))
	var out map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || out["state"] != "answered" || !strings.Contains(out["prompt"].(string), "Interview with Maya") || out["route"].(map[string]any)["extraction_model"] != "local_lookup" {
		t.Fatal(w.Code, out)
	}
}

func TestJevNoteListAgainAndReadThatNote(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, _, err := s.CreateNoteRequest(t.Context(), "note-followup", "- Bread\n- Cereal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RenameNote(t.Context(), n.NoteID, "grocery list"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: clarifyingNoteToolJev{}, free: &fixedPlanner{plan: routing.ActionPlan{Action: "lookup", Kind: "note"}}, configured: true, secret: "test"})
	send := func(path string, body any) map[string]any {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		if w.Code >= 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "note-list-again", "text": "show my notes again", "timezone": "UTC"})
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "grocery list") {
		t.Fatal(c)
	}
	path := "/api/v1/conversations/routing/" + c["id"].(string) + "/reply"
	c = send(path, map[string]any{"version": c["version"], "text": "What does it say?"})
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Bread - Cereal") || c["route"].(map[string]any)["selected_tool"] != "lookup_note" || c["route"].(map[string]any)["input"].(map[string]any)["recent_record_id"] != n.NoteID {
		t.Fatal(c)
	}
}

func TestRecentLookupDoesNotOverrideUnrelatedRequest(t *testing.T) {
	state := routing.State{Text: "Should I delete it?", RecentRecordID: "note-1", RecentRecordKind: "note", Context: []provider.ContextRecord{{ID: "note-1", Kind: "note"}}}
	if groundedRecentLookupKind(state) != "" {
		t.Fatal("write request became a lookup")
	}
}

func TestJevPipelineCompleteLinkedConversation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	due := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	newDue := due.Add(24 * time.Hour)
	reminderAt := due.Add(-time.Hour)
	je := &stagedJev{category: "create", tool: "task"}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "task", Kind: "task", Title: "Interview with Maya", DueAt: due.Format(time.RFC3339), ReminderAt: reminderAt.Format(time.RFC3339), NoteBody: "Bring portfolio"}}
	responder := &canonicalResponder{}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: je, free: planner, responder: responder, configured: true, secret: "test"})
	send := func(path string, body any) map[string]any {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		if w.Code != 200 && w.Code != 201 {
			t.Fatal(w.Code, out)
		}
		return out
	}
	c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "pipeline", "text": "Create a task for my interview with Maya, remind me an hour before, and note that I should bring my portfolio", "timezone": "UTC"})
	if c["state"] != "saved" || c["saved"] == nil {
		t.Fatal(c)
	}
	if je.calls != 2 || planner.calls != 1 || responder.calls == 0 {
		t.Fatal(je.calls, planner.calls, responder.calls)
	}
	tasks, err := s.Tasks(t.Context())
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	state, err := s.TaskState(t.Context(), tasks[0].ID)
	if err != nil || len(state.Reminders) != 1 || len(state.Notes) != 1 {
		t.Fatal(state, err)
	}
	id := c["id"].(string)
	reply := func(text string) {
		c = send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": text})
	}
	je.category, je.tool = "change", "edit_task"
	planner.plan = routing.ActionPlan{Action: "edit", Kind: "task", TargetID: tasks[0].ID, Field: "due_at", Value: newDue.Format(time.RFC3339)}
	reply("Move my interview with Maya to the following day")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "from") {
		t.Fatal(c)
	}
	state, err = s.TaskState(t.Context(), tasks[0].ID)
	parsedDue, parseErr := time.Parse(time.RFC3339Nano, state.Task.DueAt)
	if err != nil || parseErr != nil || !parsedDue.Equal(newDue) || len(state.Reminders) != 1 {
		t.Fatal(state, err)
	}
	parsedReminder, parseErr := time.Parse(time.RFC3339Nano, state.Reminders[0].ScheduledAt)
	if parseErr != nil || !parsedReminder.Equal(reminderAt.Add(24*time.Hour)) {
		t.Fatal(state, parseErr)
	}
	je.category, je.tool = "read", "lookup_task"
	planner.plan = routing.ActionPlan{Action: "lookup", Kind: "task", TargetID: tasks[0].ID}
	reply("When is my interview, and what did I note about it?")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "portfolio") {
		t.Fatal(c)
	}
	matched := false
	for _, record := range je.firstContext {
		if record.ID == tasks[0].ID {
			matched = true
		}
	}
	if !matched {
		t.Fatal("Jev did not see the matching task before routing", je.firstContext)
	}
	je.category, je.tool = "change", "edit_reminder"
	planner.plan = routing.ActionPlan{Action: "edit", Kind: "reminder", TargetID: state.Reminders[0].ID, Field: "status", Value: "dismissed"}
	reply("Dismiss the interview reminder")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "dismissed") {
		t.Fatal(c)
	}
	je.category, je.tool = "change", "edit_task"
	planner.plan = routing.ActionPlan{Action: "edit", Kind: "task", TargetID: tasks[0].ID, Field: "status", Value: "completed"}
	reply("Mark the interview task complete")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "completed") {
		t.Fatal(c)
	}
	je.category, je.tool = "read", "lookup_task"
	planner.plan = routing.ActionPlan{Action: "lookup", Kind: "task", TargetID: tasks[0].ID}
	reply("What happened with my interview task?")
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "completed") {
		t.Fatal(c)
	}
}

func TestContextKeywordsUseRecentConversation(t *testing.T) {
	records := []provider.ContextRecord{{ID: "friend", Kind: "task", Title: "Drop friend at station"}, {ID: "interview", Kind: "task", Title: "Interview with Maya"}}
	words := contextKeywords("When is it?", "User: I need to drop my friend at the station in 20 minutes", records)
	found := false
	for _, word := range words {
		if word.word == "friend" {
			found = true
		}
	}
	if !found {
		t.Fatal(words)
	}
}

func TestShortFollowupUsesRecentSavedEntity(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	due := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	_, err = s.CreateWithFields(t.Context(), "Drop my friend at the station", "", due)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateWithFields(t.Context(), "Interview with Maya", "", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	recent := "User: I need to drop my friend at the station in 20 minutes"
	context, _, err := pipelineContext(t.Context(), s, "When is it?", recent, "lookup_task", 20)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := answerLookup(t.Context(), s, routing.State{Text: "When is it?", RecentConversation: recent, Context: context, Timezone: "UTC", ReferenceAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil || !strings.Contains(answer.Text, "friend") || strings.Contains(answer.Text, "Interview") {
		t.Fatal(answer, err)
	}
}

func TestLookupResponseMustPreserveSourceAndTime(t *testing.T) {
	c := routingConversation{State: "answered", Answer: &conversationAnswer{Text: "Task “Interview with Maya” is open; due Fri, 2 Oct 2026 at 3:00 PM IST.", Sources: []conversationSource{{Type: "task", Title: "Interview with Maya"}}}}
	if c.responsePreservesFacts("The interview is tomorrow.") {
		t.Fatal("accepted a reply without grounded facts")
	}
	if !c.responsePreservesFacts("Interview with Maya is due Fri, 2 Oct 2026 at 3:00 PM IST.") {
		t.Fatal("rejected a grounded reply")
	}
}

func TestTimedTaskKeepsDeadlineWithLinkedReminder(t *testing.T) {
	plan := routing.ActionPlan{Action: "task", ReminderAt: "2026-10-02T09:00:00Z"}
	normalizeTimedTaskPlan("task", "Drop Arjun at the station in 20 minutes", &plan)
	if plan.DueAt != plan.ReminderAt {
		t.Fatal(plan)
	}
	plan = routing.ActionPlan{Action: "task", ReminderAt: "2026-10-02T09:00:00Z"}
	normalizeTimedTaskPlan("task", "Remind me before my interview", &plan)
	if plan.DueAt != "" {
		t.Fatal(plan)
	}
}

func TestJevReminderIgnoresTaskOnlyExtractionFields(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	when := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: &stagedJev{category: "create", tool: "reminder"}, free: &fixedPlanner{plan: routing.ActionPlan{Action: "reminder", Kind: "reminder", Title: "Buy bread", Details: "Tomorrow at 8 AM", DueAt: when, ReminderAt: when}}, configured: true, secret: "test"})
	data, _ := json.Marshal(map[string]any{"provider": "jev", "version": "1", "request_id": "bread-reminder", "text": "Remind me to buy bread tomorrow at 8 AM", "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/routing", bytes.NewReader(data)))
	var out map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || out["state"] != "saved" || strings.Contains(out["prompt"].(string), "linked reminder") {
		t.Fatal(w.Code, out)
	}
	reminders, err := s.Reminders(t.Context())
	if err != nil || len(reminders) != 1 || reminders[0].Title != "Buy bread" {
		t.Fatal(reminders, err)
	}
	tasks, err := s.Tasks(t.Context())
	if err != nil || len(tasks) != 0 {
		t.Fatal(tasks, err)
	}
}

func TestJevReminderUsesRequestedEightAMWhenModelMisplacesOrOmitsTime(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().In(zone).AddDate(0, 0, 1)
	want := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 8, 0, 0, 0, zone)
	wrong := want.Add(time.Hour).UTC().Format(time.RFC3339)
	for _, test := range []struct{ name, dueAt string }{{"wrong_slot_and_clock", wrong}, {"missing_model_time", ""}} {
		t.Run(test.name, func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			planner := &fixedPlanner{plan: routing.ActionPlan{Action: "reminder", Kind: "reminder", Title: "Call Neha", DueAt: test.dueAt}}
			mux := http.NewServeMux()
			routingRoutesWithService(mux, s, routingService{jev: &stagedJev{category: "create", tool: "reminder"}, free: planner, configured: true, secret: "test"})
			data, _ := json.Marshal(map[string]any{"provider": "jev", "version": "1", "request_id": "neha-" + test.name, "text": "Remind me to call Neha tomorrow morning at 8am.", "timezone": "Asia/Kolkata"})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/conversations/routing", bytes.NewReader(data)))
			var out map[string]any
			json.Unmarshal(w.Body.Bytes(), &out)
			if w.Code != 201 || out["state"] != "saved" {
				t.Fatal(w.Code, out)
			}
			reminders, err := s.Reminders(t.Context())
			if err != nil || len(reminders) != 1 {
				t.Fatal(reminders, err)
			}
			got, err := time.Parse(time.RFC3339Nano, reminders[0].ScheduledAt)
			if err != nil || !got.Equal(want) {
				t.Fatal(reminders[0].ScheduledAt, want, err)
			}
		})
	}
}

func TestJevReminderUsesVisibleLocalFallbackWhenFreeExtractionFails(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: &stagedJev{category: "create", tool: "reminder"}, free: unavailablePlanner{}, configured: true, secret: "test"})
	data, _ := json.Marshal(map[string]any{"provider": "jev", "version": "1", "request_id": "neha-local-fallback", "text": "Remind me to call Neha tomorrow morning at 8am.", "timezone": "Asia/Kolkata"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/conversations/routing", bytes.NewReader(data)))
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 201 || out["state"] != "saved" || out["route"].(map[string]any)["extraction_model"] != "local_parser" || len(out["warnings"].([]any)) == 0 {
		t.Fatal(w.Code, out)
	}
	reminders, err := s.Reminders(t.Context())
	if err != nil || len(reminders) != 1 {
		t.Fatal(reminders, err)
	}
	zone, _ := time.LoadLocation("Asia/Kolkata")
	instant, _ := time.Parse(time.RFC3339Nano, reminders[0].ScheduledAt)
	if instant.In(zone).Hour() != 8 || instant.In(zone).Minute() != 0 {
		t.Fatal(reminders[0])
	}
}

func TestLocalReminderFallbackDoesNotInventTime(t *testing.T) {
	for _, sentence := range []string{"Remind me to call Neha", "Remind me to call Neha tomorrow morning"} {
		state := routing.State{Text: sentence, Timezone: "Asia/Kolkata", ReferenceAt: "2026-10-02T09:00:00Z", SelectedTool: "reminder"}
		if plan, ok := localReminderPlan(state); ok {
			t.Fatal(sentence, plan)
		}
	}
}
