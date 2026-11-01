package httpapi

import (
	"atlas/internal/routing"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type evalRoundTripFunc func(*http.Request) (*http.Response, error)

func (f evalRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func scriptedLiveTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return evalRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		if err = json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		var response any
		if questions, ok := input["questions"].(map[string]any); ok {
			criteria := questions["primary_action"].(map[string]any)["criteria"].(map[string]any)
			state := input["state"].(map[string]any)
			request, _ := state["text"].(string)
			choice := "create"
			switch {
			case criteria["reminder"] != nil:
				choice = "reminder"
			case criteria["edit_task"] != nil:
				choice = "edit_task"
			case criteria["lookup_task"] != nil:
				choice = "lookup_task"
			case strings.Contains(request, "Send an email"):
				choice = "unsupported"
			case strings.Contains(request, "Move my interview"):
				choice = "change"
			case strings.Contains(request, "When is my interview"):
				choice = "read"
			}
			probabilities := map[string]float64{}
			for key := range criteria {
				probabilities[key] = 0
			}
			probabilities[choice] = 1
			response = map[string]any{"model": routing.OpenRouterModel, "answers": map[string]any{"primary_action": map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": probabilities}}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2}}
		} else {
			messages := input["messages"].([]any)
			var state map[string]any
			if err = json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &state); err != nil {
				t.Fatal(err)
			}
			plan := map[string]any{"action": "reminder", "kind": "reminder", "title": "Call Rahul", "reminder_at": "2035-01-01T10:00:00Z", "note_body": "Discuss the battery issue"}
			switch state["selected_tool"] {
			case "edit_task":
				plan = map[string]any{"action": "edit", "kind": "task", "target_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "field": "title", "value": "Interview with Maya and Rahul"}
			case "lookup_task":
				plan = map[string]any{"action": "lookup", "kind": "task", "target_query": "Interview with Maya"}
			}
			content, _ := json.Marshal(plan)
			response = map[string]any{"model": routing.GroqModel, "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}}
		}
		encoded, _ := json.Marshal(response)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(encoded)), Header: make(http.Header)}, nil
	})
}

func TestLiveEvalModesUseSimulatedHTTP(t *testing.T) {
	for _, mode := range []string{"jev", "groq", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			report, err := RunLiveOrchestrationEval(t.Context(), LiveOrchestrationEvalOptions{Mode: mode, JevAPIKey: "fixture", GroqAPIKey: "fixture", MaxRequests: 8, JevEndpoint: "https://example.test/decisions", GroqEndpoint: "https://example.test/chat", HTTPTransport: scriptedLiveTransport(t)})
			if err != nil || report.Total == 0 || report.Requests > 8 {
				t.Fatal(err, report)
			}
			for _, c := range report.Cases {
				if c.Error != "" {
					t.Errorf("%s: %s", c.ID, c.Error)
				}
			}
		})
	}
}

func TestLiveEvalRequiresKeysAndCapsRequests(t *testing.T) {
	if _, err := RunLiveOrchestrationEval(t.Context(), LiveOrchestrationEvalOptions{Mode: "jev", MaxRequests: 4}); err == nil {
		t.Fatal("missing key accepted")
	}
	report, err := RunLiveOrchestrationEval(t.Context(), LiveOrchestrationEvalOptions{Mode: "jev", JevAPIKey: "fixture", MaxRequests: 1, JevEndpoint: "https://example.test/decisions", HTTPTransport: scriptedLiveTransport(t)})
	if err != nil || report.Requests != 1 || report.Gaps == 0 {
		t.Fatal(err, report)
	}
}
