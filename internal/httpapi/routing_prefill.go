package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"regexp"
	"strings"
	"time"
)

var explicitReminderRequest = regexp.MustCompile(`(?i)\b(?:remind me|set (?:a )?reminder|notify me|don't forget|remember to)\b`)

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
	if kind == "task" && reminderAt != "" && !explicitReminderRequest.MatchString(state.Text) {
		// The local parser treats any timed action as a reminder, including
		// approximate phrases such as "tomorrow morning". A notification
		// must be requested explicitly; an exact time can still be a deadline.
		reminderAt = ""
		if parsed.Draft.DueAt == "" {
			approximate := false
			for _, assumption := range parsed.Assumptions {
				if strings.Contains(assumption, "review this default") {
					approximate = true
				}
			}
			if !approximate {
				put("due_at", parsed.Draft.ReminderAt, "exact task time from the original sentence")
			}
		}
	}
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
	for _, assumption := range parsed.Assumptions {
		if kind == "task" && !explicitReminderRequest.MatchString(state.Text) && (strings.Contains(assumption, "linked reminder") || strings.Contains(assumption, "action with a time creates")) {
			continue
		}
		seed.Warnings = append(seed.Warnings, assumption)
	}
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
