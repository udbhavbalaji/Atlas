package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type recordMutation struct {
	Action      string              `json:"action"`
	TargetQuery string              `json:"target_query,omitempty"`
	Kind        string              `json:"kind"`
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Field       string              `json:"field,omitempty"`
	Old         string              `json:"old,omitempty"`
	OldCaptured bool                `json:"old_captured,omitempty"`
	New         string              `json:"new,omitempty"`
	Candidates  []mutationCandidate `json:"candidates,omitempty"`
	Effects     []string            `json:"effects,omitempty"`
}

type mutationCandidate struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Score  int    `json:"score"`
	Search string `json:"-"`
}

func noteLabel(n store.Note) string {
	if n.Title != "" {
		return n.Title
	}
	return lookupExcerpt(n.Body)
}

var mutationWords = map[string]bool{
	"a": true, "an": true, "and": true, "at": true, "about": true, "by": true, "change": true, "delete": true, "details": true, "due": true, "edit": true, "for": true, "from": true, "i": true, "in": true, "is": true, "me": true, "move": true, "my": true, "note": true, "notes": true, "of": true, "on": true, "please": true, "remove": true, "reminder": true, "reminders": true, "rename": true, "reschedule": true, "set": true, "task": true, "tasks": true, "the": true, "this": true, "time": true, "title": true, "to": true, "update": true, "want": true, "would": true, "you": true, "complete": true, "finish": true, "reopen": true, "dismiss": true,
}

func mutationTerms(text string) []string {
	before, _, _ := strings.Cut(strings.ToLower(text), " to ")
	words := strings.FieldsFunc(before, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := []string{}
	for _, word := range words {
		if len([]rune(word)) >= 3 && !mutationWords[word] {
			out = append(out, word)
		}
	}
	return out
}

func mutationCandidates(ctx context.Context, s *store.Store, text string) ([]mutationCandidate, error) {
	tasks, err := s.Tasks(ctx)
	if err != nil {
		return nil, err
	}
	reminders, err := s.Reminders(ctx)
	if err != nil {
		return nil, err
	}
	notes, err := s.Notes(ctx)
	if err != nil {
		return nil, err
	}
	all := []mutationCandidate{}
	for _, t := range tasks {
		all = append(all, mutationCandidate{Kind: "task", ID: t.ID, Title: t.Title})
	}
	for _, r := range reminders {
		all = append(all, mutationCandidate{Kind: "reminder", ID: r.ID, Title: r.Title})
	}
	for _, n := range notes {
		all = append(all, mutationCandidate{Kind: "note", ID: n.ID, Title: noteLabel(n), Search: n.Title + " " + n.Body})
	}
	terms := mutationTerms(text)
	phrase := strings.ToLower(text)
	scope := lookupScope(text)
	matched := []mutationCandidate{}
	for _, candidate := range all {
		title := strings.ToLower(candidate.Title)
		if candidate.Search != "" {
			title = strings.ToLower(candidate.Search)
		}
		for _, term := range terms {
			if strings.Contains(title, term) {
				candidate.Score += 3
			}
		}
		if len(terms) == 0 {
			candidate.Score = 1
		}
		if scope != "" && scope != candidate.Kind {
			continue
		}
		if scope == candidate.Kind {
			candidate.Score += 2
		}
		if scope == "" && candidate.Kind == "task" && (strings.Contains(phrase, "reschedule") || strings.Contains(phrase, "interview")) {
			candidate.Score += 2
		}
		if candidate.Score > 0 {
			matched = append(matched, candidate)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Score != matched[j].Score {
			return matched[i].Score > matched[j].Score
		}
		return matched[i].Title < matched[j].Title
	})
	if len(matched) > 10 {
		matched = matched[:10]
	}
	return matched, nil
}

func mutationSource(m recordMutation) conversationSource {
	plural := map[string]string{"task": "tasks", "reminder": "reminders", "note": "notes"}[m.Kind]
	return conversationSource{Type: m.Kind, Title: m.Title, URL: "/#" + plural + "/" + m.ID}
}

func (c *routingConversation) prepareMutation(ctx context.Context, s *store.Store) error {
	if c.Mutation == nil {
		c.Mutation = &recordMutation{Action: c.Channel}
	}
	m := c.Mutation
	if m.ID != "" && m.Title == "" {
		switch m.Kind {
		case "task":
			current, err := s.TaskState(ctx, m.ID)
			if err != nil {
				return err
			}
			m.Title = current.Task.Title
		case "reminder":
			current, err := s.ReminderState(ctx, m.ID)
			if err != nil {
				return err
			}
			m.Title = current.Reminder.Title
		case "note":
			current, err := s.NoteState(ctx, m.ID)
			if err != nil {
				return err
			}
			m.Title = noteLabel(*current.Note)
		default:
			return routing.ErrContract
		}
	}
	if m.ID == "" {
		query := c.Route.Input.Text
		if m.TargetQuery != "" {
			query = m.TargetQuery
		}
		candidates, err := mutationCandidates(ctx, s, query)
		if err != nil {
			return err
		}
		if c.Route.Plan != nil && m.Kind != "" {
			filtered := candidates[:0]
			for _, candidate := range candidates {
				if candidate.Kind == m.Kind {
					filtered = append(filtered, candidate)
				}
			}
			candidates = filtered
		}
		if len(candidates) == 0 {
			c.Question = &channelQuestion{ID: "record", Field: "record", ValueType: "text", Prompt: "I couldn't find that saved record. What is its title or a distinctive phrase?", Required: true}
			c.State = "awaiting_target"
			return nil
		}
		if c.Route.Plan != nil {
			m.Candidates = candidates
			choices := []provider.Choice{}
			for _, candidate := range candidates {
				choices = append(choices, provider.Choice{Value: candidate.ID, Label: candidate.Kind + ": " + candidate.Title})
			}
			c.Question = &channelQuestion{ID: "record", Field: "record", ValueType: "choice", Prompt: "Which saved record should I " + m.Action + "?", Required: true, Choices: choices}
			c.State = "awaiting_target"
			return nil
		}
		if len(candidates) > 1 && candidates[0].Score-candidates[1].Score < 2 {
			m.Candidates = candidates
			choices := []provider.Choice{}
			for _, candidate := range candidates {
				choices = append(choices, provider.Choice{Value: candidate.ID, Label: candidate.Kind + ": " + candidate.Title})
			}
			c.Question = &channelQuestion{ID: "record", Field: "record", ValueType: "choice", Prompt: "Which saved record should I " + m.Action + "?", Required: true, Choices: choices}
			c.State = "awaiting_target"
			return nil
		}
		m.Kind, m.ID, m.Title = candidates[0].Kind, candidates[0].ID, candidates[0].Title
	}
	if m.Action == "edit" && c.Route.Plan != nil && m.Field != "" {
		if err := routing.ValidatePlan(routing.ActionPlan{Action: "edit", Kind: m.Kind, Field: m.Field}, c.Route.Input); err != nil {
			return err
		}
	}
	if m.Action == "delete" {
		if m.Kind == "task" {
			current, err := s.TaskState(ctx, m.ID)
			if err != nil {
				return err
			}
			for _, reminder := range current.Reminders {
				if reminder.Status != "completed" {
					m.Effects = append(m.Effects, "The linked reminder “"+reminder.Title+"” will be cancelled.")
				}
			}
		}
		c.State = "awaiting_delete_confirmation"
		return nil
	}
	return c.prepareEditValue(c.Route.Input.Text)
}

func (c *routingConversation) prepareEditValue(text string) error {
	m := c.Mutation
	if c.Route.Plan != nil && (m.Field == "due_at" || m.Field == "scheduled_at") && m.New != "" {
		instant, _, ok := interpret.ResolveTime(m.New, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, m.Field)
		if ok {
			m.New = instant
		} else {
			m.New = ""
		}
	}
	lower := strings.ToLower(c.Route.Input.Text)
	if m.Field == "" && c.Route.Plan == nil {
		switch {
		case strings.Contains(lower, "reopen") && m.Kind == "task":
			m.Field, m.New = "status", "open"
		case (strings.Contains(lower, "complete") || strings.Contains(lower, "finish")) && m.Kind == "task":
			m.Field, m.New = "status", "completed"
		case strings.Contains(lower, "dismiss") && m.Kind == "reminder":
			m.Field, m.New = "status", "dismissed"
		case (strings.Contains(lower, "complete") || strings.Contains(lower, "finish")) && m.Kind == "reminder":
			m.Field, m.New = "status", "completed"
		case strings.Contains(lower, "rename") || strings.Contains(lower, "title"):
			m.Field = "title"
		case m.Kind == "note":
			m.Field = "body"
		case strings.Contains(lower, "detail"):
			m.Field = "details"
		case strings.Contains(lower, "status"):
			m.Field = "status"
		case m.Kind == "reminder" && (strings.Contains(lower, "reschedule") || strings.Contains(lower, "move") || strings.Contains(lower, "time") || strings.Contains(lower, "date")):
			m.Field = "scheduled_at"
		case m.Kind == "task" && (strings.Contains(lower, "reschedule") || strings.Contains(lower, "move") || strings.Contains(lower, "due") || strings.Contains(lower, "time") || strings.Contains(lower, "date")):
			m.Field = "due_at"
		}
	}
	if m.Field == "" {
		choices := []provider.Choice{{Value: "title", Label: "Title"}, {Value: "status", Label: "Status"}}
		if m.Kind == "note" {
			choices = []provider.Choice{{Value: "title", Label: "Title"}, {Value: "body", Label: "Body"}}
		}
		if m.Kind == "task" {
			choices = append(choices, provider.Choice{Value: "details", Label: "Details"}, provider.Choice{Value: "due_at", Label: "Due date"})
		}
		if m.Kind == "reminder" {
			choices = append(choices, provider.Choice{Value: "scheduled_at", Label: "Reminder time"})
		}
		c.Question = &channelQuestion{ID: "edit_field", Field: "edit_field", ValueType: "choice", Prompt: "What should I change on “" + m.Title + "”?", Required: true, Choices: choices}
		c.State = "awaiting_field"
		return nil
	}
	if m.New == "" {
		value := strings.TrimSpace(text)
		if text == c.Route.Input.Text {
			_, after, ok := strings.Cut(strings.ToLower(text), " to ")
			if ok {
				value = strings.TrimSpace(text[len(text)-len(after):])
			} else {
				value = ""
			}
		}
		if value != "" {
			if m.Field == "due_at" || m.Field == "scheduled_at" {
				instant, _, ok := interpret.ResolveTime(value, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, m.Field)
				if ok {
					m.New = instant
				}
			} else {
				if m.Field == "title" {
					value = strings.TrimSuffix(value, ".")
				}
				m.New = value
			}
		}
	}
	if m.New == "" {
		prompt := "What should the new " + strings.ReplaceAll(m.Field, "_", " ") + " be for “" + m.Title + "”?"
		c.Question = &channelQuestion{ID: "edit_value", Field: "edit_value", ValueType: "text", Prompt: prompt, Required: true}
		c.State = "awaiting_change"
		return nil
	}
	c.Question = nil
	c.State = "applying_edit"
	return nil
}

func (c *routingConversation) mutationReply(ctx context.Context, s *store.Store, value string) bool {
	if c.Mutation == nil {
		return false
	}
	if c.State == "awaiting_target" {
		if len(c.Mutation.Candidates) == 0 {
			c.Mutation.TargetQuery = value
			_ = c.prepareMutation(ctx, s)
			return true
		}
		for _, candidate := range c.Mutation.Candidates {
			if candidate.ID == value || strings.EqualFold(candidate.Title, value) || strings.EqualFold(candidate.Kind+": "+candidate.Title, value) {
				c.Mutation.ID, c.Mutation.Kind, c.Mutation.Title = candidate.ID, candidate.Kind, candidate.Title
				c.Mutation.Candidates = nil
				c.Question = nil
				_ = c.prepareMutation(ctx, s)
				return true
			}
		}
		return false
	}
	if c.State == "awaiting_change" {
		c.Mutation.New = ""
		_ = c.prepareEditValue(value)
		return true
	}
	if c.State == "awaiting_field" {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, choice := range c.Question.Choices {
			if value == choice.Value || strings.EqualFold(value, choice.Label) {
				c.Mutation.Field = choice.Value
				c.Mutation.New = ""
				_ = c.prepareEditValue(c.Route.Input.Text)
				return true
			}
		}
		return false
	}
	return false
}

func (c *routingConversation) snapshotMutation(ctx context.Context, s *store.Store) error {
	m := c.Mutation
	if m == nil || m.Action != "edit" || m.OldCaptured {
		return nil
	}
	switch m.Kind {
	case "task":
		v, err := s.TaskState(ctx, m.ID)
		if err != nil {
			return err
		}
		switch m.Field {
		case "title":
			m.Old = v.Task.Title
		case "details":
			m.Old = v.Task.Details
		case "due_at":
			m.Old = v.Task.DueAt
		case "status":
			m.Old = v.Task.Status
		default:
			return routing.ErrContract
		}
	case "reminder":
		v, err := s.ReminderState(ctx, m.ID)
		if err != nil {
			return err
		}
		switch m.Field {
		case "title":
			m.Old = v.Reminder.Title
		case "scheduled_at":
			m.Old = v.Reminder.ScheduledAt
		case "status":
			m.Old = v.Reminder.Status
		default:
			return routing.ErrContract
		}
	case "note":
		v, err := s.NoteState(ctx, m.ID)
		if err != nil {
			return err
		}
		if m.Field == "title" {
			m.Old = v.Note.Title
		} else {
			m.Old = v.Note.Body
		}
	default:
		return routing.ErrContract
	}
	m.OldCaptured = true
	return nil
}

func (c *routingConversation) commitMutation(ctx context.Context, s *store.Store) error {
	m := c.Mutation
	if m == nil || m.ID == "" {
		return routing.ErrContract
	}
	if m.Action == "delete" {
		var err error
		switch m.Kind {
		case "task":
			_, err = s.DeleteTaskState(ctx, m.ID)
		case "reminder":
			_, err = s.DeleteReminder(ctx, m.ID)
		case "note":
			_, err = s.DeleteNote(ctx, m.ID)
		default:
			return routing.ErrContract
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrReminderNotFound) && !errors.Is(err, store.ErrNoteNotFound) {
			return err
		}
		c.Answer = &conversationAnswer{Text: strings.TrimSpace(fmt.Sprintf("Deleted %s “%s”. %s", m.Kind, m.Title, strings.Join(m.Effects, " "))), Sources: []conversationSource{}}
		c.State = "answered"
		return nil
	}
	var before, after string
	effects := []string{}
	switch m.Kind {
	case "task":
		current, err := s.TaskState(ctx, m.ID)
		if err != nil {
			return err
		}
		switch m.Field {
		case "title":
			before = current.Task.Title
		case "details":
			before = current.Task.Details
		case "due_at":
			before = current.Task.DueAt
		case "status":
			before = current.Task.Status
		default:
			return routing.ErrContract
		}
		if before != m.New {
			patch := store.TaskPatch{}
			switch m.Field {
			case "title":
				patch.Title = &m.New
			case "details":
				patch.Details = &m.New
			case "due_at":
				patch.DueAt = &m.New
			case "status":
				patch.Status = &m.New
			}
			updated, err := s.PatchTaskState(ctx, m.ID, patch)
			if err != nil {
				return err
			}
			type reminderBefore struct{ Status, ScheduledAt string }
			prior := map[string]reminderBefore{}
			for _, reminder := range current.Reminders {
				prior[reminder.ID] = reminderBefore{Status: reminder.Status, ScheduledAt: reminder.ScheduledAt}
			}
			for _, reminder := range updated.Reminders {
				if old, ok := prior[reminder.ID]; ok && old.Status != reminder.Status {
					effects = append(effects, fmt.Sprintf("Linked reminder “%s” changed from %s to %s.", reminder.Title, old.Status, reminder.Status))
				}
				if old, ok := prior[reminder.ID]; ok && old.ScheduledAt != reminder.ScheduledAt {
					effects = append(effects, fmt.Sprintf("Linked reminder “%s” moved from %s to %s.", reminder.Title, old.ScheduledAt, reminder.ScheduledAt))
				}
			}
			switch m.Field {
			case "title":
				after = updated.Task.Title
			case "details":
				after = updated.Task.Details
			case "due_at":
				after = updated.Task.DueAt
			case "status":
				after = updated.Task.Status
			}
		} else {
			after = before
		}
	case "reminder":
		current, err := s.ReminderState(ctx, m.ID)
		if err != nil {
			return err
		}
		switch m.Field {
		case "title":
			before = current.Reminder.Title
		case "scheduled_at":
			before = current.Reminder.ScheduledAt
		case "status":
			before = current.Reminder.Status
		default:
			return routing.ErrContract
		}
		if before != m.New {
			var updated store.ReminderAction
			if m.Field == "title" {
				updated, err = s.RenameReminder(ctx, m.ID, m.New)
			} else if m.Field == "scheduled_at" {
				var changes []store.WorkflowChange
				updated, changes, err = s.ReminderMutationWorkflow(ctx, m.ID, "scheduled", m.New, true)
				for _, change := range changes {
					if !change.Derived {
						continue
					}
					label := change.Kind + " “" + change.Title + "”"
					effects = append(effects, fmt.Sprintf("Linked %s changed %s from %s to %s.", label, strings.ReplaceAll(change.Field, "_", " "), change.Old, change.New))
				}
			} else {
				updated, err = s.ReminderMutation(ctx, m.ID, m.New, "")
			}
			if err != nil {
				return err
			}
			switch m.Field {
			case "title":
				after = updated.Reminder.Title
			case "scheduled_at":
				after = updated.Reminder.ScheduledAt
			case "status":
				after = updated.Reminder.Status
			}
		} else {
			after = before
		}
	case "note":
		if m.Field != "body" && m.Field != "title" {
			return routing.ErrContract
		}
		current, err := s.NoteState(ctx, m.ID)
		if err != nil {
			return err
		}
		if m.Field == "title" {
			before = current.Note.Title
		} else {
			before = current.Note.Body
		}
		if before != m.New {
			var updated store.NoteAction
			if m.Field == "title" {
				updated, err = s.RenameNote(ctx, m.ID, m.New)
			} else {
				updated, err = s.UpdateNote(ctx, m.ID, m.New)
			}
			if err != nil {
				return err
			}
			if m.Field == "title" {
				after = updated.Note.Title
			} else {
				after = updated.Note.Body
			}
		} else {
			after = before
		}
	default:
		return routing.ErrContract
	}
	if !m.OldCaptured {
		m.Old = before
		m.OldCaptured = true
	}
	format := func(value string) string {
		if value == "" {
			return "unset"
		}
		if m.Field == "due_at" || m.Field == "scheduled_at" {
			return lookupTime(value, c.Route.Input.Timezone)
		}
		return "“" + lookupExcerpt(value) + "”"
	}
	message := fmt.Sprintf("Updated %s “%s”: %s from %s to %s.", m.Kind, m.Title, strings.ReplaceAll(m.Field, "_", " "), format(m.Old), format(after))
	if before == after && m.Old == before {
		message = fmt.Sprintf("%s “%s” already has %s set to %s.", strings.ToUpper(m.Kind[:1])+m.Kind[1:], m.Title, strings.ReplaceAll(m.Field, "_", " "), format(after))
	}
	if len(effects) > 0 {
		message += " " + strings.Join(effects, " ")
	}
	c.Answer = &conversationAnswer{Text: message, Sources: []conversationSource{mutationSource(*m)}}
	c.State = "answered"
	return nil
}
