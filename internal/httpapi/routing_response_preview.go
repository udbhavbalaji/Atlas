package httpapi

import (
	"net/http"
	"strings"
)

// The response test page uses this fixture-only route to inspect the same
// acceptance gate as a live writing request. It never calls a model or reads
// or changes saved records.
func responsePreviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /response-test/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Canonical string   `json:"canonical"`
			Candidate string   `json:"candidate"`
			Titles    []string `json:"titles"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Canonical) > 5000 || len(input.Candidate) > 1200 || len(input.Titles) > 10 {
			apiError(w, 400, "invalid_response_preview", "Preview text is too long.", false)
			return
		}
		answer := conversationAnswer{Text: input.Canonical}
		for _, title := range input.Titles {
			answer.Sources = append(answer.Sources, conversationSource{Title: title})
		}
		accepted := false
		message := input.Canonical
		if details := verifiedListDetails(input.Canonical); details != "" {
			accepted = safeListIntro(input.Candidate, &answer)
			if accepted {
				message = strings.TrimSpace(input.Candidate) + "\n" + details
			}
		} else {
			c := routingConversation{State: "answered", Answer: &answer}
			accepted = c.responsePreservesFacts(input.Candidate)
			if accepted {
				message = input.Candidate
			}
		}
		respond(w, 200, map[string]any{"accepted": accepted, "message": message, "model_calls": 0})
	})
}
