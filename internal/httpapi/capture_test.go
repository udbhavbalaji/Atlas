package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureAPIContract(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	send := func(path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/capture/"+path, bytes.NewBufferString(body))
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("preview", `{"title":"Task","reminder_at":"2030-01-01T00:00:00Z","timezone":"UTC"}`, "")
	var p store.CaptureProposal
	if e = json.Unmarshal(w.Body.Bytes(), &p); e != nil || w.Code != 200 || p.Input.PreviewID == "" {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	b, _ := json.Marshal(p.Input)
	w = send("commit", string(b), "")
	assertErrorCode(t, w, 400, "capture_key_required")
	w = send("commit", string(b), "key")
	if w.Code != 201 || w.Header().Get("Location") == "" || w.Header().Get("Idempotency-Replayed") != "false" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send("commit", string(b), "key")
	if w.Code != 200 || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w.Code, w.Body.String())
	}
	p.Input.Title = "Changed"
	b, _ = json.Marshal(p.Input)
	w = send("commit", string(b), "new-key")
	assertErrorCode(t, w, 409, "capture_preview_conflict")
	for _, body := range []string{`{"title":"A","title":"B"}`, `{"title":null}`, `{"title":"A","unknown":true}`} {
		if w = send("preview", body, ""); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/capture/commit", nil))
	assertErrorCode(t, w, 405, "method_not_allowed")
}

func TestSentenceToAtomicRecords(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	request := func(path string, value any, key string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(value)
		r := httptest.NewRequest("POST", "/api/v1/capture/"+path, bytes.NewReader(b))
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct {
		text                           string
		tasks, reminders, notes, links int
		location                       string
	}{
		{"Call Mom tomorrow at 6pm; note: ask about the trip", 1, 1, 1, 2, "/api/v1/tasks/"},
		{"Remind me to drink water in 10 minutes; note: use the blue bottle", 0, 1, 1, 1, "/api/v1/reminders/"},
		{"Remember that the gate code is 1234", 0, 0, 1, 0, "/api/v1/notes/"},
	} {
		w := request("interpret", map[string]string{"text": c.text, "timezone": "UTC"}, "")
		var result struct {
			Status   string                 `json:"status"`
			Proposal *store.CaptureProposal `json:"proposal"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 || result.Status != "ready" || result.Proposal == nil {
			t.Fatal(w.Body.String(), e)
		}
		w = request("commit", result.Proposal.Input, "sentence-"+c.location[8:len(c.location)-1])
		var state store.TaskAction
		if e = json.Unmarshal(w.Body.Bytes(), &state); e != nil || w.Code != 201 || !strings.HasPrefix(w.Header().Get("Location"), c.location) || len(state.Reminders) != c.reminders || len(state.Notes) != c.notes || len(state.Notes[0].Links) != c.links || (state.Task != nil) != (c.tasks == 1) {
			t.Fatal(w.Code, w.Body.String(), e)
		}
		replay := request("commit", result.Proposal.Input, "sentence-"+c.location[8:len(c.location)-1])
		if replay.Code != 200 || replay.Header().Get("Idempotency-Replayed") != "true" {
			t.Fatal(replay.Code, replay.Body.String())
		}
	}
	w := request("interpret", map[string]string{"text": "Call Mom tomorrow at 6", "timezone": "UTC"}, "")
	var ambiguous struct {
		Status   string                 `json:"status"`
		Proposal *store.CaptureProposal `json:"proposal"`
		Draft    store.CaptureInput     `json:"draft"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &ambiguous); e != nil || w.Code != 200 || ambiguous.Proposal != nil || ambiguous.Status != "needs_clarification" {
		t.Fatal(w.Body.String(), e)
	}
	w = request("commit", ambiguous.Draft, "ambiguous")
	if w.Code < 400 {
		t.Fatal("unresolved draft committed", w.Body.String())
	}
	w = request("interpret", map[string]string{"text": "Buy milk", "timezone": "unknown"}, "")
	assertErrorCode(t, w, 400, "invalid_interpretation")
}
