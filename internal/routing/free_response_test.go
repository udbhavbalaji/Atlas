package routing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type responseRoundTrip func(*http.Request) (*http.Response, error)

func (f responseRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFreeResponderPinsOneWritingModelAndChecksPrivacy(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: responseRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("bad request"))}, nil
		}
		if body["model"] != WritingModel || body["max_tokens"] != float64(300) {
			t.Error(body)
		}
		provider, _ := body["provider"].(map[string]any)
		if provider["data_collection"] != "deny" || provider["zdr"] != true {
			t.Error(provider)
		}
		messages, _ := body["messages"].([]any)
		if len(messages) != 2 {
			t.Error(messages)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"text\":\"Here is what I found.\"}"}}]}`))}, nil
	})}
	got, model, err := (FreeResponder{APIKey: "fixture", Client: client}).Respond(context.Background(), map[string]any{"response_mode": "list_intro", "canonical_answer": "Here are your open reminders:\n“Buy bread”"})
	if err != nil || got != "Here is what I found." || model != WritingModel || calls != 1 {
		t.Fatal(got, model, calls, err)
	}
}

func TestFreeResponderDoesNotRetryAnotherModel(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: responseRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("busy"))}, nil
	})}
	_, _, err := (FreeResponder{APIKey: "fixture", Client: client}).Respond(context.Background(), map[string]any{"canonical_answer": "Verified answer"})
	if err == nil || calls != 1 {
		t.Fatal(calls, err)
	}
}
