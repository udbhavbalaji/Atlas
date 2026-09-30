package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type conversationSource struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type conversationAnswer struct {
	Text    string               `json:"text"`
	Sources []conversationSource `json:"sources"`
}

var lookupStopWords = map[string]bool{
	"a": true, "about": true, "all": true, "am": true, "an": true, "and": true,
	"are": true, "at": true, "can": true, "check": true, "did": true, "do": true,
	"for": true, "from": true, "have": true, "i": true, "in": true, "information": true,
	"is": true, "it": true, "last": true, "latest": true, "list": true, "me": true, "month": true, "my": true,
	"next": true, "note": true, "notes": true, "of": true, "old": true, "on": true,
	"past": true, "previous": true, "remind": true, "reminder": true, "reminders": true,
	"recent": true, "saved": true, "show": true, "task": true, "tasks": true, "tell": true, "the": true, "this": true,
	"there": true, "to": true, "upcoming": true, "was": true, "were": true,
	"today": true, "tomorrow": true, "week": true, "what": true, "when": true, "where": true, "which": true, "with": true, "year": true, "yesterday": true,
	"you": true,
}

func lookupTerms(question string) []string {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	seen := map[string]bool{}
	terms := []string{}
	for _, word := range words {
		if len([]rune(word)) < 3 || lookupStopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	sort.SliceStable(terms, func(i, j int) bool { return len([]rune(terms[i])) > len([]rune(terms[j])) })
	if len(terms) > 3 {
		terms = terms[:3]
	}
	return terms
}

func lookupScope(question string) string {
	text := strings.ToLower(question)
	switch {
	case strings.Contains(text, "reminder"):
		return "reminder"
	case strings.Contains(text, "note"):
		return "note"
	case strings.Contains(text, "task"):
		return "task"
	default:
		return ""
	}
}

func lookupTime(value, timezone string) string {
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		zone = time.UTC
	}
	return instant.In(zone).Format("Mon, 2 Jan 2006 at 3:04 PM MST")
}

func lookupExcerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 220 {
		return string(runes[:220]) + "…"
	}
	return text
}

type lookupItem struct {
	line   string
	source conversationSource
	score  int
	date   string
}

func lookupHit(ctx context.Context, s *store.Store, hit store.SearchResult, question string, state routing.State) (lookupItem, error) {
	wantsWhen := strings.Contains(strings.ToLower(question), "when")
	item := lookupItem{source: conversationSource{Type: hit.Type, Title: hit.Title, URL: hit.URL}, date: hit.UpdatedAt}
	if len(hit.MatchedFields) > 0 && hit.MatchedFields[0] == "title" {
		item.score += 2
	}
	switch hit.Type {
	case "task":
		v, err := s.TaskState(ctx, hit.ID)
		if err != nil {
			return item, err
		}
		t := v.Task
		item.source.Title = t.Title
		item.line = fmt.Sprintf("Task “%s” is %s", t.Title, t.Status)
		if t.DueAt != "" {
			item.line += "; due " + lookupTime(t.DueAt, state.Timezone)
			if due, dueErr := time.Parse(time.RFC3339Nano, t.DueAt); dueErr == nil {
				if reference, referenceErr := time.Parse(time.RFC3339Nano, state.ReferenceAt); referenceErr == nil && due.Before(reference) && t.Status == "open" {
					item.line += " (past due)"
				}
			}
			if wantsWhen {
				item.score += 3
			}
		} else if wantsWhen {
			item.line += "; no due date is saved"
		}
		if t.Details != "" {
			item.line += ". Details: " + lookupExcerpt(t.Details)
		}
		if len(v.Reminders) > 0 {
			reminder := v.Reminders[0]
			for _, candidate := range v.Reminders[1:] {
				if candidate.Status == "scheduled" && reminder.Status != "scheduled" {
					reminder = candidate
				}
			}
			item.line += fmt.Sprintf(". Linked reminder: %s (%s)", lookupTime(reminder.ScheduledAt, state.Timezone), reminder.Status)
		}
		for i, note := range v.Notes {
			if i == 3 { item.line += ". More linked notes are saved"; break }
			item.line += ". Linked note: " + lookupExcerpt(note.Body)
		}
		if t.Status == "completed" && t.UpdatedAt != "" {
			item.line += ". Last updated " + lookupTime(t.UpdatedAt, state.Timezone)
		}
		if t.Status == "open" {
			item.score++
		}
	case "reminder":
		v, err := s.ReminderState(ctx, hit.ID)
		if err != nil {
			return item, err
		}
		r := v.Reminder
		item.source.Title = r.Title
		item.line = fmt.Sprintf("Reminder “%s” was set for %s (%s)", r.Title, lookupTime(r.ScheduledAt, state.Timezone), r.Status)
		if r.TaskTitle != "" {
			item.line += "; linked task: “" + r.TaskTitle + "”"
		}
		if wantsWhen {
			item.score += 2
		}
	case "note":
		v, err := s.NoteState(ctx, hit.ID)
		if err != nil {
			return item, err
		}
		item.source.Title = lookupExcerpt(v.Note.Body)
		item.line = "Note: “" + lookupExcerpt(v.Note.Body) + "”"
		if wantsWhen {
			item.line += " (no structured event time is saved in this note)"
		}
	}
	return item, nil
}

func answerLookup(ctx context.Context, s *store.Store, state routing.State) (conversationAnswer, error) {
	answer := conversationAnswer{Sources: []conversationSource{}}
	question := strings.TrimSpace(state.Text)
	scope := lookupScope(question)
	terms := lookupTerms(question)
	if len(terms) == 0 {
		return listLookup(ctx, s, state, scope)
	}
	var hits []store.SearchResult
	for _, term := range terms {
		found, err := s.Search(ctx, store.SearchOptions{Query: term, Type: scope, Limit: 100})
		if err != nil {
			return answer, err
		}
		if len(found.Results) > 0 {
			hits = found.Results
			break
		}
	}
	if len(hits) == 0 {
		answer.Text = "I couldn't find a saved task, reminder, or note matching that question. Try a name or phrase from the record."
		return answer, nil
	}
	items := make([]lookupItem, 0, len(hits))
	for _, hit := range hits {
		item, err := lookupHit(ctx, s, hit, question, state)
		if err != nil {
			return answer, err
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].date > items[j].date
	})
	if len(items) > 5 {
		items = items[:5]
	}
	lines := make([]string, 0, len(items)+1)
	for _, item := range items {
		lines = append(lines, item.line+".")
		answer.Sources = append(answer.Sources, item.source)
	}
	if len(items) == 1 {
		answer.Text = "I found this in Atlas: " + lines[0]
	} else {
		answer.Text = "I found these saved records:\n" + strings.Join(lines, "\n")
	}
	return answer, nil
}

func answerPlannedLookup(ctx context.Context, s *store.Store, state routing.State, plan routing.ActionPlan) (conversationAnswer, error) {
	if plan.TargetID == "" {
		return answerLookup(ctx, s, state)
	}
	for _, record := range state.Context {
		if record.ID != plan.TargetID || record.Kind != plan.Kind {
			continue
		}
		plural := map[string]string{"task": "tasks", "reminder": "reminders", "note": "notes"}[record.Kind]
		item, err := lookupHit(ctx, s, store.SearchResult{Type: record.Kind, ID: record.ID, Title: record.Title, URL: "/#" + plural + "/" + record.ID}, state.Text, state)
		if err != nil {
			return conversationAnswer{}, err
		}
		return conversationAnswer{Text: "I found this in Atlas: " + item.line + ".", Sources: []conversationSource{item.source}}, nil
	}
	return conversationAnswer{}, routing.ErrContract
}

func listLookup(ctx context.Context, s *store.Store, state routing.State, scope string) (conversationAnswer, error) {
	answer := conversationAnswer{Sources: []conversationSource{}}
	if scope == "" {
		answer.Text = "Which saved task, reminder, or note should I look up? You can ask by name, such as “When is my interview?”"
		return answer, nil
	}
	lines := []string{}
	switch scope {
	case "reminder":
		reminders, err := s.Reminders(ctx)
		if err != nil {
			return answer, err
		}
		sort.SliceStable(reminders, func(i, j int) bool { return reminders[i].ScheduledAt > reminders[j].ScheduledAt })
		pastOnly := strings.Contains(strings.ToLower(state.Text), "old") || strings.Contains(strings.ToLower(state.Text), "past") || strings.Contains(strings.ToLower(state.Text), "previous") || strings.Contains(strings.ToLower(state.Text), "last")
		futureOnly := strings.Contains(strings.ToLower(state.Text), "upcoming")
		reference, err := time.Parse(time.RFC3339Nano, state.ReferenceAt)
		if err != nil {
			reference = time.Now()
		}
		for _, r := range reminders {
			when, _ := time.Parse(time.RFC3339Nano, r.ScheduledAt)
			if pastOnly && when.After(reference) {
				continue
			}
			if futureOnly && when.Before(reference) {
				continue
			}
			lines = append(lines, fmt.Sprintf("“%s” — %s (%s)", r.Title, lookupTime(r.ScheduledAt, state.Timezone), r.Status))
			answer.Sources = append(answer.Sources, conversationSource{Type: "reminder", Title: r.Title, URL: "/#reminders/" + r.ID})
			if len(lines) == 10 {
				break
			}
		}
	case "task":
		tasks, err := s.Tasks(ctx)
		if err != nil {
			return answer, err
		}
		for _, t := range tasks {
			line := fmt.Sprintf("“%s” (%s)", t.Title, t.Status)
			if t.DueAt != "" {
				line += " — due " + lookupTime(t.DueAt, state.Timezone)
			}
			lines = append(lines, line)
			answer.Sources = append(answer.Sources, conversationSource{Type: "task", Title: t.Title, URL: "/#tasks/" + t.ID})
			if len(lines) == 10 {
				break
			}
		}
	case "note":
		notes, err := s.Notes(ctx)
		if err != nil {
			return answer, err
		}
		for _, n := range notes {
			excerpt := lookupExcerpt(n.Body)
			lines = append(lines, "“"+excerpt+"”")
			answer.Sources = append(answer.Sources, conversationSource{Type: "note", Title: excerpt, URL: "/#notes/" + n.ID})
			if len(lines) == 10 {
				break
			}
		}
	}
	if len(lines) == 0 {
		answer.Text = "I couldn't find any saved " + scope + "s."
		return answer, nil
	}
	answer.Text = "Here are the most recent saved " + scope + "s:\n" + strings.Join(lines, "\n")
	return answer, nil
}
