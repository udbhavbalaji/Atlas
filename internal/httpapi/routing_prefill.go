package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"strings"
	"time"
)

type channelPrefill struct {
	Fields     map[string]string `json:"fields"`
	Reminder   string            `json:"reminder"`
	Note       string            `json:"note"`
	Provenance map[string]string `json:"provenance"`
	Warnings   []string          `json:"warnings"`
}

// Use the already implemented local parser only for reviewable field hints.
// Its intent decision does not override routing. No context search, new language
// rule, model call or write occurs here. Existing records remain context data;
// their fields are not silently inherited by a newly captured record.
func prepareChannel(state routing.State, kind string) channelPrefill {
	seed := channelPrefill{Fields: map[string]string{}, Provenance: map[string]string{}, Warnings: []string{}}
	put := func(field, value, source string) {
		if value != "" {
			seed.Fields[field] = value
			seed.Provenance[field] = source
		}
	}
	at, err := time.Parse(time.RFC3339Nano, state.ReferenceAt)
	if err != nil {
		seed.Warnings = append(seed.Warnings, "The source reference time is invalid; enter fields explicitly.")
		return seed
	}
	parsed, err := interpret.Interpret(state.Text, state.Timezone, at)
	if err != nil {
		seed.Warnings = append(seed.Warnings, "The existing local parser could not prepare this input; enter fields explicitly.")
		return seed
	}
	if kind == "note" {
		body := parsed.Draft.NoteBody
		origin := "note content from the original sentence"
		if body == "" {
			body = state.Text
			origin = "original sentence, retained verbatim"
		}
		put("note_body", body, origin)
		return seed
	}
	if title := strings.TrimSpace(parsed.Draft.Title); len([]rune(title)) <= 500 {
		put("title", title, "title from the original sentence")
	}
	if kind == "task" {
		put("due_at", parsed.Draft.DueAt, "deadline from the original sentence")
	}
	reminderAt := parsed.Draft.ReminderAt
	if kind == "reminder" && reminderAt == "" {
		reminderAt = parsed.Draft.DueAt
	}
	put("reminder_at", reminderAt, "resolved time from the original sentence")
	put("repeat", parsed.Draft.Repeat, "repeat from the original sentence")
	put("note_body", parsed.Draft.NoteBody, "attached note from the original sentence")
	if kind == "task" && reminderAt != "" {
		seed.Reminder = "add"
		seed.Provenance["reminder"] = "resolved reminder in the original sentence"
	}
	if parsed.Draft.NoteBody != "" {
		seed.Note = "add"
		seed.Provenance["note"] = "attached note in the original sentence"
	}
	for _, q := range parsed.Questions {
		// The selected channel already resolved intent. Other unresolved fields
		// remain visible rather than manufacturing a date or accepting a guess.
		if q.Field != "kind" {
			seed.Warnings = append(seed.Warnings, q.Message)
		}
	}
	seed.Warnings = append(seed.Warnings, parsed.Assumptions...)
	return seed
}

func applyChannelPrefill(in dispatchRequest, seed channelPrefill) dispatchRequest {
	for field, target := range map[string]*string{"title": &in.Fields.Title, "due_at": &in.Fields.DueAt, "reminder_at": &in.Fields.ReminderAt, "repeat": &in.Fields.Repeat, "note_body": &in.Fields.NoteBody} {
		if (field == "reminder_at" || field == "repeat") && in.Reminder == "skip" || field == "note_body" && in.Note == "skip" {
			continue
		}
		if *target == "" {
			*target = seed.Fields[field]
		}
	}
	if in.Reminder == "" {
		in.Reminder = seed.Reminder
	}
	if in.Note == "" {
		in.Note = seed.Note
	}
	return in
}
