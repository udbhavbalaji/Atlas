package httpapi

import (
	"atlas/internal/routing"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type writingFixture struct {
	text  string
	calls int
	mode  string
}

func (w *writingFixture) Respond(_ context.Context, facts map[string]any) (string, string, error) {
	w.calls++
	w.mode, _ = facts["response_mode"].(string)
	return w.text, routing.WritingModel, nil
}

func TestWritingStageKeepsVerifiedListItemsAcrossProviders(t *testing.T) {
	canonical := "Here are your open reminders:\n“Pay rent” — tomorrow at 9 AM\n“Call the doctor” — Friday at 2 PM"
	writer := &writingFixture{text: "Here are the reminders you asked about:"}
	c := routingConversation{State: "answered", Route: routing.Result{Provider: "free", Input: routing.State{Text: "What open reminders do I have?"}}, Answer: &conversationAnswer{Text: canonical, Sources: []conversationSource{{Title: "Pay rent"}, {Title: "Call the doctor"}}}}
	c.appendAssistant(httptest.NewRequest("GET", "/", nil), routingService{responder: writer})
	if writer.calls != 1 || writer.mode != "list_intro" || c.ResponseModel != routing.WritingModel {
		t.Fatal(writer, c.ResponseModel)
	}
	if c.Messages[0].Text != "Here are the reminders you asked about:\n“Pay rent” — tomorrow at 9 AM\n“Call the doctor” — Friday at 2 PM" || c.Answer.Text != canonical {
		t.Fatal(c.Messages, c.Answer)
	}
}

func TestWritingStageRejectsUngroundedListIntro(t *testing.T) {
	canonical := "Here are your open reminders:\n“Pay rent”"
	writer := &writingFixture{text: "I completed your 2 reminders today."}
	c := routingConversation{State: "answered", Route: routing.Result{Provider: "groq"}, Answer: &conversationAnswer{Text: canonical, Sources: []conversationSource{{Title: "Pay rent"}}}}
	c.appendAssistant(httptest.NewRequest("GET", "/", nil), routingService{responder: writer})
	if writer.calls != 1 || c.ResponseModel != "local" || c.Messages[0].Text != canonical || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "verified") {
		t.Fatal(writer, c)
	}
}

func TestWritingStageSkipsMockFixture(t *testing.T) {
	writer := &writingFixture{text: "Changed"}
	c := routingConversation{State: "answered", Route: routing.Result{Provider: "mock"}, Answer: &conversationAnswer{Text: "Verified"}}
	c.appendAssistant(httptest.NewRequest("GET", "/", nil), routingService{responder: writer})
	if writer.calls != 0 || c.Messages[0].Text != "Verified" {
		t.Fatal(writer, c)
	}
}

func TestResponsePreviewUsesNoModelAndPreservesFixtureDetails(t *testing.T) {
	mux := http.NewServeMux()
	responsePreviewRoutes(mux)
	for _, tc := range []struct {
		candidate string
		accepted  bool
		message   string
	}{
		{"Here are the reminders you asked about:", true, "Here are the reminders you asked about:\n“Pay rent” — tomorrow at 9 AM"},
		{"I completed your 2 reminders today.", false, "Here are your open reminders:\n“Pay rent” — tomorrow at 9 AM"},
	} {
		body, _ := json.Marshal(map[string]any{"canonical": "Here are your open reminders:\n“Pay rent” — tomorrow at 9 AM", "candidate": tc.candidate, "titles": []string{"Pay rent"}})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/response-test/preview", strings.NewReader(string(body))))
		var result struct {
			Accepted   bool   `json:"accepted"`
			Message    string `json:"message"`
			ModelCalls int    `json:"model_calls"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Accepted != tc.accepted || result.Message != tc.message || result.ModelCalls != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
