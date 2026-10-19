package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

type reportRequest struct {
	Version      int    `json:"version"`
	RequestID    string `json:"request_id"`
	Surface      string `json:"surface"`
	Category     string `json:"category"`
	Expected     string `json:"expected"`
	MessageIndex *int   `json:"message_index"`
}

func validReportRequest(input reportRequest) bool {
	if input.Version < 1 || len(input.RequestID) < 8 || len(input.RequestID) > 128 {
		return false
	}
	for _, r := range input.RequestID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	if !map[string]bool{"web": true, "macos": true, "omarchy": true, "ios": true}[input.Surface] {
		return false
	}
	if !map[string]bool{"wrong_answer": true, "unwanted_change": true, "missed_action": true, "confusing_flow": true, "other": true}[input.Category] {
		return false
	}
	n := utf8.RuneCountInString(strings.TrimSpace(input.Expected))
	return n >= 3 && n <= 2000
}

func reportRoutes(mux *http.ServeMux, s *store.Store) {
	mux.HandleFunc("POST /api/v1/conversations/routing/{id}/reports", func(w http.ResponseWriter, r *http.Request) {
		var input reportRequest
		if !decode(w, r, &input) {
			return
		}
		if !validReportRequest(input) {
			failure(w, store.ErrReportInvalid)
			return
		}
		d, c, err := loadRoutingConversation(r, s)
		if err != nil {
			failure(w, err)
			return
		}
		if c.Route.Provider == "" || len(c.Messages) == 0 {
			failure(w, store.ErrReportInvalid)
			return
		}
		index := -1
		if input.MessageIndex != nil {
			index = *input.MessageIndex
		}
		if index < -1 || index >= len(c.Messages) {
			failure(w, store.ErrReportInvalid)
			return
		}
		snapshot, err := json.Marshal(map[string]any{
			"state": c.State, "prompt": c.prompt(), "messages": c.Messages,
			"provider": c.Route.Provider, "route_category": c.Route.Category,
			"selected_tool": c.Route.SelectedTool, "selected_channel": c.Route.SelectedChannel,
			"extraction_model": c.Route.ExtractionModel, "workflow": c.Workflow,
			"warnings": c.Warnings, "answer": c.Answer, "mutation": c.Mutation,
			"batch": c.Batch, "batch_edit": c.BatchEdit, "association": c.Association,
			"relation_list": c.RelationList, "occurrence": c.Occurrence,
		})
		if err != nil {
			failure(w, err)
			return
		}
		report, replay, err := s.CreateConversationReport(r.Context(), store.ConversationReportInput{
			RequestID: input.RequestID, SessionID: d.ID, SessionVersion: input.Version,
			Surface: input.Surface, Category: input.Category, Expected: strings.TrimSpace(input.Expected),
			MessageIndex: index, Snapshot: snapshot,
		})
		if err != nil {
			failure(w, err)
			return
		}
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		respond(w, status, report)
	})
	mux.HandleFunc("GET /api/v1/reports", func(w http.ResponseWriter, r *http.Request) {
		reports, err := s.ConversationReports(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, http.StatusOK, reports)
	})
	mux.HandleFunc("GET /api/v1/reports/corpus.jsonl", func(w http.ResponseWriter, r *http.Request) {
		reports, err := s.ConversationReports(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="atlas-report-candidates.jsonl"`)
		encoder := json.NewEncoder(w)
		for _, report := range reports {
			var snapshot struct {
				State    string             `json:"state"`
				Provider string             `json:"provider"`
				Messages []conversationTurn `json:"messages"`
			}
			if err := json.Unmarshal(report.Snapshot, &snapshot); err != nil {
				return
			}
			requestText := ""
			for _, turn := range snapshot.Messages {
				if turn.Role == "user" {
					requestText = turn.Text
					break
				}
			}
			if err := encoder.Encode(map[string]any{
				"candidate_id": report.ID, "request_text": requestText,
				"expected_behavior": report.Expected, "observed_state": snapshot.State,
				"category": report.Category, "surface": report.Surface,
				"provider": snapshot.Provider, "message_index": report.MessageIndex,
				"messages": snapshot.Messages, "reported_at": report.CreatedAt,
			}); err != nil {
				return
			}
		}
	})
}
