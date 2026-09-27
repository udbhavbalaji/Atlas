package provider

import (
	"atlas/internal/store"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fixture struct{ response Response }

func (f fixture) Name() string                                       { return "test" }
func (f fixture) Propose(context.Context, Request) (Response, error) { return f.response, nil }
func request() Request {
	return Request{Version: Version, RequestID: "test", Text: "Demo task", Timezone: "UTC", ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), Scenario: "task"}
}
func TestMockEnrichmentValidationAndAtomicSave(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := request()
	out, e := Run(t.Context(), s, Mock{}, r)
	if e != nil || out.State != "awaiting_clarification" || len(out.Response.Questions) != 2 || out.Proposal != nil {
		t.Fatal(out, e)
	}
	r.Answers = Answers{Reminder: "skip", Note: "skip"}
	out, e = Run(t.Context(), s, Mock{}, r)
	if e != nil || out.State != "awaiting_confirmation" || out.Proposal.Input.ReminderAt != "" || out.Proposal.Input.NoteBody != "" {
		t.Fatal(out, e)
	}
	r.Answers = Answers{Reminder: "add", ReminderAt: "2090-01-01T09:00:00Z", Note: "add", NoteBody: "Research note"}
	out, e = Run(t.Context(), s, Mock{}, r)
	if e != nil || out.State != "awaiting_confirmation" || len(out.Response.Questions) != 0 {
		t.Fatal(out, e)
	}
	tasks, _ := s.Tasks(t.Context())
	notes, _ := s.Notes(t.Context())
	if len(tasks) != 0 || len(notes) != 0 || out.Persisted {
		t.Fatal(tasks, notes, out)
	}
	saved, _, e := s.CommitCapture(t.Context(), "provider-demo", out.Proposal.Input)
	if e != nil || len(saved.Notes) != 1 || len(saved.Reminders) != 1 || saved.Reminders[0].TaskID != saved.TaskID {
		t.Fatal(saved, e)
	}
	replay, duplicate, e := s.CommitCapture(t.Context(), "provider-demo", out.Proposal.Input)
	if e != nil || !duplicate || replay.TaskID != saved.TaskID {
		t.Fatal(replay, e)
	}
	r.Answers.ReminderAt = "bad"
	out, e = Run(t.Context(), s, Mock{}, r)
	if e == nil || out.Proposal != nil {
		t.Fatal(out, e)
	}
}
func TestBoundedContextAndUnseenReferenceRejection(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	selected, _ := s.CreateWithFields(t.Context(), "Interview at Ather", "PRIVATE DETAILS", "2090-01-01T09:00:00Z")
	for i := 0; i < 6; i++ {
		s.Create(t.Context(), "Interview")
	}
	r := request()
	r.Scenario = "context"
	r.ContextQuery = "Interview"
	r.Answers = Answers{Reminder: "skip", Note: "skip"}
	out, e := Run(t.Context(), s, Mock{}, r)
	if e != nil || len(out.Trace) != 2 || len(out.Trace[1].Request.Context[0].Records) != 5 || !out.Trace[1].Request.Context[0].Truncated {
		t.Fatal(out, e)
	}
	r.ContextQuery = "Ather"
	r.Answers.ContextTaskID = selected.ID
	out, e = Run(t.Context(), s, Mock{}, r)
	if e != nil || out.Proposal == nil || out.Proposal.Input.BeforeTaskVersion == "" {
		t.Fatal(out, e)
	}
	fake := Response{Version: Version, Status: "ready", Draft: &store.CaptureInput{Kind: "task", Title: "Bad", BeforeTaskID: selected.ID}}
	out, e = Run(t.Context(), s, fixture{fake}, request())
	if !errors.Is(e, ErrContract) || out.Proposal != nil {
		t.Fatal(out, e)
	}
	for _, limit := range []int{0, 6, 100} {
		fake = Response{Version: Version, Status: "needs_context", ContextRequests: []ContextRequest{{"Interview", limit}}}
		_, e = Run(t.Context(), s, fixture{fake}, request())
		if !errors.Is(e, ErrRequest) {
			t.Fatal(limit, e)
		}
	}
	fake = Response{Version: Version, Status: "needs_context", ContextRequests: []ContextRequest{{"Interview", 1}}}
	_, e = Run(t.Context(), s, fixture{fake}, request())
	if !errors.Is(e, ErrRequest) {
		t.Fatal("loop not bounded", e)
	}
}
func TestInvalidAndUnavailableNeverPersist(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, scenario := range []string{"invalid", "unavailable"} {
		r := request()
		r.Scenario = scenario
		out, e := Run(t.Context(), s, Mock{}, r)
		if e == nil || out.Proposal != nil {
			t.Fatal(out, e)
		}
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 0 {
		t.Fatal(tasks)
	}
	invalid := []Response{{Version: "2", Status: "ready", Draft: &store.CaptureInput{Title: "A"}}, {Version: Version, Status: "ready", Draft: &store.CaptureInput{Title: "A", PreviewID: "forged"}}, {Version: Version, Status: "saved"}, {Version: Version, Status: "needs_clarification", Questions: []Question{{ID: "bad", Field: "execute", Prompt: "Bad"}}}}
	for _, response := range invalid {
		if ValidateResponse(response) == nil {
			t.Fatal(response)
		}
	}
}
