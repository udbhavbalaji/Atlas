package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestConversationEditsExistingReminderRepeat(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.CreateReminder(t.Context(), "Repeat review", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "test"})
	send := batchTestSend(t, mux)
	for _, tc := range []struct{ text, want string }{
		{"Change reminder Repeat review repeat to weekly", "weekly"},
		{"Change reminder Repeat review repeat to none", ""},
	} {
		code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "edit", "version": "1", "request_id": tc.text, "text": tc.text, "timezone": "UTC"})
		if code != 201 || c["state"] != "answered" {
			t.Fatal(code, c)
		}
		state, err := s.ReminderState(t.Context(), r.ID)
		if err != nil || state.Reminder.Repeat != tc.want {
			t.Fatal(state, err)
		}
	}
}

func TestReminderRepeatPatchRejectsMixedChanges(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.CreateReminder(t.Context(), "Patch repeat", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	mux := Handler(s)
	request := func(body any) int {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/v1/reminders/"+r.ID, bytes.NewReader(data)))
		return w.Code
	}
	if code := request(map[string]any{"repeat": "daily", "task_id": ""}); code != 400 {
		t.Fatalf("mixed patch returned %d", code)
	}
	if code := request(map[string]any{"repeat": "daily"}); code != 200 {
		t.Fatalf("repeat patch returned %d", code)
	}
	state, err := s.ReminderState(t.Context(), r.ID)
	if err != nil || state.Reminder.Repeat != "daily" {
		t.Fatal(state, err)
	}
}
