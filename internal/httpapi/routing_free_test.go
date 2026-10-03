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

type fixedPlanner struct {
	plan  routing.ActionPlan
	calls int
}

func (p *fixedPlanner) Plan(_ context.Context, _ routing.State) (routing.PlanResult, error) {
	p.calls++
	return routing.PlanResult{Plan: p.plan, Model: "free-fixture"}, nil
}

func TestFreeConversationEditsExactRecordAndConfirmsDelete(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	newDue := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	task, err := s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", old)
	if err != nil {
		t.Fatal(err)
	}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "edit", Kind: "task", TargetID: task.ID, Field: "due_at", Value: newDue}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{free: planner, configured: true, secret: "test"})
	send := func(path string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "free", "version": "1", "request_id": "free-edit", "text": "Could you shift my interview to later this week?", "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "from") || !strings.Contains(c["prompt"].(string), "to") {
		t.Fatal(code, c)
	}
	current, err := s.TaskState(t.Context(), task.ID)
	gotTime, parseErr := time.Parse(time.RFC3339Nano, current.Task.DueAt)
	wantTime, _ := time.Parse(time.RFC3339Nano, newDue)
	if err != nil || parseErr != nil || !gotTime.Equal(wantTime) || planner.calls != 1 {
		t.Fatal(current, err, planner.calls)
	}
	planner.plan = routing.ActionPlan{Action: "delete", Kind: "task", TargetID: task.ID}
	code, c = send("/api/v1/conversations/routing", map[string]any{"provider": "free", "version": "1", "request_id": "free-delete", "text": "Get rid of my interview", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_delete_confirmation" || !strings.Contains(c["prompt"].(string), "Interview with Maya") {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err != nil {
		t.Fatal("deleted without confirmation", err)
	}
	code, c = send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": "yes"})
	if code != 200 || c["state"] != "answered" {
		t.Fatal(code, c)
	}
	if _, err = s.TaskState(t.Context(), task.ID); err == nil {
		t.Fatal("confirmed delete did not happen")
	}
}

func TestFreeConversationRenamesNoteFromExtractedTitleAndPreservesBody(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	created, _, err := s.CreateNoteRequest(t.Context(), "rename-note-test", "- Bread\n- Cereal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "edit", Kind: "note", TargetID: created.NoteID, Title: "grocery list"}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{free: planner, configured: true, secret: "test"})
	data, _ := json.Marshal(map[string]any{"provider": "free", "version": "1", "request_id": "rename-note", "text": "Rename this notes title to grocery list.", "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/conversations/routing", bytes.NewReader(data)))
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 201 || response["state"] != "answered" || !strings.Contains(response["prompt"].(string), "title from unset to “grocery list”") {
		t.Fatal(w.Code, w.Body.String())
	}
	state, err := s.NoteState(t.Context(), created.NoteID)
	if err != nil || state.Note.Title != "grocery list" || state.Note.Body != "- Bread\n- Cereal" {
		t.Fatal(state, err)
	}
}

func TestFreeConversationNoteTitleFollowupDoesNotLoop(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	created, _, err := s.CreateNoteRequest(t.Context(), "rename-note-reply", "- Bread\n- Cereal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "edit", Kind: "note", TargetID: created.NoteID}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{free: planner, configured: true, secret: "test"})
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
	c := send("/api/v1/conversations/routing", map[string]any{"provider": "free", "version": "1", "request_id": "rename-note-followup", "text": "Edit this note", "timezone": "UTC"})
	if c["state"] != "awaiting_field" {
		t.Fatal(c)
	}
	path := "/api/v1/conversations/routing/" + c["id"].(string) + "/reply"
	c = send(path, map[string]any{"version": c["version"], "text": "title"})
	if c["state"] != "awaiting_change" {
		t.Fatal(c)
	}
	c = send(path, map[string]any{"version": c["version"], "text": "grocery list."})
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	state, err := s.NoteState(t.Context(), created.NoteID)
	if err != nil || state.Note.Title != "grocery list" || state.Note.Body != "- Bread\n- Cereal" {
		t.Fatal(state, err)
	}
}

func TestFreeConversationRejectsInventedRecord(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "delete", Kind: "task", TargetID: "invented"}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{free: planner, configured: true, secret: "test"})
	body, _ := json.Marshal(map[string]any{"provider": "free", "version": "1", "request_id": "invented", "text": "Delete my interview", "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/routing", bytes.NewReader(body)))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "routing_evaluation_rejected") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestFreeConversationLookupAndCapturePlan(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.CreateWithFields(t.Context(), "Interview with Maya", "Bring portfolio", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CreateNoteRequest(t.Context(), "lookup-linked-note", "Print directions", task.ID, ""); err != nil {
		t.Fatal(err)
	}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "lookup", Kind: "task", TargetID: task.ID}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{free: planner, configured: true, secret: "test"})
	send := func(body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/routing", bytes.NewReader(data)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		return w.Code, out
	}
	code, c := send(map[string]any{"provider": "free", "version": "1", "request_id": "free-lookup", "text": "When is my interview?", "timezone": "UTC"})
	if code != 201 || c["state"] != "answered" || !strings.Contains(c["prompt"].(string), "Interview with Maya") || !strings.Contains(c["prompt"].(string), "due") || !strings.Contains(c["prompt"].(string), "Print directions") {
		t.Fatal(code, c)
	}
	planner.plan = routing.ActionPlan{Action: "task", Kind: "task", Title: "Book train tickets", Details: "For the conference"}
	code, c = send(map[string]any{"provider": "free", "version": "1", "request_id": "free-capture", "text": "Please put booking train tickets on my list for the conference", "timezone": "UTC"})
	if code != 201 || c["state"] != "awaiting_confirmation" {
		t.Fatal(code, c)
	}
	proposal := c["proposal"].(map[string]any)["input"].(map[string]any)
	if proposal["title"] != "Book train tickets" || proposal["details"] != "For the conference" {
		t.Fatal(proposal)
	}
}

func TestGroqConversationUsesStructuredPlan(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.Create(t.Context(), "Interview with Maya")
	if err != nil {
		t.Fatal(err)
	}
	planner := &fixedPlanner{plan: routing.ActionPlan{Action: "lookup", Kind: "task", TargetID: task.ID}}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{groq: planner, groqConfigured: true, secret: "test"})
	body, _ := json.Marshal(map[string]any{"provider": "groq", "version": "1", "request_id": "groq-lookup", "text": "How is my interview going?", "timezone": "UTC"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/routing", bytes.NewReader(body)))
	if w.Code != 201 || !strings.Contains(w.Body.String(), "Interview with Maya") || planner.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), planner.calls)
	}
}
