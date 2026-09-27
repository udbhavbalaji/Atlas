// Package interpret turns supported English capture sentences into proposals.
// It never writes records. Unresolved meaning is returned as clarification.
package interpret

import (
	"atlas/internal/store"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("provide 1–12000 Unicode characters and a valid IANA timezone")

type Question struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Result struct {
	Continuation        *Continuation          `json:"continuation,omitempty"`
	ReferenceQuery      string                 `json:"reference_query"`
	Reference           *store.Task            `json:"reference"`
	Candidates          []store.Task           `json:"candidates"`
	CandidatesTruncated bool                   `json:"candidates_truncated"`
	Status              string                 `json:"status"`
	Engine              string                 `json:"engine"`
	Source              string                 `json:"source"`
	Timezone            string                 `json:"timezone"`
	ReferenceAt         string                 `json:"reference_at"`
	Draft               store.CaptureInput     `json:"draft"`
	Proposal            *store.CaptureProposal `json:"proposal"`
	Questions           []Question             `json:"questions"`
	Assumptions         []string               `json:"assumptions"`
}

var notePrefix = regexp.MustCompile(`(?i)^(?:please\s+)?(?:note\s*:|note that\s+|take a note\s*:?|make a note(?: that)?\s*:?|save a note(?: that)?\s*:?|(?:i need to |i want to )?remember that\s+)\s*`)
var reminderPrefix = regexp.MustCompile(`(?i)^(?:please\s+)?(?:remind me(?: to)?\s+|reminder\s*:\s*|set a reminder(?: to| for)?\s+|(?:i need to |i want to )?remember to\s+|don't forget to\s+)`)
var taskPrefix = regexp.MustCompile(`(?i)^(?:please\s+)?(?:task\s*:\s*|(?:add|create) (?:a )?task(?: to)?\s*:?\s+|i need to\s+|i want to\s+|i have to\s+|todo\s*:\s*)`)
var noteClause = regexp.MustCompile(`(?is)(?:[;,]\s*|\s+and\s+)(?:note\s*:\s*|note that\s+|remember that\s+|(?:make|take|save|add|create) a note(?: that)?\s*:?\s+)(.+)$`)
var linkedClause = regexp.MustCompile(`(?i)(?:[;,]\s*|\s+and\s+)(?:remind me(?: to)?|remember to)\s+`)

var imperative = regexp.MustCompile(`(?i)^(?:please\s+)?(?:call|buy|pay|email|send|finish|submit|review|book|schedule|take|walk|water|pick up|collect|check|read|write|prepare|do|clean|visit|meet|bring|order|renew|cancel|follow up|plan|test|update|build|fix|make|get|go|learn|practice|exercise|drink|return|ask|contact|wash|feed|pack|file|complete|charge|print)\b`)
var unsupportedAction = regexp.MustCompile(`(?i)\b(?:and|then|also) (?:call|buy|pay|email|send|finish|submit|book|create|add|remind)\b|^(?:don't|do not|never)\b|[;\n]|\.\s+(?:call|buy|pay|email|send|finish|submit|book|create|add|remind)\b`)
var timingStart = regexp.MustCompile(`(?i)\b(?:by\s+|due\s+|(?:the )?end of (?:the |this )?(?:day|today|tomorrow|week|month|business day|work day|workday)\b|(?:eod|eow|eom|cob)\b|close of business\b|day after tomorrow\b|tomorrow\b|today\b|tonight\b|every\b|daily\b|weekly\b|monthly\b|weekdays\b|weekends\b|yearly\b|annually\b|biweekly\b|once a month\b|(?:next\s+|on\s+)?(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b|\d{4}-\d{2}-\d{2}\b|(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d|(?:in|after)\s+(?:half (?:an? )?hour|a half hour|a quarter (?:of an? )?hour|quarter of an? hour|(?:a )?couple(?: of)? (?:seconds?|minutes?|hours?|days?|weeks?))\b|(?:in|after)\s+(?:\d+|an?|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(?:seconds?|minutes?|hours?|days?|weeks?)\b|\d{1,2}(?::\d{2})?\s*(?:am|pm)\b|\d{1,2}:\d{2}\b|at\s+(?:\d|one\b|two\b|three\b|four\b|five\b|six\b|seven\b|eight\b|nine\b|ten\b|eleven\b|twelve\b|noon\b|midnight\b)|later\b|next week\b|next month\b|after lunch\b|(?:this|next) (?:morning|evening|afternoon|night)\b|on\s+\d)`)

func Interpret(text, zone string, reference time.Time) (Result, error) {
	text = strings.TrimSpace(text)
	r := Result{Engine: "local-english-v1", Source: text, Timezone: zone, ReferenceAt: reference.UTC().Format(time.RFC3339Nano), Candidates: []store.Task{}, Questions: []Question{}, Assumptions: []string{}}
	if !utf8.ValidString(text) || len([]rune(text)) == 0 || len([]rune(text)) > 12000 || zone == "" || zone == "Local" {
		return r, ErrInput
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return r, ErrInput
	}
	reference = reference.In(loc)
	input := store.CaptureInput{Kind: "task"}
	ask := func(field, code, message string) { r.Questions = append(r.Questions, Question{field, code, message}) }
	command := regexp.MustCompile(`(?i)^(?:can|could|would) you (?:please )?`).ReplaceAllString(text, "")
	if prefix := notePrefix.FindStringIndex(command); prefix != nil {
		input.Kind = "note"
		input.NoteBody = strings.TrimSpace(command[prefix[1]:])
		return finish(r, input)
	}
	action := command
	if m := noteClause.FindStringSubmatchIndex(action); m != nil {
		input.NoteBody = strings.TrimSpace(action[m[2]:m[3]])
		action = strings.TrimSpace(action[:m[0]])
	}
	explicitTask := false
	if m := regexp.MustCompile(`(?i)^(.+?)\s*,?\s+remind me(?: to)?\s+(.+)$`).FindStringSubmatch(action); m != nil {
		if start := timingStart.FindStringIndex(m[1]); start != nil && start[0] == 0 {
			action = "Remind me to " + m[2] + " " + strings.TrimRight(m[1], ", ")
		}
	}
	if prefix := reminderPrefix.FindStringIndex(action); prefix != nil {
		input.Kind = "reminder"
		action = strings.TrimSpace(action[prefix[1]:])
	} else if prefix := taskPrefix.FindStringIndex(action); prefix != nil {
		explicitTask = true
		action = strings.TrimSpace(action[prefix[1]:])
	}
	linkedTime := ""
	if input.Kind == "task" {
		if m := linkedClause.FindStringIndex(action); m != nil {
			linkedTime = strings.TrimSpace(action[m[1]:])
			action = strings.TrimSpace(action[:m[0]])
		}
	}
	// Support both "remind me to call ... tomorrow" and "remind me tomorrow ... to call ...".
	primaryTime := ""
	if input.Kind == "reminder" {
		if start := timingStart.FindStringIndex(action); start != nil && start[0] == 0 {
			if at := strings.Index(strings.ToLower(action), " to "); at >= 0 {
				primaryTime = action[:at]
				action = strings.TrimSpace(action[at+4:])
			}
		}
	}
	if start := timingStart.FindStringIndex(action); start != nil {
		suffix := strings.TrimSpace(action[start[0]:])
		if primaryTime == "" {
			primaryTime = suffix
		} else {
			primaryTime += " " + suffix
		}
		action = strings.TrimSpace(action[:start[0]])
		action = strings.TrimSpace(strings.TrimSuffix(action, " on"))
	}
	action = strings.TrimSpace(strings.TrimRight(action, ".;,"))
	action = regexp.MustCompile(`(?i)^please\s+`).ReplaceAllString(action, "")
	input.Title = action
	if action == "" {
		ask("title", "missing_action", "What should the task or reminder say?")
	}
	if !explicitTask && input.Kind == "task" && !imperative.MatchString(action) {
		ask("kind", "unclear_intent", "Is this an action to track, a reminder, or a note? Choose a record type below.")
	}
	if unsupportedAction.MatchString(action) {
		ask("title", "multiple_or_negated_actions", "This flow creates one action at a time. Split separate actions or clarify the wording below.")
	}
	if primaryTime == "" && linkedTime == "" && regexp.MustCompile(`(?i)\b(?:soon|later|tonight|morning|evening|afternoon|midnight|noon|every|monthly|weekdays|weekends)\b|\bnext\s+(?:week|month|year|january|february|march|april|may|june|july|august|september|october|november|december)\b|\bin\s+.+\b(?:seconds?|minutes?|hours?|days?|weeks?)\b`).MatchString(action) {
		input.Timezone = zone
		ask("reminder_at", "unresolved_timing", "The sentence mentions timing that I could not resolve. Set an explicit time below or rewrite it.")
	}
	if input.Kind == "task" && primaryTime == "" && linkedTime == "" && len(r.Questions) == 0 {
		r.Assumptions = append(r.Assumptions, "An action without a time creates a task, with no reminder.")
	}
	if primaryTime != "" {
		deadline := input.Kind == "task" && (strings.HasPrefix(strings.ToLower(primaryTime), "by ") || strings.HasPrefix(strings.ToLower(primaryTime), "due "))
		field := "reminder_at"
		if deadline {
			field = "due_at"
		} else {
			input.Timezone = zone
		}
		at, repeat, assumptions, q := parseTime(primaryTime, reference, loc, field)
		r.Assumptions = append(r.Assumptions, assumptions...)
		r.Questions = append(r.Questions, q...)
		if deadline {
			input.DueAt = at
			if repeat != "" {
				ask("repeat", "repeating_deadline", "Task deadlines do not repeat. Use a recurring reminder instead.")
			}
		} else {
			input.ReminderAt = at
			input.Repeat = repeat
			if input.Kind == "task" {
				r.Assumptions = append(r.Assumptions, "An action with a time creates a task and a linked reminder; use 'by' for a deadline.")
			}
		}
	}
	if linkedTime != "" {
		if input.ReminderAt != "" || input.Repeat != "" {
			ask("reminder_at", "multiple_reminders", "Only one reminder can be captured at a time. Choose its time below.")
		}
		input.Timezone = zone
		at, repeat, assumptions, q := parseTime(linkedTime, reference, loc, "reminder_at")
		input.ReminderAt = at
		input.Repeat = repeat
		r.Assumptions = append(r.Assumptions, assumptions...)
		r.Questions = append(r.Questions, q...)
	}
	if input.Kind == "reminder" && primaryTime == "" {
		input.Timezone = zone
		ask("reminder_at", "missing_time", "When should Atlas remind you? Set a date and time below.")
	}
	return finish(r, input)
}
func finish(r Result, input store.CaptureInput) (Result, error) {
	r.Draft = input
	r.Status = "needs_clarification"
	if len(r.Questions) > 0 {
		return r, nil
	}
	p, e := store.PreviewCapture(input)
	if e != nil {
		r.Questions = append(r.Questions, Question{"input", "invalid_fields", e.Error()})
		return r, nil
	}
	r.Proposal = &p
	r.Draft = p.Input
	r.Status = "ready"
	return r, nil
}
