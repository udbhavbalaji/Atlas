package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestProviderAPIContractAndFixtures(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w
	}
	w := send("GET", "/api/v1/providers", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, test := range []struct {
		body   string
		status int
	}{{`{"version":"1","request_id":"x","text":"Demo","timezone":"UTC","scenario":"task","answers":{"reminder":"skip","note":"skip"}}`, 200}, {`{"version":"1","request_id":"x","text":"Demo","timezone":"UTC","scenario":"task"}`, 200}, {`{"version":"1","request_id":"x","text":"Demo","timezone":"UTC","scenario":"invalid"}`, 422}, {`{"version":"1","request_id":"x","text":"Demo","timezone":"UTC","scenario":"unavailable"}`, 503}, {`{"version":"2","request_id":"x","text":"Demo","timezone":"UTC","scenario":"task"}`, 400}, {`{"unknown":true}`, 400}, {`{"version":"1","version":"2"}`, 400}} {
		w = send("POST", "/api/v1/providers/mock/preview", test.body)
		if w.Code != test.status {
			t.Fatal(w.Code, w.Body.String())
		}
		var value map[string]any
		if json.Unmarshal(w.Body.Bytes(), &value) != nil {
			t.Fatal(w.Body.String())
		}
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 0 {
		t.Fatal(tasks)
	}
	w = send("GET", "/api/v1/providers/mock/preview", "")
	assertErrorCode(t, w, 405, "method_not_allowed")
	w = send("GET", "/providers.js", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
