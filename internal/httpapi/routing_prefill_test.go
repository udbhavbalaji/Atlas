package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestChannelPreparationCarriesOriginalInputAndBoundedContext(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	interview, err := s.CreateWithFields(t.Context(), "Interview at Ather", "Private details must not enter the context pack", "2030-01-08T15:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	input := routing.State{Text: "I have an interview at Ather on Tuesday at 3pm; note: bring portfolio", Timezone: "UTC", ReferenceAt: "2030-01-07T10:00:00Z", Context: []provider.ContextRecord{{ID: interview.ID, Title: interview.Title, DueAt: interview.DueAt, UpdatedAt: interview.UpdatedAt}}}
	evaluation, _ := (routing.Mock{}).Evaluate(t.Context(), input, routing.Registry(), "task")
	route, err := routing.Decide(routing.Request{Version: "1", RequestID: "prefill"}, input, evaluation, "mock", true)
	if err != nil {
		t.Fatal(err)
	}
	service := routingService{secret: "test-secret"}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, service)
	body, _ := json.Marshal(dispatchRequest{Version: "1", RoutingToken: service.sign(route), Prefill: true})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/routing/dispatch", bytes.NewReader(body)))
	var response channelResponse
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if response.Input.Text != input.Text || response.Input.Timezone != "UTC" || len(response.Input.Context) != 1 || response.Input.Context[0].ID != interview.ID {
		t.Fatal(response.Input)
	}
	if response.State != "needs_fields" || response.Proposal != nil || response.Persisted || response.Extraction != "local_prefill_review" {
		t.Fatal(response)
	}
	seed := response.Prefill
	if seed == nil || seed.Fields["title"] != "interview at Ather" || seed.Fields["due_at"] != "2030-01-08T15:00:00.000000000Z" || seed.Fields["reminder_at"] != "2030-01-08T15:00:00.000000000Z" || seed.Fields["note_body"] != "bring portfolio" || seed.Reminder != "add" || seed.Note != "add" {
		t.Fatal(seed)
	}
	if seed.Fields["before_task_id"] != "" || len(response.Questions) != 0 {
		t.Fatal("Context was implicitly linked or source fields were asked again", response)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal("preparation wrote a task", tasks)
	}
}

func TestPrefillPreservesUnresolvedTimesAndExplicitEdits(t *testing.T) {
	state := routing.State{Text: "I have an interview at Ather on Tuesday", Timezone: "UTC", ReferenceAt: "2030-01-07T10:00:00Z", Context: []provider.ContextRecord{{ID: "unselected", Title: "Other interview", DueAt: "2030-01-09T09:00:00Z"}}}
	seed := prepareChannel(state, "task")
	if seed.Fields["title"] != "interview at Ather" || seed.Fields["due_at"] != "" || seed.Fields["reminder_at"] != "" || len(seed.Warnings) == 0 {
		t.Fatal(seed)
	}
	state.Text = "I have an interview at Ather on Tuesday at 3pm; note: bring portfolio"
	seed = prepareChannel(state, "task")
	in := dispatchRequest{Reminder: "skip", Note: "skip", Fields: store.CaptureInput{Title: "My edited title", DueAt: "2030-01-10T11:00:00Z"}}
	prepared := applyChannelPrefill(in, seed)
	if prepared.Fields.Title != in.Fields.Title || prepared.Fields.DueAt != in.Fields.DueAt || prepared.Fields.ReminderAt != "" || prepared.Fields.NoteBody != "" || prepared.Reminder != "skip" || prepared.Note != "skip" {
		t.Fatal(prepared)
	}
	state.Text = "Remind me to drink water every day at 9am"
	seed = prepareChannel(state, "reminder")
	if seed.Fields["title"] != "drink water" || seed.Fields["reminder_at"] == "" || seed.Fields["repeat"] != "daily" || seed.Fields["due_at"] != "" {
		t.Fatal(seed)
	}
	state.Text = "Remember that the gate code is 1234"
	seed = prepareChannel(state, "note")
	if seed.Fields["note_body"] != "the gate code is 1234" || len(seed.Fields) != 1 {
		t.Fatal(seed)
	}
	state.Text = "An observation outside the local grammar"
	seed = prepareChannel(state, "note")
	if seed.Fields["note_body"] != state.Text {
		t.Fatal(seed)
	}
}
