package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchAcrossRecordsFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.CreateWithFields(ctx, "Plan ATLAS", "Meeting context Äpfel 文本 100% literal_under", "")
	if e != nil {
		t.Fatal(e)
	}
	reminder, e := s.CreateReminder(ctx, "Atlas follow-up", "2030-01-01T09:00:00Z", "UTC")
	if e != nil {
		t.Fatal(e)
	}
	note, _, e := s.CreateNoteRequest(ctx, "", "Atlas context\n"+strings.Repeat("文", 210)+"Äpfel and details", "", "")
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Search(ctx, SearchOptions{Query: "atlas", Limit: 2})
	if e != nil || len(result.Results) != 2 || !result.HasMore || result.NextOffset == nil {
		t.Fatal(result, e)
	}
	next, e := s.Search(ctx, SearchOptions{Query: "atlas", Limit: 2, Offset: *result.NextOffset})
	if e != nil || len(next.Results) != 1 || next.HasMore || next.NextOffset != nil {
		t.Fatal(next, e)
	}
	seen := map[string]bool{}
	for _, item := range append(result.Results, next.Results...) {
		if seen[item.ID] {
			t.Fatal("duplicate page result")
		}
		seen[item.ID] = true
		if item.APIURL == "" || item.URL == "" || len(item.MatchedFields) == 0 {
			t.Fatal(item)
		}
	}
	for _, query := range []string{"äPFEL", "文本", "100%", "literal_under"} {
		r, e := s.Search(ctx, SearchOptions{Query: query, Type: "task", Limit: 20})
		if e != nil || len(r.Results) != 1 || r.Results[0].ID != task.ID || r.Results[0].MatchedFields[0] != "details" {
			t.Fatal(query, r, e)
		}
	}
	r, e := s.Search(ctx, SearchOptions{Query: "%", Limit: 20})
	if e != nil || len(r.Results) != 1 {
		t.Fatal("wildcard was not literal", r, e)
	}
	r, e = s.Search(ctx, SearchOptions{Query: "äpfel", Type: "note", Limit: 20})
	if e != nil || len(r.Results) != 1 || !strings.Contains(r.Results[0].Snippet, "Äpfel") || len([]rune(r.Results[0].Snippet)) > 182 {
		t.Fatal(r, e)
	}
	if e = s.Complete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	r, e = s.Search(ctx, SearchOptions{Query: "atlas", Status: "completed", Limit: 20})
	if e != nil || len(r.Results) != 1 || r.Results[0].ID != task.ID {
		t.Fatal(r, e)
	}
	r, e = s.Search(ctx, SearchOptions{Query: "atlas", Type: "reminder", Status: "scheduled", Limit: 20})
	if e != nil || len(r.Results) != 1 || r.Results[0].ID != reminder.ID {
		t.Fatal(r, e)
	}
	if _, e = s.UpdateNote(ctx, note.NoteID, "Changed text"); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, task.ID); e != nil {
		t.Fatal(e)
	}
	r, e = s.Search(ctx, SearchOptions{Query: "atlas", Limit: 20})
	if e != nil || len(r.Results) != 1 || r.Results[0].ID != reminder.ID {
		t.Fatal(r, e)
	}
}
func TestSearchValidationAndEmptyCollections(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, o := range []SearchOptions{{Query: " ", Limit: 20}, {Query: strings.Repeat("x", 201), Limit: 20}, {Query: "x", Limit: 101}, {Query: "x", Type: "note", Status: "completed", Limit: 20}, {Query: "x", Type: "task", Status: "due", Limit: 20}, {Query: "x", Limit: 20, Offset: -1}, {Query: "x", Type: "wrong", Limit: 20}} {
		if _, e = s.Search(ctx, o); !errors.Is(e, ErrInvalidSearch) {
			t.Fatal(o, e)
		}
	}
	r, e := s.Search(ctx, SearchOptions{Query: "absent", Limit: 20})
	if e != nil || r.Results == nil || len(r.Results) != 0 || r.NextOffset != nil {
		t.Fatal(r, e)
	}
}
