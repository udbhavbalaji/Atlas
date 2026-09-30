package routing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"atlas/internal/provider"
)

func TestFreePlannerStructuredActionAndGroundedTarget(t *testing.T) {
	state := State{Text: "Could you shift my interview to tomorrow?", Timezone: "UTC", ReferenceAt: "2026-09-30T09:00:00Z", Context: []provider.ContextRecord{{ID: "task-1", Kind: "task", Title: "Interview"}}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("request authentication")
		}
		var request struct {
			Model    string `json:"model"`
			Provider struct {
				RequireParameters bool `json:"require_parameters"`
			} `json:"provider"`
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Strict bool `json:"strict"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != FreeModel || !request.Provider.RequireParameters || request.ResponseFormat.Type != "json_schema" || !request.ResponseFormat.JSONSchema.Strict {
			t.Fatal("request contract", request, err)
		}
		content, _ := json.Marshal(ActionPlan{Action: "edit", Kind: "task", TargetID: "task-1", Field: "due_at", Value: "2026-10-01T09:00:00Z"})
		json.NewEncoder(w).Encode(map[string]any{"model": "free-test-model", "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20}})
	}))
	defer server.Close()
	result, err := (FreePlanner{APIKey: "test-key", Endpoint: server.URL, Client: server.Client()}).Plan(t.Context(), state)
	if err != nil || result.Plan.TargetID != "task-1" || calls != 1 || result.Usage.InputTokens != 100 {
		t.Fatal(result, err, calls)
	}
	if !errors.Is(ValidatePlan(ActionPlan{Action: "edit", Kind: "task", TargetID: "invented", Field: "due_at"}, state), ErrContract) {
		t.Fatal("invented target ID accepted")
	}
	if !errors.Is(ValidatePlan(ActionPlan{Action: "edit", Kind: "note", Field: "due_at"}, state), ErrContract) {
		t.Fatal("invalid edit field accepted")
	}
	if !errors.Is(ValidatePlan(ActionPlan{Action: "reminder", ReminderAt: "2026-10-02T17:00:00Z"}, State{Text: "Remind me today at 5 PM", Timezone: "UTC", ReferenceAt: "2026-09-30T09:00:00Z"}), ErrContract) {
		t.Fatal("wrong relative date accepted")
	}
	if err := ValidatePlan(ActionPlan{Action: "reminder", ReminderAt: "2026-09-30T17:00:00Z"}, State{Text: "Remind me today at 5 PM", Timezone: "UTC", ReferenceAt: "2026-09-30T09:00:00Z"}); err != nil {
		t.Fatal("correct relative date rejected", err)
	}
	linked := State{Context: []provider.ContextRecord{{ID: "task-1", Kind: "task"}, {ID: "note-1", Kind: "note", TaskID: "task-1"}}}
	if err := ValidatePlan(ActionPlan{Action: "edit", Kind: "note", TargetID: "note-1", LinkedTaskID: "task-1", Field: "body", Value: "Updated note"}, linked); err != nil {
		t.Fatal("grounded link metadata on edit rejected", err)
	}
	if got := NormalizePlan(ActionPlan{Action: "edit", Kind: "task", Field: "status", Value: "complete"}).Value; got != "completed" {
		t.Fatal("task completion was not normalized", got)
	}
}

func TestGroqPlannerUsesStructuredSchemaWithoutOpenRouterRouting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != GroqModel || request["reasoning_effort"] != "low" || request["provider"] != nil || request["max_completion_tokens"] == nil {
			t.Fatal(request)
		}
		content, _ := json.Marshal(ActionPlan{Action: "lookup", Kind: "task"})
		json.NewEncoder(w).Encode(map[string]any{"model": GroqModel, "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}})
	}))
	defer server.Close()
	result, err := (FreePlanner{APIKey: "test-groq-key", Service: "groq", Endpoint: server.URL, Client: server.Client()}).Plan(t.Context(), State{Text: "When is my interview?"})
	if err != nil || result.Plan.Action != "lookup" {
		t.Fatal(result, err)
	}
}
