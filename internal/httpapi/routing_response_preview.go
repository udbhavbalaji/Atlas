package httpapi

import (
	"net/http"
	"strings"
)

// The response test page uses this fixture-only route to inspect the list
// introduction gate and exact-answer fallback. It never calls a model or
// reads or changes saved records.
func responsePreviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /response-test/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Canonical string `json:"canonical"`
			Candidate string `json:"candidate"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Canonical) > 5000 || len(input.Candidate) > 1200 {
			apiError(w, 400, "invalid_response_preview", "Preview text is too long.", false)
			return
		}
		accepted := false
		message := input.Canonical
		if details := verifiedListDetails(input.Canonical); details != "" {
			accepted = safeListIntro(input.Candidate, input.Canonical)
			if accepted {
				message = strings.TrimSpace(input.Candidate) + "\n" + details
			}
		} else {
			accepted = input.Candidate == input.Canonical
		}
		respond(w, 200, map[string]any{"accepted": accepted, "message": message, "model_calls": 0})
	})
}
