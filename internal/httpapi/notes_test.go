package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotesHTTPContract(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := Handler(s)
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := request("POST", "/api/v1/notes", `{"body":"Context"}`, "capture")
	var n store.NoteAction
	if e = json.Unmarshal(first.Body.Bytes(), &n); e != nil || first.Code != 201 || n.Note == nil || n.Note.Links == nil || first.Header().Get("Location") != "/api/v1/notes/"+n.NoteID {
		t.Fatal(first.Code, first.Body.String(), e)
	}
	replay := request("POST", "/api/v1/notes", `{ "body": "Context" }`, "capture")
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatal(replay.Body.String())
	}
	path := "/api/v1/notes/" + n.NoteID
	w := request("PATCH", path, `{"body":"Updated"}`, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request("PUT", path+"/links/task/missing", "", "")
	assertErrorCode(t, w, 404, "task_not_found")
	w = request("PUT", path+"/links/unknown/id", "", "")
	assertErrorCode(t, w, 400, "invalid_note_link")
	w = request("PATCH", path, `{"body":null}`, "")
	assertErrorCode(t, w, 400, "null_field")
	w = request("PATCH", path, `{}`, "")
	assertErrorCode(t, w, 400, "invalid_note")
	w = request("POST", path, `{}`, "")
	assertErrorCode(t, w, 405, "method_not_allowed")
	w = request("DELETE", path, "", "")
	if e = json.Unmarshal(w.Body.Bytes(), &n); e != nil || w.Code != 200 || !n.Deleted || n.Note != nil {
		t.Fatal(w.Body.String(), e)
	}
	w = request("GET", path, "", "")
	assertErrorCode(t, w, 404, "note_not_found")
}
