package routing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRoutingDecisions(t *testing.T) {
	request := Request{Version: Version, RequestID: "test", Text: "I have an interview at Ather", Timezone: "UTC"}
	if err := ValidateRequest(request); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ fixture, state, channel string }{{"task", "routed", "tasks"}, {"reminder", "routed", "reminders"}, {"note", "routed", "notes"}, {"lookup", "routed", "lookup"}, {"ambiguous", "needs_clarification", ""}, {"clarify", "needs_clarification", ""}, {"unsupported", "unsupported", ""}} {
		e, err := (Mock{}).Evaluate(t.Context(), State{}, Registry(), tc.fixture)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Decide(request, State{}, e, "mock", true)
		if err != nil || r.State != tc.state || r.SelectedChannel != tc.channel || r.Persisted {
			t.Fatal(tc, r, err)
		}
	}
	for _, change := range []func(*Choice){func(c *Choice) { c.Choice = "unknown" }, func(c *Choice) { delete(c.Probabilities, "note") }, func(c *Choice) { c.Probabilities["note"] = 0.7 }, func(c *Choice) { c.Confidence = math.NaN() }, func(c *Choice) { c.Probabilities["note"] = -0.1 }} {
		e, _ := (Mock{}).Evaluate(t.Context(), State{}, Registry(), "task")
		change(&e.Decision)
		if ValidateChoice(e.Decision, Registry()) == nil {
			t.Fatal(e)
		}
	}
	request.Timezone = "Local"
	if ValidateRequest(request) == nil {
		t.Fatal("local timezone accepted")
	}
}

func TestClearModerateProbabilityEditRoutes(t *testing.T) {
	request := Request{Version: Version, RequestID: "edit", Text: "Reschedule my interview to 4 October at 7pm", Timezone: "Asia/Kolkata"}
	decision, err := (Mock{}).Evaluate(t.Context(), State{}, Registry(), "edit")
	if err != nil {
		t.Fatal(err)
	}
	for id := range decision.Decision.Probabilities {
		decision.Decision.Probabilities[id] = 0
	}
	decision.Decision.Probabilities["edit"] = 0.64
	decision.Decision.Probabilities["task"] = 0.28
	decision.Decision.Probabilities["clarify"] = 0.04
	decision.Decision.Probabilities["unsupported"] = 0.04
	result, err := Decide(request, State{}, decision, "jev", false)
	if err != nil || result.State != "routed" || result.SelectedChannel != "edit" {
		t.Fatal(result, err)
	}
}

func TestJevAcceptsRoundedProbabilitiesAndMissingConfidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probabilities := map[string]float64{}
		for _, action := range Registry() {
			probabilities[action.ID] = 0
		}
		probabilities["edit"] = 0.79
		probabilities["task"] = 0.20
		json.NewEncoder(w).Encode(map[string]any{"model": OpenRouterModel, "answers": map[string]any{"primary_action": map[string]any{"type": "choice", "choice": "edit", "probabilities": probabilities}}})
	}))
	defer server.Close()
	evaluation, err := (Jev{APIKey: "test", Endpoint: server.URL}).Evaluate(t.Context(), State{Text: "Reschedule my interview"}, Registry(), "")
	if err != nil || math.Abs(evaluation.Decision.Probabilities["edit"]+evaluation.Decision.Probabilities["task"]-1) > 0.0001 || evaluation.Decision.Confidence <= 0 {
		t.Fatal(evaluation, err)
	}
}

func TestJevWireContractAndRedactedFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-only-secret" || r.URL.Path != "/api/alpha/decisions" {
			t.Error("missing auth or wrong method")
		}
		var request struct {
			Model     string `json:"model"`
			State     State  `json:"state"`
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != OpenRouterModel || len(request.Questions) != 1 || len(request.Questions["primary_action"].Criteria) != len(Registry()) {
			t.Error(request)
		}
		state := request.State
		if state.Text != "Interview" {
			t.Error(request.State)
		}
		e, _ := (Mock{}).Evaluate(r.Context(), state, Registry(), "task")
		json.NewEncoder(w).Encode(map[string]any{"model": OpenRouterModel, "answers": map[string]any{"primary_action": e.Decision}, "usage": map[string]any{"input_tokens": 275, "output_tokens": 20, "cost": 0.00001155}})
	}))
	defer server.Close()
	j := Jev{APIKey: "test-only-secret", Endpoint: server.URL + "/api/alpha/decisions", Client: server.Client()}
	e, err := j.Evaluate(t.Context(), State{Text: "Interview"}, Registry(), "")
	if err != nil || e.Decision.Choice != "task" || e.Usage.CostUSD != "0.00001155" || calls != 1 {
		t.Fatal(e, err, calls)
	}
	for _, body := range []string{`{"model":"x","answers":{"primary_action":{"type":"choice","choice":"task","probabilities":{"task":1}}}}`, `{"model":"x","answers":{}}`, `not JSON`, string(make([]byte, 262145))} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		j.Endpoint = s.URL
		_, err = j.Evaluate(t.Context(), State{}, Registry(), "")
		s.Close()
		if !errors.Is(err, ErrContract) {
			t.Fatal(err)
		}
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); w.Write([]byte("test-only-secret")) }))
	defer failure.Close()
	j.Endpoint = failure.URL
	_, err = j.Evaluate(t.Context(), State{}, Registry(), "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	j.APIKey = ""
	_, err = j.Evaluate(t.Context(), State{}, Registry(), "")
	if err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestJevHonorsCancellationAndDoesNotFollowRedirects(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := (Jev{APIKey: "test", Endpoint: s.URL}).Evaluate(ctx, State{}, Registry(), "")
	if err != ErrUnavailable {
		t.Fatal(err)
	}
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err = (Jev{APIKey: "test", Endpoint: redirect.URL}).Evaluate(t.Context(), State{}, Registry(), "")
	if !errors.Is(err, ErrUnavailable) || redirectCalls != 0 {
		t.Fatal(err, redirectCalls)
	}
}
