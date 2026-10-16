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

type contextGateJev struct {
	tool  string
	tools map[string]string
}

func (g contextGateJev) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	choice := g.tool
	if g.tools[state.Text] != "" {
		choice = g.tools[state.Text]
	}
	if actions[0].ID == "create" {
		switch {
		case strings.HasPrefix(choice, "lookup_"):
			choice = "read"
		case strings.HasPrefix(choice, "edit_"):
			choice = "change"
		case strings.HasPrefix(choice, "delete_"):
			choice = "remove"
		}
	}
	return (routing.Mock{}).Evaluate(ctx, state, actions, choice)
}

type specificityGatePlanner struct {
	plans          map[string]routing.ActionPlan
	expectedRecent map[string]string
	t              *testing.T
}

func (p specificityGatePlanner) Plan(_ context.Context, state routing.State) (routing.PlanResult, error) {
	p.t.Helper()
	if want := p.expectedRecent[state.Text]; want != "" && state.RecentRecordID != want {
		p.t.Fatalf("%q received recent record %q; want %q", state.Text, state.RecentRecordID, want)
	}
	plan, ok := p.plans[state.Text]
	if !ok {
		p.t.Fatalf("no specificity plan for %q", state.Text)
	}
	return routing.PlanResult{Plan: plan, Model: "specificity-gate"}, nil
}

// This is the offline orchestration gate for conversational context. The
// sentences deliberately mix lookup, edit, delete, task, reminder, and note
// workflows. No Jev call is made: this gate verifies that the production
// context gatherer gives the router and planner the intended record before
// either model is asked to decide.
func TestConversationContextGate50MixedWorkflowUtterances(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	interview, err := s.CreateWithFields(ctx, "Interview with Maya", "Prepare the case study", time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	interviewReminder, err := s.CreateLinkedReminder(ctx, "Interview alarm", time.Now().Add(71*time.Hour).UTC().Format(time.RFC3339), "UTC", interview.ID)
	if err != nil {
		t.Fatal(err)
	}
	interviewNote, _, err := s.CreateNoteRequest(ctx, "interview-note", "Portfolio is in the navy folder", interview.ID, interviewReminder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RenameNote(ctx, interviewNote.NoteID, "Interview prep note"); err != nil {
		t.Fatal(err)
	}
	dentist, err := s.CreateWithFields(ctx, "Dentist checkup", "Ask about the night guard", time.Now().Add(96*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	passport, err := s.CreateReminder(ctx, "Renew passport", time.Now().Add(120*time.Hour).UTC().Format(time.RFC3339), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	grocery, _, err := s.CreateNoteRequest(ctx, "grocery-note", "Oat milk, cardamom, and cereal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RenameNote(ctx, grocery.NoteID, "Grocery list"); err != nil {
		t.Fatal(err)
	}

	type gateCase struct {
		text, recent, tool, wantID, wantKind string
	}
	plannerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			t.Fatalf("planner request: %#v %v", request, err)
		}
		var state routing.State
		if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &state); err != nil {
			t.Fatal(err)
		}
		plan := routing.ActionPlan{Kind: strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(state.SelectedTool, "lookup_"), "edit_"), "delete_")}
		switch {
		case strings.HasPrefix(state.SelectedTool, "lookup_"):
			plan.Action = "lookup"
		case strings.HasPrefix(state.SelectedTool, "delete_"):
			plan.Action = "delete"
		case strings.HasPrefix(state.SelectedTool, "edit_"):
			plan.Action = "edit"
			switch plan.Kind {
			case "task":
				plan.Field, plan.Value = "status", "completed"
			case "reminder":
				plan.Field, plan.Value = "status", "dismissed"
			case "note":
				plan.Field, plan.Value = "title", "Updated note"
			}
		}
		// This test verifies context ranking. Model extraction is simulated
		// separately from grounding ambiguous follow-ups.
		if len(state.Context) > 0 {
			plan.TargetID = state.Context[0].ID
		}
		content, _ := json.Marshal(plan)
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "context-gate-planner", "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1}})
	}))
	defer plannerServer.Close()
	assertOrchestration := func(t *testing.T, tc gateCase) {
		t.Helper()
		service := routingService{jev: contextGateJev{tool: tc.tool}, free: routing.FreePlanner{APIKey: "test", Endpoint: plannerServer.URL, Client: plannerServer.Client()}, configured: true, secret: "test"}
		result, err := evaluateJevPipeline(t.Context(), s, service, routing.Request{Version: routing.Version, RequestID: "context-gate", Text: tc.text, Timezone: "UTC", RecentConversation: tc.recent})
		if err != nil {
			t.Fatal(err)
		}
		wantCategory, wantAction := "", ""
		switch {
		case strings.HasPrefix(tc.tool, "lookup_"):
			wantCategory, wantAction = "read", "lookup"
		case strings.HasPrefix(tc.tool, "edit_"):
			wantCategory, wantAction = "change", "edit"
		case strings.HasPrefix(tc.tool, "delete_"):
			wantCategory, wantAction = "remove", "delete"
		}
		if result.State != "routed" || result.Category != wantCategory || result.SelectedTool != tc.tool || result.Plan == nil || result.Plan.Action != wantAction || result.Plan.Kind != tc.wantKind || result.Plan.TargetID != tc.wantID {
			t.Fatalf("unexpected orchestration: state=%s category=%s tool=%s plan=%#v; want %s/%s/%s target %s", result.State, result.Category, result.SelectedTool, result.Plan, wantCategory, tc.tool, wantAction, tc.wantID)
		}
		if wantAction == "edit" && (result.Plan.Field == "" || result.Plan.Value == "") {
			t.Fatalf("edit reached orchestration without a field and value: %#v", result.Plan)
		}
	}
	cases := []gateCase{
		{"When was that again?", "User: Let's deal with the Interview with Maya", "lookup_task", interview.ID, "task"},
		{"Push it to Friday afternoon", "Atlas: Task Interview with Maya is open", "edit_task", interview.ID, "task"},
		{"Actually make that Saturday", "Atlas: Moved Interview with Maya to Friday", "edit_task", interview.ID, "task"},
		{"Is it still open?", "User: I meant the Interview with Maya", "lookup_task", interview.ID, "task"},
		{"Finish that one", "Atlas: We are looking at Interview with Maya", "edit_task", interview.ID, "task"},
		{"Bring it back", "Atlas: Interview with Maya was completed", "edit_task", interview.ID, "task"},
		{"Lose the whole thing", "User: About my Interview with Maya", "delete_task", interview.ID, "task"},
		{"What is attached to it?", "Atlas: Interview with Maya is due soon", "lookup_task", interview.ID, "task"},
		{"Same item, what was the deadline?", "User: Continue with Interview with Maya", "lookup_task", interview.ID, "task"},
		{"Move the one we just discussed by a day", "Atlas: Found Interview with Maya", "edit_task", interview.ID, "task"},

		{"What time will that ping me?", "Atlas: Linked reminder Interview alarm for Interview with Maya", "lookup_reminder", interviewReminder.ID, "reminder"},
		{"Nudge that back an hour", "User: Change the Interview alarm", "edit_reminder", interviewReminder.ID, "reminder"},
		{"Dismiss that alert", "Atlas: Interview alarm is scheduled", "edit_reminder", interviewReminder.ID, "reminder"},
		{"No, mark it handled", "Atlas: We were editing the Interview alarm", "edit_reminder", interviewReminder.ID, "reminder"},
		{"Delete only the ping", "User: The Interview alarm, not the task", "delete_reminder", interviewReminder.ID, "reminder"},
		{"And when is the reminder itself?", "Atlas: Interview with Maya has a linked reminder", "lookup_reminder", interviewReminder.ID, "reminder"},
		{"Make the alert later instead", "User: Back to the Interview alarm", "edit_reminder", interviewReminder.ID, "reminder"},
		{"What does that notification belong to?", "Atlas: Interview alarm is linked to Interview with Maya", "lookup_reminder", interviewReminder.ID, "reminder"},
		{"Keep the task, remove this", "Atlas: The current item is Interview alarm", "delete_reminder", interviewReminder.ID, "reminder"},
		{"Read me that alert once more", "User: I asked about Interview alarm", "lookup_reminder", interviewReminder.ID, "reminder"},

		{"What did I write down for that?", "Atlas: Interview prep note is attached", "lookup_note", interviewNote.NoteID, "note"},
		{"Call that note prep checklist", "User: Rename the Interview prep note", "edit_note", interviewNote.NoteID, "note"},
		{"Replace what it says with bring two copies", "Atlas: Opened Interview prep note", "edit_note", interviewNote.NoteID, "note"},
		{"Read that scrap back", "User: The navy folder note for the interview", "lookup_note", interviewNote.NoteID, "note"},
		{"Bin the note, not the interview", "Atlas: Current record is Interview prep note", "delete_note", interviewNote.NoteID, "note"},
		{"What was in the navy-folder thing?", "User: Continue with the interview note", "lookup_note", interviewNote.NoteID, "note"},
		{"Change only the writing", "Atlas: Interview prep note says Portfolio is in the navy folder", "edit_note", interviewNote.NoteID, "note"},
		{"Show that annotation again", "User: I meant the Interview prep note", "lookup_note", interviewNote.NoteID, "note"},
		{"Give that little note a better title", "Atlas: Found Interview prep note", "edit_note", interviewNote.NoteID, "note"},
		{"Remove just the written bit", "User: We are on Interview prep note", "delete_note", interviewNote.NoteID, "note"},

		{"When is the other appointment?", "User: Switch to Dentist checkup", "lookup_task", dentist.ID, "task"},
		{"Move that appointment to next week", "Atlas: Dentist checkup is open", "edit_task", dentist.ID, "task"},
		{"What was I meant to ask there?", "User: About the night guard at Dentist checkup", "lookup_task", dentist.ID, "task"},
		{"Mark that visit done", "Atlas: Current task is Dentist checkup", "edit_task", dentist.ID, "task"},
		{"Delete the dental one", "User: Return to Dentist checkup", "delete_task", dentist.ID, "task"},

		{"When does that expire warning fire?", "User: Look at Renew passport", "lookup_reminder", passport.ID, "reminder"},
		{"Move that ping to Monday", "Atlas: Renew passport is scheduled", "edit_reminder", passport.ID, "reminder"},
		{"Dismiss the passport one", "User: Still on Renew passport", "edit_reminder", passport.ID, "reminder"},
		{"Show me that reminder again", "Atlas: Current reminder is Renew passport", "lookup_reminder", passport.ID, "reminder"},
		{"Get rid of the renewal alert", "User: Back to Renew passport", "delete_reminder", passport.ID, "reminder"},

		{"What's on that list?", "User: Open Grocery list", "lookup_note", grocery.NoteID, "note"},
		{"Add saffron to it", "Atlas: Grocery list contains oat milk", "edit_note", grocery.NoteID, "note"},
		{"Rename that shopping scribble", "User: We are editing Grocery list", "edit_note", grocery.NoteID, "note"},
		{"Read the cardamom bit", "Atlas: Current note is Grocery list", "lookup_note", grocery.NoteID, "note"},
		{"Throw away that list", "User: Finish with Grocery list", "delete_note", grocery.NoteID, "note"},

		{"Actually, back to the interview—when is it?", "Atlas: Grocery list was the latest note", "lookup_task", interview.ID, "task"},
		{"The passport one instead—move it to Tuesday", "Atlas: We had returned to Interview with Maya", "edit_reminder", passport.ID, "reminder"},
		{"Not that; show the grocery list", "Atlas: Renew passport is selected", "lookup_note", grocery.NoteID, "note"},
		{"Now the dental visit: finish it", "Atlas: Grocery list is open", "edit_task", dentist.ID, "task"},
		{"Back to Maya's note; what did it say?", "Atlas: Dentist checkup was completed", "lookup_note", interviewNote.NoteID, "note"},
	}
	if len(cases) != 50 {
		t.Fatalf("gate must contain exactly 50 utterances, got %d", len(cases))
	}
	for i, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			records, _, err := pipelineContext(ctx, s, tc.text, tc.recent, tc.tool, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) == 0 || records[0].ID != tc.wantID || records[0].Kind != tc.wantKind {
				t.Fatalf("case %d routed context to %#v; want %s %s", i+1, records, tc.wantKind, tc.wantID)
			}
			assertOrchestration(t, tc)
		})
	}

	// Specific references in the current sentence must beat stale conversational
	// focus. These cases intentionally point recent context at the wrong record.
	specificCases := []gateCase{
		{"When is Interview with Maya?", "Atlas: We were discussing Dentist checkup", "lookup_task", interview.ID, "task"},
		{"Show me Dentist checkup", "Atlas: Interview with Maya is selected", "lookup_task", dentist.ID, "task"},
		{"Move Dentist checkup to next Thursday", "Atlas: We just edited Interview with Maya", "edit_task", dentist.ID, "task"},
		{"Delete the task Interview with Maya", "Atlas: Current task is Dentist checkup", "delete_task", interview.ID, "task"},
		{"What time is the Renew passport reminder?", "Atlas: Interview alarm is selected", "lookup_reminder", passport.ID, "reminder"},
		{"Move Interview alarm to 9 AM", "Atlas: We were discussing Renew passport", "edit_reminder", interviewReminder.ID, "reminder"},
		{"Delete Renew passport, the reminder", "Atlas: Current reminder is Interview alarm", "delete_reminder", passport.ID, "reminder"},
		{"Read the reminder called Interview alarm", "Atlas: Renew passport was the last result", "lookup_reminder", interviewReminder.ID, "reminder"},
		{"What does Grocery list say?", "Atlas: Interview prep note is open", "lookup_note", grocery.NoteID, "note"},
		{"Open Interview prep note", "Atlas: Current note is Grocery list", "lookup_note", interviewNote.NoteID, "note"},
		{"Rename Grocery list to Pantry list", "Atlas: We just edited Interview prep note", "edit_note", grocery.NoteID, "note"},
		{"Delete the note Interview prep note", "Atlas: Grocery list is selected", "delete_note", interviewNote.NoteID, "note"},
		{"Show the note containing cardamom", "Atlas: Interview prep note mentions a navy folder", "lookup_note", grocery.NoteID, "note"},
		{"Show the note mentioning the navy folder", "Atlas: Grocery list contains cardamom", "lookup_note", interviewNote.NoteID, "note"},
		{"Finish the task with the night guard details", "Atlas: Renew passport is the recent reminder", "edit_task", dentist.ID, "task"},
	}
	if len(specificCases) != 15 {
		t.Fatalf("specificity gate must contain exactly 15 utterances, got %d", len(specificCases))
	}
	for i, tc := range specificCases {
		t.Run("specific/"+tc.text, func(t *testing.T) {
			records, _, err := pipelineContext(ctx, s, tc.text, tc.recent, tc.tool, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) == 0 || records[0].ID != tc.wantID || records[0].Kind != tc.wantKind {
				t.Fatalf("specificity case %d routed context to %#v; want %s %s", i+1, records, tc.wantKind, tc.wantID)
			}
			assertOrchestration(t, tc)
		})
	}
}

func TestConversationFocusSurvivesLookupSaveAndEdit(t *testing.T) {
	task := store.Task{ID: "task-1", Title: "Interview"}
	for name, c := range map[string]routingConversation{
		"lookup": {Answer: &conversationAnswer{Sources: []conversationSource{{Type: "note", URL: "/#notes/note-1"}}}},
		"save":   {Proposal: &store.CaptureProposal{Input: store.CaptureInput{Kind: "task"}}, Saved: &store.TaskAction{TaskID: task.ID, Task: &task}},
		"edit":   {Mutation: &recordMutation{Action: "edit", Kind: "reminder", ID: "reminder-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			id, kind := conversationFocusRecord(c)
			if id == "" || kind == "" {
				t.Fatalf("focus was dropped: %q %q", id, kind)
			}
		})
	}
	if id, kind := conversationFocusRecord(routingConversation{Mutation: &recordMutation{Action: "delete", Kind: "task", ID: "gone"}}); id != "" || kind != "" {
		t.Fatalf("deleted record remained in focus: %q %q", id, kind)
	}
}

func TestSpecificCurrentRequestOverridesRecentFocusEndToEnd(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	interview, _ := s.CreateWithFields(ctx, "Interview with Maya", "Prepare case study", time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339))
	dentist, _ := s.CreateWithFields(ctx, "Dentist checkup", "Ask about night guard", time.Now().Add(96*time.Hour).UTC().Format(time.RFC3339))
	passport, _ := s.CreateReminder(ctx, "Renew passport", time.Now().Add(120*time.Hour).UTC().Format(time.RFC3339), "UTC")
	grocery, _, _ := s.CreateNoteRequest(ctx, "specific-grocery", "Oat milk and cardamom", "", "")
	_, _ = s.RenameNote(ctx, grocery.NoteID, "Grocery list")
	prep, _, _ := s.CreateNoteRequest(ctx, "specific-prep", "Portfolio is in the navy folder", interview.ID, "")
	_, _ = s.RenameNote(ctx, prep.NoteID, "Interview prep note")

	tools := map[string]string{
		"Show Dentist checkup":                "lookup_task",
		"Complete Interview with Maya":        "edit_task",
		"Dismiss the Renew passport reminder": "edit_reminder",
		"Rename Grocery list to Pantry list":  "edit_note",
		"What does Interview prep note say?":  "lookup_note",
	}
	planner := specificityGatePlanner{t: t, plans: map[string]routing.ActionPlan{
		"Show Dentist checkup":                {Action: "lookup", Kind: "task", TargetID: dentist.ID},
		"Complete Interview with Maya":        {Action: "edit", Kind: "task", TargetID: interview.ID, Field: "status", Value: "completed"},
		"Dismiss the Renew passport reminder": {Action: "edit", Kind: "reminder", TargetID: passport.ID, Field: "status", Value: "dismissed"},
		"Rename Grocery list to Pantry list":  {Action: "edit", Kind: "note", TargetID: grocery.NoteID, Field: "title", Value: "Pantry list"},
		"What does Interview prep note say?":  {Action: "lookup", Kind: "note", TargetID: prep.NoteID},
	}, expectedRecent: map[string]string{
		"Complete Interview with Maya":        dentist.ID,
		"Dismiss the Renew passport reminder": interview.ID,
		"Rename Grocery list to Pantry list":  passport.ID,
		"What does Interview prep note say?":  grocery.NoteID,
	}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: contextGateJev{tools: tools}, free: planner, configured: true, secret: "test"})
	send := func(path string, body any) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code >= 300 {
			t.Fatalf("POST %s: %d %s", path, w.Code, w.Body.String())
		}
		return out
	}
	c := send("/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": "1", "request_id": "specificity-e2e", "text": "Show Dentist checkup", "timezone": "UTC"})
	if c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Dentist checkup") {
		t.Fatal(c)
	}
	path := "/api/v1/conversations/routing/" + c["id"].(string) + "/reply"
	for _, sentence := range []string{"Complete Interview with Maya", "Dismiss the Renew passport reminder", "Rename Grocery list to Pantry list", "What does Interview prep note say?"} {
		c = send(path, map[string]any{"version": c["version"], "text": sentence})
		if c["state"] != "answered" {
			t.Fatalf("%q did not complete: %#v", sentence, c)
		}
	}
	interviewState, _ := s.TaskState(ctx, interview.ID)
	dentistState, _ := s.TaskState(ctx, dentist.ID)
	passportState, _ := s.ReminderState(ctx, passport.ID)
	groceryState, _ := s.NoteState(ctx, grocery.NoteID)
	if interviewState.Task.Status != "completed" || dentistState.Task.Status != "open" || passportState.Reminder.Status != "dismissed" || groceryState.Note.Title != "Pantry list" || !strings.Contains(c["prompt"].(string), "navy folder") {
		t.Fatalf("specific requests did not override focus: interview=%#v dentist=%#v passport=%#v grocery=%#v response=%#v", interviewState, dentistState, passportState, groceryState, c)
	}
}
