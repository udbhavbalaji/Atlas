package routing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolRegistryMatchesSelectedPlans(t *testing.T) {
	tools := Tools()
	if len(tools) != 13 {
		t.Fatalf("got %d tools", len(tools))
	}
	for _, tool := range tools {
		if tool.ID == "" || tool.Channel == "" || tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("incomplete tool: %+v", tool)
		}
		if got, ok := Tool(tool.ID); !ok || got.Category != tool.Category {
			t.Fatalf("lookup failed for %q", tool.ID)
		}
	}
	if got := ToolChoices("create"); len(got) != 4 || got[0].ID != "task" || got[3].ID != "clarify" {
		t.Fatalf("create choices changed: %+v", got)
	}
	if !ToolPlanMatches("task", ActionPlan{Action: "task"}) || !ToolPlanMatches("lookup_all", ActionPlan{Action: "lookup", Kind: "note"}) || !ToolPlanMatches("edit_note", ActionPlan{Action: "edit", Kind: "note"}) {
		t.Fatal("matching proposal rejected")
	}
	for _, pair := range []struct {
		tool string
		plan ActionPlan
	}{
		{"task", ActionPlan{Action: "reminder"}},
		{"lookup_task", ActionPlan{Action: "lookup", Kind: "note"}},
		{"edit_task", ActionPlan{Action: "edit", Kind: "reminder"}},
		{"delete_note", ActionPlan{Action: "delete", Kind: "task"}},
		{"unknown", ActionPlan{Action: "task"}},
	} {
		if ToolPlanMatches(pair.tool, pair.plan) {
			t.Fatalf("mismatched proposal accepted: %+v", pair)
		}
	}
}

func TestSelectedToolGetsOnlyItsArgumentSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ResponseFormat struct {
				JSONSchema struct {
					Schema struct {
						Properties map[string]any `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		fields := request.ResponseFormat.JSONSchema.Schema.Properties
		if len(fields) != 4 || fields["action"] == nil || fields["kind"] == nil || fields["note_body"] == nil || fields["linked_task_id"] == nil || fields["due_at"] != nil || fields["target_id"] != nil {
			t.Fatalf("note extractor received unrelated fields: %+v", fields)
		}
		_, _ = w.Write([]byte(`{"model":"fixture","choices":[{"message":{"content":"{\"action\":\"note\",\"kind\":\"note\",\"note_body\":\"Hello\",\"linked_task_id\":\"\"}"}}]}`))
	}))
	defer server.Close()
	result, err := (FreePlanner{APIKey: "fixture", Endpoint: server.URL, Client: server.Client()}).Plan(t.Context(), State{Text: "Note this: Hello", SelectedTool: "note"})
	if err != nil || result.Plan.NoteBody != "Hello" {
		t.Fatal(result, err)
	}
}

func TestSelectedToolRejectsDifferentModelAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"fixture","choices":[{"message":{"content":"{\"action\":\"task\",\"kind\":\"task\",\"title\":\"Wrong record\"}"}}]}`))
	}))
	defer server.Close()
	_, err := (FreePlanner{APIKey: "fixture", Endpoint: server.URL, Client: server.Client()}).Plan(t.Context(), State{Text: "Note this", SelectedTool: "note"})
	if err == nil {
		t.Fatal("extractor changed Jev's selected tool")
	}
}
