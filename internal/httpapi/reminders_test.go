package httpapi

import (
	"atlas/internal/store"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReminderAPI(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	for _, body := range []string{`{}`, `null`, `{"title":"x","scheduled_at":"tomorrow","timezone":"UTC"}`, `{"title":"x","scheduled_at":"2026-10-01T12:00:00Z","timezone":"bad"}`, `{"title":"x","scheduled_at":"2026-10-01T12:00:00Z","timezone":"UTC","extra":true}`} {
		if w := request("POST", "/api/v1/reminders", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request("POST", "/api/v1/reminders", `{"title":"Test reminder","scheduled_at":"2026-01-01T12:00:00Z","timezone":"UTC"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r store.Reminder
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessDue(context.Background(), time.Now()); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/reminders", "/api/v1/deliveries", "/api/v1/activity"} {
		if w = request("GET", path, ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	url := "/api/v1/reminders/" + r.ID
	if w = request("POST", url+"/snooze", `{"scheduled_at":"2020-01-01T12:00:00Z"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = request("POST", url+"/dismiss", ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w = request("POST", url+"/complete", ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	if w = request("POST", url+"/snooze", `{"scheduled_at":"`+future+`"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w = request("POST", "/api/v1/reminders/missing/complete", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
