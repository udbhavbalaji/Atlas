// Package provider defines Atlas's internal reasoning boundary. It is not a
// claim about Jev's transport or API; adapters must translate into this contract.
package provider

import (
	"atlas/internal/store"
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "1"

var ErrContract = errors.New("provider returned an invalid structured response")
var ErrUnavailable = errors.New("reasoning provider is unavailable")
var ErrRequest = errors.New("invalid provider request or context budget")

type ContextRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}
type ContextRecord struct {
	ID        string `json:"id"`
	Kind      string `json:"kind,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	Status    string `json:"status,omitempty"`
	DueAt     string `json:"due_at"`
	UpdatedAt string `json:"updated_at"`
}
type ContextResult struct {
	Request   ContextRequest  `json:"request"`
	Records   []ContextRecord `json:"records"`
	Truncated bool            `json:"truncated"`
}
type Answers struct {
	ContextTaskID string `json:"context_task_id"`
	Reminder      string `json:"reminder"`
	ReminderAt    string `json:"reminder_at"`
	Note          string `json:"note"`
	NoteBody      string `json:"note_body"`
}
type Request struct {
	Version     string          `json:"version"`
	RequestID   string          `json:"request_id"`
	Text        string          `json:"text"`
	Timezone    string          `json:"timezone"`
	ReferenceAt string          `json:"reference_at"`
	Answers     Answers         `json:"answers"`
	Context     []ContextResult `json:"context"`
	// Mock controls are never part of a future Jev transport contract.
	Scenario     string `json:"mock_scenario"`
	ContextQuery string `json:"mock_context_query"`
}
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type Question struct {
	ID       string   `json:"id"`
	Field    string   `json:"field"`
	Prompt   string   `json:"prompt"`
	Required bool     `json:"required"`
	Choices  []Choice `json:"choices"`
}
type Response struct {
	Version         string              `json:"version"`
	Status          string              `json:"status"`
	Draft           *store.CaptureInput `json:"draft"`
	Questions       []Question          `json:"questions"`
	ContextRequests []ContextRequest    `json:"context_requests"`
	Explanation     string              `json:"explanation"`
}
type Provider interface {
	Name() string
	Propose(context.Context, Request) (Response, error)
}
type Exchange struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}
type Result struct {
	Provider  string                 `json:"provider"`
	Mock      bool                   `json:"mock"`
	State     string                 `json:"state"`
	Persisted bool                   `json:"persisted"`
	Trace     []Exchange             `json:"trace"`
	Response  Response               `json:"response"`
	Proposal  *store.CaptureProposal `json:"proposal"`
}

func ValidateResponse(r Response) error {
	if r.Version != Version || len(r.Explanation) > 4000 || len(r.Questions) > 8 || len(r.ContextRequests) > 1 {
		return ErrContract
	}
	ids := map[string]bool{}
	for _, q := range r.Questions {
		if ids[q.ID] {
			return ErrContract
		}
		ids[q.ID] = true
		if q.ID == "" || len(q.ID) > 100 || q.Prompt == "" || len(q.Prompt) > 1000 || len(q.Choices) > 5 {
			return ErrContract
		}
		if q.Field != "context_task_id" && q.Field != "reminder" && q.Field != "reminder_at" && q.Field != "note" && q.Field != "note_body" {
			return ErrContract
		}
		values := map[string]bool{}
		for _, c := range q.Choices {
			if values[c.Value] {
				return ErrContract
			}
			values[c.Value] = true
			if c.Value == "" || c.Label == "" || len(c.Value) > 100 || len(c.Label) > 600 {
				return ErrContract
			}
		}
	}
	switch r.Status {
	case "needs_context":
		if len(r.ContextRequests) != 1 || r.Draft != nil || len(r.Questions) != 0 {
			return ErrContract
		}
	case "needs_clarification":
		if len(r.Questions) == 0 || len(r.ContextRequests) != 0 {
			return ErrContract
		}
	case "ready":
		if r.Draft == nil || len(r.Questions) != 0 || len(r.ContextRequests) != 0 || r.Draft.PreviewID != "" || r.Draft.BeforeTaskVersion != "" {
			return ErrContract
		}
	default:
		return ErrContract
	}
	return nil
}
func Run(ctx context.Context, s *store.Store, p Provider, req Request) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := Result{Provider: p.Name(), Mock: p.Name() == "mock-fixtures", State: "rejected", Trace: []Exchange{}}
	if req.Version != Version || req.RequestID == "" || len(req.RequestID) > 128 || !utf8.ValidString(req.Text) || strings.TrimSpace(req.Text) == "" || len([]rune(req.Text)) > 12000 || req.Timezone == "" || req.Timezone == "Local" || len(req.Context) > 0 {
		return out, ErrRequest
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return out, ErrRequest
	}
	if _, err := time.Parse(time.RFC3339Nano, req.ReferenceAt); err != nil {
		return out, ErrRequest
	}
	req.Context = []ContextResult{}
	for round := 0; round < 2; round++ {
		response, err := p.Propose(ctx, req)
		if response.Questions == nil {
			response.Questions = []Question{}
		}
		if response.ContextRequests == nil {
			response.ContextRequests = []ContextRequest{}
		}
		for i := range response.Questions {
			if response.Questions[i].Choices == nil {
				response.Questions[i].Choices = []Choice{}
			}
		}
		out.Trace = append(out.Trace, Exchange{req, response})
		if err != nil {
			if ctx.Err() != nil {
				out.State = "unavailable"
				return out, errors.Join(ErrUnavailable, ctx.Err())
			}
			if errors.Is(err, ErrUnavailable) {
				out.State = "unavailable"
			}
			return out, err
		}
		out.Response = response
		if err = ValidateResponse(response); err != nil {
			return out, err
		}
		if response.Status == "needs_context" {
			if round != 0 {
				return out, ErrRequest
			}
			query := response.ContextRequests[0]
			if query.Limit < 1 || query.Limit > 5 || strings.TrimSpace(query.Query) == "" || len([]rune(query.Query)) > 200 {
				return out, ErrRequest
			}
			found, err := s.Search(ctx, store.SearchOptions{Query: query.Query, Type: "task", Status: "open", Limit: query.Limit})
			if err != nil {
				return out, err
			}
			pack := ContextResult{Request: query, Records: []ContextRecord{}, Truncated: found.HasMore}
			for _, hit := range found.Results {
				snapshot, err := s.TaskState(ctx, hit.ID)
				if err != nil {
					return out, err
				}
				task := snapshot.Task
				if task.Status != "open" {
					continue
				}
				pack.Records = append(pack.Records, ContextRecord{ID: task.ID, Title: task.Title, DueAt: task.DueAt, UpdatedAt: task.UpdatedAt})
			}
			req.Context = []ContextResult{pack}
			continue
		}
		if response.Status == "needs_clarification" {
			out.State = "awaiting_clarification"
			return out, nil
		}
		if response.Draft.BeforeTaskID != "" {
			allowed := false
			for _, pack := range req.Context {
				for _, record := range pack.Records {
					if record.ID == response.Draft.BeforeTaskID {
						allowed = true
					}
				}
			}
			if !allowed {
				return out, ErrContract
			}
		}
		proposal, err := s.CapturePreview(ctx, *response.Draft)
		if err != nil {
			return out, err
		}
		out.Proposal = &proposal
		out.State = "awaiting_confirmation"
		return out, nil
	}
	return out, ErrRequest
}
