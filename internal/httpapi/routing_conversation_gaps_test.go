package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfflineOrchestrationCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/orchestration_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Category, Text, State, Tool, Scenario string
		Steps                                 int
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Alpha")
	b, _ := s.Create(t.Context(), "Beta")
	_, _ = s.SetRelation(t.Context(), "task", a.ID, "task", b.ID, true)
	_, _, _ = s.CreateNoteRequest(t.Context(), "", "Corpus note", "", "")
	_, _ = s.CreateReminder(t.Context(), "Ordinary reminder", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC")
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	_, _, err = s.CreateRepeatingReminderRequest(t.Context(), "corpus-repeat", "Daily dose", at.Format(time.RFC3339), "UTC", "", "daily")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessDue(t.Context(), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	send := gapSession(t, s)
	for _, tc := range cases {
		t.Run(tc.Category+"/"+tc.Text, func(t *testing.T) {
			if tc.Scenario != "" {
				runConversationCorpusScenario(t, tc.Scenario)
				return
			}
			c := startGap(t, send, tc.Text)
			if c["state"] != tc.State {
				t.Fatalf("state %v, want %s", c["state"], tc.State)
			}
			tool, _ := c["route"].(map[string]any)["selected_tool"].(string)
			if tool != tc.Tool {
				t.Fatalf("tool %q, want %q", tool, tc.Tool)
			}
			if tc.Steps > 0 && len(c["workflow"].(map[string]any)["steps"].([]any)) != tc.Steps {
				t.Fatal(c["workflow"])
			}
		})
	}
	for _, id := range []string{a.ID, b.ID} {
		state, _ := s.TaskState(t.Context(), id)
		if state.Task.Status != "open" {
			t.Fatal("corpus mutated task", state)
		}
	}
}

func runConversationCorpusScenario(t *testing.T, scenario string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Scenario alpha")
	b, _ := s.Create(t.Context(), "Scenario beta")
	send := gapSession(t, s)
	switch scenario {
	case "bulk_cancel":
		c := startGap(t, send, "Complete all tasks")
		c = replyGap(t, send, c, "cancel")
		if c["state"] != "cancelled" {
			t.Fatal(c)
		}
	case "bulk_changed":
		c := startGap(t, send, "Complete all tasks")
		changed := "Scenario beta revised"
		if _, err = s.PatchTaskState(t.Context(), b.ID, store.TaskPatch{Title: &changed}); err != nil {
			t.Fatal(err)
		}
		c = replyGap(t, send, c, "yes")
		if c["state"] != "awaiting_batch_edit_confirmation" {
			t.Fatal(c)
		}
	case "occurrence_replay":
		at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
		r, _, e := s.CreateRepeatingReminderRequest(t.Context(), "scenario-repeat", "Scenario repeat", at.Format(time.RFC3339), "UTC", "", "daily")
		if e != nil {
			t.Fatal(e)
		}
		if e = s.ProcessDue(t.Context(), at.Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
		c := startGap(t, send, "Dismiss occurrence of Scenario repeat")
		if c["state"] != "awaiting_occurrence_confirmation" {
			t.Fatal(c)
		}
		// Simulate a committed store write whose conversation response was lost.
		if _, e = s.FinishOccurrence(t.Context(), r.ID, r.OccurrenceID, "dismiss"); e != nil {
			t.Fatal(e)
		}
		c = replyGap(t, send, c, "yes")
		if c["state"] != "answered" {
			t.Fatal(c)
		}
		state, e := s.ReminderState(t.Context(), r.ID)
		if e != nil || len(state.Deliveries) != 2 {
			t.Fatal(state, e)
		}
	default:
		t.Fatalf("unknown corpus scenario %q", scenario)
	}
	for _, id := range []string{a.ID, b.ID} {
		state, e := s.TaskState(t.Context(), id)
		if e != nil || state.Task.Status != "open" {
			t.Fatal("scenario changed unconfirmed task", state, e)
		}
	}
}

func gapSession(t *testing.T, s *store.Store) func(string, any) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "gaps-test"})
	return batchTestSend(t, mux)
}

func startGap(t *testing.T, send func(string, any) (int, map[string]any), text string) map[string]any {
	t.Helper()
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": text, "text": text, "timezone": "UTC"})
	if code != 201 {
		t.Fatal(code, c)
	}
	return c
}

func replyGap(t *testing.T, send func(string, any) (int, map[string]any), c map[string]any, text string) map[string]any {
	t.Helper()
	code, next := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": c["version"], "text": text})
	if code != 200 {
		t.Fatal(code, next)
	}
	return next
}

func TestConversationBulkStatusReviewStaleAndRetry(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Bulk alpha")
	b, _ := s.Create(t.Context(), "Bulk beta")
	send := gapSession(t, s)
	c := startGap(t, send, "Complete all tasks")
	if c["state"] != "awaiting_batch_edit_confirmation" || len(c["batch_edit"].(map[string]any)["targets"].([]any)) != 2 {
		t.Fatal(c)
	}
	state, _ := s.TaskState(t.Context(), a.ID)
	if state.Task.Status != "open" {
		t.Fatal(state)
	}
	changed := "Bulk beta updated"
	if _, err = s.PatchTaskState(t.Context(), b.ID, store.TaskPatch{Title: &changed}); err != nil {
		t.Fatal(err)
	}
	c = replyGap(t, send, c, "yes")
	if c["state"] != "awaiting_batch_edit_confirmation" {
		t.Fatal(c)
	}
	state, _ = s.TaskState(t.Context(), a.ID)
	if state.Task.Status != "open" {
		t.Fatal("partial bulk change")
	}
	oldVersion := c["version"]
	c = replyGap(t, send, c, "yes")
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	for _, id := range []string{a.ID, b.ID} {
		state, _ = s.TaskState(t.Context(), id)
		if state.Task.Status != "completed" {
			t.Fatal(state)
		}
	}
	code, replay := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": oldVersion, "text": "yes"})
	if code != 200 || replay["state"] != "answered" {
		t.Fatal(code, replay)
	}
}

func TestConversationBulkPickerAndInterruption(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Choose alpha")
	b, _ := s.Create(t.Context(), "Choose beta")
	send := gapSession(t, s)
	c := startGap(t, send, "Complete tasks")
	if c["state"] != "awaiting_batch_edit_targets" {
		t.Fatal(c)
	}
	c = replyGap(t, send, c, a.ID)
	if c["state"] != "awaiting_batch_edit_confirmation" {
		t.Fatal(c)
	}
	c = replyGap(t, send, c, "cancel")
	if c["state"] != "cancelled" {
		t.Fatal(c)
	}
	for _, id := range []string{a.ID, b.ID} {
		state, _ := s.TaskState(t.Context(), id)
		if state.Task.Status != "open" {
			t.Fatal(state)
		}
	}
}

func TestConversationBulkNoteEditReviewsExactSet(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _, _ := s.CreateNoteRequest(t.Context(), "", "First body", "", "")
	b, _, _ := s.CreateNoteRequest(t.Context(), "", "Second body", "", "")
	send := gapSession(t, s)
	c := startGap(t, send, "Replace all notes with Reviewed body")
	if c["state"] != "awaiting_batch_edit_confirmation" || !strings.Contains(c["prompt"].(string), "Reviewed body") {
		t.Fatal(c)
	}
	before, _ := s.NoteState(t.Context(), a.NoteID)
	if before.Note.Body != "First body" {
		t.Fatal(before)
	}
	c = replyGap(t, send, c, "yes")
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	for _, id := range []string{a.NoteID, b.NoteID} {
		state, _ := s.NoteState(t.Context(), id)
		if state.Note.Body != "Reviewed body" {
			t.Fatal(state)
		}
	}
}

func TestCommonBulkEditPhrasesKeepReviewAndExactSelection(t *testing.T) {
	t.Run("mark all tasks done", func(t *testing.T) {
		s, err := store.Open(filepath.Join(t.TempDir(), "db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		a, _ := s.Create(t.Context(), "First task")
		b, _ := s.Create(t.Context(), "Second task")
		send := gapSession(t, s)
		c := startGap(t, send, "Mark all tasks done")
		if c["state"] != "awaiting_batch_edit_confirmation" || len(c["batch_edit"].(map[string]any)["targets"].([]any)) != 2 {
			t.Fatal(c)
		}
		for _, id := range []string{a.ID, b.ID} {
			state, _ := s.TaskState(t.Context(), id)
			if state.Task.Status != "open" {
				t.Fatal("task changed before confirmation", state)
			}
		}
		c = replyGap(t, send, c, "yes")
		if c["state"] != "answered" {
			t.Fatal(c)
		}
		for _, id := range []string{a.ID, b.ID} {
			state, _ := s.TaskState(t.Context(), id)
			if state.Task.Status != "completed" {
				t.Fatal(state)
			}
		}
	})
	t.Run("rename notes with explicit selection", func(t *testing.T) {
		s, err := store.Open(filepath.Join(t.TempDir(), "db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		a, _, _ := s.CreateNoteRequest(t.Context(), "", "First body", "", "")
		b, _, _ := s.CreateNoteRequest(t.Context(), "", "Second body", "", "")
		send := gapSession(t, s)
		c := startGap(t, send, "Rename notes to Team")
		if c["state"] != "awaiting_batch_edit_targets" || len(c["batch_edit"].(map[string]any)["candidates"].([]any)) != 2 {
			t.Fatal(c)
		}
		c = replyGap(t, send, c, a.NoteID)
		if c["state"] != "awaiting_batch_edit_confirmation" || len(c["batch_edit"].(map[string]any)["targets"].([]any)) != 1 {
			t.Fatal(c)
		}
		before, _ := s.NoteState(t.Context(), a.NoteID)
		if before.Note.Title == "Team" {
			t.Fatal("note changed before confirmation", before)
		}
		c = replyGap(t, send, c, "yes")
		if c["state"] != "answered" {
			t.Fatal(c)
		}
		selected, _ := s.NoteState(t.Context(), a.NoteID)
		untouched, _ := s.NoteState(t.Context(), b.NoteID)
		if selected.Note.Title != "Team" || untouched.Note.Title == "Team" {
			t.Fatal(selected, untouched)
		}
	})
}

func TestConversationRelationListingExactAndAmbiguous(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create(t.Context(), "Relation anchor")
	b, _ := s.Create(t.Context(), "Relation other")
	if _, err = s.SetRelation(t.Context(), "task", a.ID, "task", b.ID, true); err != nil {
		t.Fatal(err)
	}
	send := gapSession(t, s)
	c := startGap(t, send, "Show relations for Relation anchor")
	if c["state"] != "answered" || !strings.Contains(c["answer"].(map[string]any)["text"].(string), "Relation other") {
		t.Fatal(c)
	}
	c = startGap(t, send, "List relations for a task")
	if c["state"] != "awaiting_relation_target" {
		t.Fatal(c)
	}
	c = replyGap(t, send, c, a.ID)
	if c["state"] != "answered" || !strings.Contains(c["answer"].(map[string]any)["text"].(string), "Relation other") {
		t.Fatal(c)
	}
}

func TestConversationRepeatingOccurrenceReviewAndRetry(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	r, _, err := s.CreateRepeatingReminderRequest(t.Context(), "gap-repeat", "Repeat dose", at.Format(time.RFC3339), "UTC", "", "daily")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessDue(t.Context(), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	send := gapSession(t, s)
	c := startGap(t, send, "Dismiss occurrence of Repeat dose")
	if c["state"] != "awaiting_occurrence_confirmation" {
		t.Fatal(c)
	}
	before, _ := s.ReminderState(t.Context(), r.ID)
	if before.Reminder.Status != "due" {
		t.Fatal(before)
	}
	oldVersion := c["version"]
	c = replyGap(t, send, c, "yes")
	if c["state"] != "answered" {
		t.Fatal(c)
	}
	after, _ := s.ReminderState(t.Context(), r.ID)
	if after.Reminder.Status != "scheduled" || after.Reminder.OccurrenceID == r.OccurrenceID || len(after.Deliveries) != 2 {
		t.Fatal(after)
	}
	code, replay := send("/api/v1/conversations/routing/"+c["id"].(string)+"/reply", map[string]any{"version": oldVersion, "text": "yes"})
	if code != 200 || replay["state"] != "answered" {
		t.Fatal(code, replay)
	}
	after, _ = s.ReminderState(t.Context(), r.ID)
	if len(after.Deliveries) != 2 {
		t.Fatal("advanced twice")
	}
}

func TestConversationGapPagesServed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	for path, marker := range map[string]string{"/batch-edit-test": "Reviewed bulk edit test", "/relation-list-test": "Relation listing test", "/occurrence-test": "Repeating occurrence test"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), marker) {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/conversation-gap-test.js", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "awaiting_occurrence_confirmation") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/conversation-gap-test.css", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal(w.Code, w.Header().Get("Content-Type"))
	}
}
