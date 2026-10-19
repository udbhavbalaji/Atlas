package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"strings"
)

type occurrenceMutation struct {
	Action       string              `json:"action"`
	Candidates   []store.BatchTarget `json:"candidates,omitempty"`
	Target       *store.BatchTarget  `json:"target,omitempty"`
	OccurrenceID string              `json:"occurrence_id,omitempty"`
	ScheduledAt  string              `json:"scheduled_at,omitempty"`
	Acknowledged bool                `json:"acknowledged,omitempty"`
}

func occurrenceIntent(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range []string{"please ", "can you ", "could you ", "would you "} {
		lower = strings.TrimPrefix(lower, prefix)
	}
	if !strings.Contains(lower, "occurrence") && !strings.Contains(lower, "instance") {
		return ""
	}
	if strings.HasPrefix(lower, "dismiss ") {
		return "dismiss"
	}
	if strings.HasPrefix(lower, "complete ") || strings.HasPrefix(lower, "finish ") {
		return "complete"
	}
	return ""
}

func localOccurrenceRoute(service routingService, name string, input routing.Request) routing.Result {
	result := localBatchRoute(service, name, input)
	result.Category, result.SelectedTool, result.SelectedChannel, result.ExtractionModel = "change", "acknowledge_occurrence", "occurrence", "local_occurrence_scope"
	result.RoutingToken = service.sign(result)
	return result
}

func (c *routingConversation) prepareOccurrence(ctx context.Context, s *store.Store) error {
	action := occurrenceIntent(c.Route.Input.Text)
	if action == "" {
		return routing.ErrContract
	}
	c.Occurrence = &occurrenceMutation{Action: action}
	rows, err := s.Reminders(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Repeat != "" && row.Status == "due" {
			c.Occurrence.Candidates = append(c.Occurrence.Candidates, store.BatchTarget{Kind: "reminder", ID: row.ID, Title: row.Title, UpdatedAt: row.UpdatedAt})
		}
	}
	if len(c.Occurrence.Candidates) == 0 {
		c.Answer, c.State = &conversationAnswer{Text: "There is no due repeating reminder occurrence to acknowledge.", Sources: []conversationSource{}}, "answered"
		return nil
	}
	matches := exactTitleMatches(strings.ToLower(c.Route.Input.Text), c.Occurrence.Candidates)
	if len(matches) == 1 {
		return c.selectOccurrence(ctx, s, matches[0].ID)
	}
	c.State = "awaiting_occurrence_target"
	return nil
}

func (c *routingConversation) selectOccurrence(ctx context.Context, s *store.Store, id string) error {
	if c.Occurrence == nil {
		return routing.ErrContract
	}
	for _, candidate := range c.Occurrence.Candidates {
		if candidate.ID != strings.TrimSpace(id) {
			continue
		}
		state, err := s.ReminderState(ctx, id)
		if err != nil {
			return err
		}
		r := state.Reminder
		if r.Repeat == "" || r.Status != "due" || r.OccurrenceID == "" {
			return store.ErrOccurrenceConflict
		}
		c.Occurrence.Target = &candidate
		c.Occurrence.OccurrenceID, c.Occurrence.ScheduledAt = r.OccurrenceID, r.ScheduledAt
		c.Occurrence.Candidates = nil
		c.State = "awaiting_occurrence_confirmation"
		return nil
	}
	return routing.ErrContract
}

func (c *routingConversation) commitOccurrence(ctx context.Context, s *store.Store) error {
	o := c.Occurrence
	if o == nil || o.Target == nil {
		return routing.ErrContract
	}
	state, err := s.ReminderState(ctx, o.Target.ID)
	if err != nil {
		return err
	}
	r := state.Reminder
	if r.OccurrenceID == o.OccurrenceID && (r.UpdatedAt != o.Target.UpdatedAt || r.ScheduledAt != o.ScheduledAt || r.Status != "due" || r.Repeat == "") {
		return store.ErrOccurrenceConflict
	}
	// A committed acknowledgement may have advanced to the next occurrence
	// before the conversation response was saved. FinishOccurrence recognizes
	// the acknowledged delivery as a durable receipt and returns that result.
	updated, err := s.FinishOccurrence(ctx, o.Target.ID, o.OccurrenceID, o.Action)
	if err != nil {
		return err
	}
	o.Acknowledged = true
	verb := "Completed"
	if o.Action == "dismiss" {
		verb = "Dismissed"
	}
	c.Answer = &conversationAnswer{Text: fmt.Sprintf("%s the current occurrence of reminder “%s”. The next occurrence is scheduled for %s.", verb, o.Target.Title, lookupTime(updated.Reminder.ScheduledAt, c.Route.Input.Timezone)), Sources: []conversationSource{{Type: "reminder", Title: o.Target.Title, URL: "/#reminders/" + o.Target.ID}}}
	c.State = "answered"
	return nil
}
