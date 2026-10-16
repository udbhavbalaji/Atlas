package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidSearch = errors.New("search requires q of 1–200 Unicode code points, a supported type/status, limit 1–100, and offset 0–10000")

type SearchOptions struct {
	Query  string
	Type   string
	Status string
	Limit  int
	Offset int
}
type SearchResult struct {
	Type          string   `json:"type"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Snippet       string   `json:"snippet"`
	Status        string   `json:"status"`
	UpdatedAt     string   `json:"updated_at"`
	MatchedFields []string `json:"matched_fields"`
	URL           string   `json:"url"`
	APIURL        string   `json:"api_url"`
}
type SearchResponse struct {
	Query      string         `json:"query"`
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	Results    []SearchResult `json:"results"`
	HasMore    bool           `json:"has_more"`
	NextOffset *int           `json:"next_offset"`
}

func validSearch(o SearchOptions) bool {
	if !utf8.ValidString(o.Query) || len([]rune(o.Query)) == 0 || len([]rune(o.Query)) > 200 || o.Limit < 1 || o.Limit > 100 || o.Offset < 0 || o.Offset > 10000 {
		return false
	}
	if o.Type != "" && o.Type != "task" && o.Type != "reminder" && o.Type != "note" {
		return false
	}
	if o.Status == "" {
		return true
	}
	switch o.Type {
	case "note":
		return false
	case "task":
		return o.Status == "open" || o.Status == "completed"
	case "reminder":
		return o.Status == "scheduled" || o.Status == "due" || o.Status == "dismissed" || o.Status == "completed"
	default:
		return o.Status == "open" || o.Status == "completed" || o.Status == "scheduled" || o.Status == "due" || o.Status == "dismissed"
	}
}
func searchSnippet(text, query string) string {
	runes := []rune(text)
	position := strings.Index(strings.ToLower(text), strings.ToLower(query))
	start := 0
	// Lowercase can change byte lengths, so translate the matching prefix by rune count.
	if position >= 0 {
		start = len([]rune(strings.ToLower(text)[:position])) - 45
		if start < 0 {
			start = 0
		}
	}
	end := start + 180
	if end > len(runes) {
		end = len(runes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// Search streams a canonical SQLite snapshot in deterministic update order.
// Unicode-aware matching happens in Go; %, _, and SQL-like text stay literal.
// No extra index or duplicated search state can drift from record edits/deletes.
func (s *Store) Search(ctx context.Context, o SearchOptions) (SearchResponse, error) {
	o.Query = strings.TrimSpace(o.Query)
	response := SearchResponse{Query: o.Query, Type: o.Type, Status: o.Status, Limit: o.Limit, Offset: o.Offset, Results: []SearchResult{}}
	if !validSearch(o) {
		return response, ErrInvalidSearch
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind,id,title,body,status,updated_at FROM (SELECT 'task' AS kind,id,title,details AS body,status,updated_at FROM tasks UNION ALL SELECT 'reminder',id,title,'' AS body,status,updated_at FROM reminders UNION ALL SELECT 'note',id,title,body,'' AS status,updated_at FROM notes) WHERE (?='' OR kind=?) AND (?='' OR status=?) ORDER BY updated_at DESC,kind,id`, o.Type, o.Type, o.Status, o.Status)
	if err != nil {
		return response, err
	}
	defer rows.Close()
	matched := 0
	needle := strings.ToLower(o.Query)
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return response, err
		}
		var item SearchResult
		var body string
		if err = rows.Scan(&item.Type, &item.ID, &item.Title, &body, &item.Status, &item.UpdatedAt); err != nil {
			return response, err
		}
		item.MatchedFields = []string{}
		inTitle := strings.Contains(strings.ToLower(item.Title), needle)
		inBody := strings.Contains(strings.ToLower(body), needle)
		if inTitle {
			item.MatchedFields = append(item.MatchedFields, "title")
		}
		if inBody {
			field := "details"
			if item.Type == "note" {
				field = "body"
			}
			item.MatchedFields = append(item.MatchedFields, field)
		}
		if len(item.MatchedFields) == 0 {
			continue
		}
		matched++
		if matched <= o.Offset {
			continue
		}
		if len(response.Results) == o.Limit {
			response.HasMore = true
			next := o.Offset + o.Limit
			if next <= 10000 {
				response.NextOffset = &next
			}
			break
		}
		excerpt := body
		if !inBody {
			excerpt = item.Title
		}
		item.Snippet = searchSnippet(excerpt, o.Query)
		if item.Type == "note" && item.Title == "" {
			first := strings.SplitN(strings.TrimSpace(body), "\n", 2)[0]
			title := []rune(first)
			if len(title) > 80 {
				first = string(title[:80]) + "…"
			}
			item.Title = first
		}
		plural := map[string]string{"task": "tasks", "reminder": "reminders", "note": "notes"}[item.Type]
		item.URL = "/#" + plural + "/" + item.ID
		item.APIURL = "/api/v1/" + plural + "/" + item.ID
		response.Results = append(response.Results, item)
	}
	return response, rows.Err()
}
