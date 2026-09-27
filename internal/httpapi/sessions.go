package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/store"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type conversationTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}
type conversation struct {
	State          string             `json:"state"`
	ContextTaskID  string             `json:"context_task_id"`
	Lead           int                `json:"reminder_lead_minutes"`
	Interpretation interpret.Result   `json:"interpretation"`
	Messages       []conversationTurn `json:"messages"`
	Saved          *store.TaskAction  `json:"saved,omitempty"`
}

func conversationPrompt(c conversation) string {
	switch c.State {
	case "saved":
		return "Saved. Start a new conversation for another capture."
	case "cancelled":
		return "Cancelled. No records created."
	case "confirming":
		return "Confirmation is in progress. Retry confirm to recover the saved result."
	}
	if c.Interpretation.Status == "ready" {
		return "Review the proposal below. Reply yes to save, a new time to adjust, or cancel."
	}
	prompts := []string{}
	for _, q := range c.Interpretation.Questions {
		prompts = append(prompts, q.Message)
	}
	return strings.Join(prompts, " ")
}
func conversationResponse(w http.ResponseWriter, status int, d store.SessionDocument, c conversation) {
	c.Interpretation = interpret.WithContinuation(c.Interpretation, c.ContextTaskID, c.Lead)
	c.Interpretation.Continuation.State = c.State
	if c.State == "saved" || c.State == "cancelled" || c.State == "confirming" {
		c.Interpretation.Continuation.NextCalls = []interpret.ContinuationCall{}
		c.Interpretation.Continuation.Clarifications = []interpret.ClarificationRequest{}
		c.Interpretation.Continuation.Persisted = c.State == "saved"
		c.Interpretation.Continuation.ConfirmationRequired = c.State == "confirming"
	}
	respond(w, status, map[string]any{"id": d.ID, "version": d.Version, "updated_at": d.UpdatedAt, "state": c.State, "messages": c.Messages, "interpretation": c.Interpretation, "saved": c.Saved, "next_request": map[string]any{"method": "POST", "path": "/api/v1/capture/sessions/" + d.ID + "/reply", "body": map[string]any{"version": d.Version, "text": ""}, "confirmation_required": c.State == "awaiting_confirmation" || c.State == "confirming", "terminal": c.State == "saved" || c.State == "cancelled"}})
}
func sessionRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("POST /api/v1/capture/sessions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string `json:"text"`
			Timezone string `json:"timezone"`
		}
		if !decode(w, r, &in) {
			return
		}
		result, e := interpret.InterpretWithContextLead(r.Context(), s, in.Text, in.Timezone, "", time.Now(), 60)
		if e != nil {
			apiError(w, 400, "invalid_interpretation", e.Error(), false)
			return
		}
		c := conversation{State: "awaiting_clarification", Lead: 60, Interpretation: result, Messages: []conversationTurn{{"user", in.Text}}}
		if result.Status == "ready" {
			c.State = "awaiting_confirmation"
		}
		c.Messages = append(c.Messages, conversationTurn{"assistant", conversationPrompt(c)})
		d, e := s.CreateSession(r.Context(), c)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("Location", "/api/v1/capture/sessions/"+d.ID)
		conversationResponse(w, 201, d, c)
	})
	mux.HandleFunc("GET /api/v1/capture/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, e := s.Session(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		var c conversation
		if e = json.Unmarshal(d.Data, &c); e != nil {
			failure(w, e)
			return
		}
		conversationResponse(w, 200, d, c)
	})
	mux.HandleFunc("POST /api/v1/capture/sessions/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int    `json:"version"`
			Text    string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.Text) == "" || len([]rune(in.Text)) > 12000 {
			apiError(w, 400, "invalid_reply", "Reply must contain 1 to 12000 characters.", false)
			return
		}
		d, e := s.Session(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		var c conversation
		if e = json.Unmarshal(d.Data, &c); e != nil {
			failure(w, e)
			return
		}
		answer := strings.Trim(strings.ToLower(strings.TrimSpace(in.Text)), ".! ")
		confirm := answer == "yes" || answer == "confirm" || answer == "save it" || answer == "yes save it"
		if c.State == "saved" && confirm {
			conversationResponse(w, 200, d, c)
			return
		}
		if in.Version != d.Version {
			failure(w, store.ErrSessionConflict)
			return
		}
		if c.State == "saved" || c.State == "cancelled" {
			apiError(w, 409, "session_closed", "Start a new capture session.", false)
			return
		}
		if c.State == "confirming" && !confirm {
			apiError(w, 409, "confirmation_pending", "Retry confirm to recover the result.", false)
			return
		}
		if c.State != "confirming" {
			n, selected, lead, action, err := interpret.Reply(r.Context(), s, c.Interpretation, c.ContextTaskID, c.Lead, in.Text, time.Now())
			if err != nil {
				failure(w, err)
				return
			}
			if action == "confirm" && c.Interpretation.Status != "ready" {
				apiError(w, 409, "clarification_required", "Resolve the questions before confirming.", false)
				return
			}
			c.Interpretation = n
			c.ContextTaskID = selected
			c.Lead = lead
			c.Messages = append(c.Messages, conversationTurn{"user", in.Text})
			c.State = "awaiting_clarification"
			if n.Status == "ready" {
				c.State = "awaiting_confirmation"
			}
			if action == "confirm" {
				c.State = "confirming"
			}
			if action == "cancel" {
				c.State = "cancelled"
			}
			prompt := conversationPrompt(c)
			if c.State == "confirming" {
				prompt = "Saving the reviewed records."
			}
			if action == "unresolved" {
				prompt = "I could not resolve that reply. Choose a task by title, give a supported time, or use ‘replace:’ followed by a new sentence. " + prompt
			}
			c.Messages = append(c.Messages, conversationTurn{"assistant", prompt})
			d, e = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if e != nil {
				failure(w, e)
				return
			}
		}
		if c.State == "confirming" {
			saved, _, err := s.CommitCapture(r.Context(), "session."+d.ID, c.Interpretation.Proposal.Input)
			if err != nil {
				if errors.Is(err, store.ErrCaptureContext) {
					reference, refreshErr := time.Parse(time.RFC3339Nano, c.Interpretation.ReferenceAt)
					if refreshErr != nil {
						failure(w, refreshErr)
						return
					}
					n, refreshErr := interpret.InterpretWithContextLead(r.Context(), s, c.Interpretation.Source, c.Interpretation.Timezone, c.ContextTaskID, reference, c.Lead)
					if refreshErr != nil {
						failure(w, refreshErr)
						return
					}
					c.Interpretation = n
					c.State = "awaiting_clarification"
					if n.Status == "ready" {
						c.State = "awaiting_confirmation"
					}
					c.Messages = append(c.Messages, conversationTurn{"assistant", "The referenced task changed. Nothing was saved. Review the refreshed proposal and confirm again. " + conversationPrompt(c)})
					d, e = s.SaveSession(r.Context(), d.ID, d.Version, c)
					if e != nil {
						failure(w, e)
						return
					}
					conversationResponse(w, 200, d, c)
					return
				}
				failure(w, err)
				return
			}
			c.State = "saved"
			c.Saved = &saved
			c.Messages = append(c.Messages, conversationTurn{"assistant", conversationPrompt(c)})
			d, e = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if e == store.ErrSessionConflict {
				d, e = s.Session(r.Context(), d.ID)
				if e == nil {
					e = json.Unmarshal(d.Data, &c)
				}
			}
			if e != nil {
				failure(w, e)
				return
			}
		}
		conversationResponse(w, 200, d, c)
	})
	for path, method := range map[string]string{"/api/v1/capture/sessions": "POST", "/api/v1/capture/sessions/{id}": "GET", "/api/v1/capture/sessions/{id}/reply": "POST"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", method)
			apiError(w, 405, "method_not_allowed", "Method is not supported for this endpoint.", false)
		})
	}

}
