package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRoutingConversationQuestioningAndConfirmation(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	send := func(path string, body any, method string) (int, map[string]any) {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(payload)))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err, w.Body.String())
		}
		return w.Code, out
	}
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "voice-1", "text": "Call Maya", "timezone": "UTC", "fixture": "task"}, "POST")
	if code != 201 || c["state"] != "awaiting_answer" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	reply := func(text, field, value string) map[string]any {
		code, next := send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": text, "field": field, "value": value}, "POST")
		if code != 200 {
			t.Fatal(code, next)
		}
		c = next
		return c
	}
	// The local parser may prepare the title, but optional decisions stay explicit.
	for turn := 0; c["state"] == "awaiting_answer" && turn < 5; turn++ {
		q := c["question"].(map[string]any)
		switch q["field"] {
		case "fields.title":
			reply("Call Maya", "", "")
		case "reminder", "note":
			reply("skip", "", "")
		default:
			t.Fatal(q)
		}
	}
	if c["state"] != "awaiting_confirmation" || c["proposal"] == nil {
		t.Fatal(c)
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 0 {
		t.Fatal("preview persisted records")
	}
	before := c["version"]
	code, stale := send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": before.(float64) - 1, "text": "yes"}, "POST")
	if code != 409 || stale["error"] == nil {
		t.Fatal(code, stale)
	}
	reply("change title to Call Maya tonight", "", "")
	if c["state"] != "awaiting_confirmation" || c["proposal"].(map[string]any)["input"].(map[string]any)["title"] != "Call Maya tonight" {
		t.Fatal(c)
	}
	version := c["version"]
	reply("yes", "", "")
	if c["state"] != "saved" || c["saved"] == nil {
		t.Fatal(c)
	}
	code, replay := send("/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": version, "text": "yes"}, "POST")
	if code != 200 || replay["state"] != "saved" {
		t.Fatal(code, replay)
	}
	tasks, _ = s.Tasks(t.Context())
	if len(tasks) != 1 || tasks[0].Title != "Call Maya tonight" {
		t.Fatal(tasks)
	}
}

func TestRoutingConversationAmbiguityTimeAndCancel(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	send := func(method, path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(b)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, c := send("POST", "/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "ambiguous", "text": "Remind me to call Maya tomorrow at 6pm", "timezone": "UTC", "fixture": "ambiguous"})
	if code != 201 || c["state"] != "awaiting_route" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	reply := func(text, field, value string) {
		code, n := send("POST", "/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": text, "field": field, "value": value})
		if code != 200 {
			t.Fatal(code, n)
		}
		c = n
	}
	reply("reminder", "", "")
	if c["channel"] != "reminders" {
		t.Fatal(c)
	}
	for turn := 0; c["state"] == "awaiting_answer" && turn < 5; turn++ {
		q := c["question"].(map[string]any)
		switch q["field"] {
		case "fields.title":
			reply("Call Maya", "", "")
		case "fields.reminder_at":
			reply("", "fields.reminder_at", "tomorrow at 6pm")
		case "note":
			reply("no", "", "")
		default:
			t.Fatal(q)
		}
	}
	if c["state"] != "awaiting_confirmation" {
		t.Fatal(c)
	}
	reply("cancel", "", "")
	if c["state"] != "cancelled" {
		t.Fatal(c)
	}
	reminders, _ := s.Reminders(t.Context())
	if len(reminders) != 0 {
		t.Fatal(reminders)
	}
}

func TestRoutingConversationSpokenTimeAndWrongField(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	send := func(method, path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(b)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, c := send("POST", "/api/v1/conversations/routing", map[string]any{"provider": "mock", "version": "1", "request_id": "time", "text": "Remind me to call Maya", "timezone": "UTC", "fixture": "reminder"})
	if code != 201 {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	for turn := 0; c["state"] == "awaiting_answer" && turn < 5; turn++ {
		q := c["question"].(map[string]any)
		field := q["field"].(string)
		if field == "fields.reminder_at" {
			version := c["version"]
			code, c = send("POST", "/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": version, "field": "fields.title", "value": "tomorrow at 6pm"})
			if code != 200 || c["question"].(map[string]any)["field"] != "fields.reminder_at" {
				t.Fatal(code, c)
			}
		}
		value := "Call Maya"
		switch field {
		case "fields.reminder_at":
			value = "tomorrow at 6pm"
		case "note":
			value = "no"
		case "fields.title":
		default:
			t.Fatal(field)
		}
		code, c = send("POST", "/api/v1/conversations/routing/"+id+"/reply", map[string]any{"version": c["version"], "text": value})
		if code != 200 {
			t.Fatal(code, c)
		}
	}
	if c["state"] != "awaiting_confirmation" {
		t.Fatal(c)
	}
	input := c["proposal"].(map[string]any)["input"].(map[string]any)
	if input["reminder_at"] == "" {
		t.Fatal(input)
	}
}

func TestRoutingConversationResumesAcrossServerRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(s)
	b, _ := json.Marshal(map[string]any{"provider": "mock", "version": "1", "request_id": "restart", "text": "Note that the gate code is 1234", "timezone": "UTC", "fixture": "note"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/conversations/routing", bytes.NewReader(b)))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var start map[string]any
	json.Unmarshal(w.Body.Bytes(), &start)
	id := start["id"].(string)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h = Handler(s)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/conversations/routing/"+id, nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var resumed map[string]any
	json.Unmarshal(w.Body.Bytes(), &resumed)
	if resumed["id"] != id || resumed["state"] != "awaiting_confirmation" {
		t.Fatal(resumed)
	}
}
