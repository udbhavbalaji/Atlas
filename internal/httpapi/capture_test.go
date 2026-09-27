package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
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
