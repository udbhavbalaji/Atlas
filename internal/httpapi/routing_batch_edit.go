package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"strings"
)

type batchEditMutation struct {
	Kind       string              `json:"kind"`
	Status     string              `json:"status"`
	Field      string              `json:"field,omitempty"`
	Value      string              `json:"value,omitempty"`
	All        bool                `json:"all"`
	Candidates []store.BatchTarget `json:"candidates,omitempty"`
	Targets    []store.BatchTarget `json:"targets"`
	Effects    []string            `json:"effects"`
}

// Only a complete status command is accepted locally. Extra qualifiers need
// clarification so "all tasks due today" cannot become "all tasks".
func batchEditIntent(text string) *batchEditMutation {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(strings.TrimRight(text, ".!?"))))
	for len(words) > 0 && words[0] == "please" {
		words = words[1:]
	}
	if len(words) >= 2 && (words[0] == "can" || words[0] == "could" || words[0] == "would") && words[1] == "you" {
		words = words[2:]
	}
	if len(words) < 2 {
		return nil
	}
	if words[0] == "rename" || words[0] == "replace" {
		verb := words[0]
		if len(words) >= 5 && (words[1] == "all" || words[1] == "every") && (words[2] == "notes" || words[2] == "note") && (words[3] == "to" || words[3] == "with") {
			field := "title"
			if verb == "replace" {
				field = "body"
			}
			original := strings.Fields(strings.TrimSpace(strings.TrimRight(text, ".!?")))
			value := strings.Join(original[len(original)-len(words)+4:], " ")
			if strings.TrimSpace(value) != "" {
				return &batchEditMutation{Kind: "note", Field: field, Value: value, All: true, Targets: []store.BatchTarget{}, Effects: []string{}}
			}
		}
		return nil
	}
	verb := words[0]
	status, kind := "", ""
	switch verb {
	case "complete", "finish":
		status = "completed"
	case "reopen":
		status = "open"
	case "dismiss":
		status = "dismissed"
	default:
		return nil
	}
	words = words[1:]
	all := false
	if len(words) > 0 && (words[0] == "all" || words[0] == "every") {
		all = true
		words = words[1:]
	}
	if len(words) > 0 && (words[0] == "my" || words[0] == "the") {
		words = words[1:]
	}
	if len(words) != 1 {
		return nil
	}
	switch words[0] {
	case "task", "tasks":
		kind = "task"
	case "reminder", "reminders":
		kind = "reminder"
	default:
		return nil
	}
	if kind == "task" && status == "dismissed" || kind == "reminder" && status == "open" {
		return nil
	}
	return &batchEditMutation{Kind: kind, Status: status, All: all, Targets: []store.BatchTarget{}, Effects: []string{}}
}

func localBatchEditRoute(service routingService, name string, input routing.Request) routing.Result {
	result := localBatchRoute(service, name, input)
	result.Category, result.SelectedTool, result.SelectedChannel, result.ExtractionModel = "change", "edit_batch", "batch_edit", "local_batch_edit_scope"
	result.RoutingToken = service.sign(result)
	return result
}

func ambiguousMultiEdit(ctx context.Context, s *store.Store, text string) (bool, error) {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, " and ") && !strings.Contains(lower, ",") {
		return false, nil
	}
	words := strings.Fields(lower)
	for len(words) > 0 && (words[0] == "please" || words[0] == "can" || words[0] == "could" || words[0] == "you") {
		words = words[1:]
	}
	if len(words) == 0 {
		return false, nil
	}
	switch words[0] {
	case "complete", "finish", "reopen", "dismiss", "edit", "update", "rename", "reschedule", "replace", "change", "mark", "set":
	default:
		return false, nil
	}
	if strings.Contains(lower, "tasks and reminders") || strings.Contains(lower, "reminders and tasks") || strings.Contains(lower, "notes and tasks") || strings.Contains(lower, "tasks and notes") || strings.Contains(lower, "notes and reminders") || strings.Contains(lower, "reminders and notes") {
		return true, nil
	}
	items, err := batchInventory(ctx, s, []string{"task", "reminder", "note"})
	if err != nil {
		return false, err
	}
	return len(exactTitleMatches(lower, items)) >= 2, nil
}

func (c *routingConversation) prepareBatchEdit(ctx context.Context, s *store.Store) error {
	b := batchEditIntent(c.Route.Input.Text)
	if b == nil {
		return routing.ErrContract
	}
	c.BatchEdit = b
	items, err := batchInventory(ctx, s, []string{b.Kind})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		c.Answer, c.State = &conversationAnswer{Text: "There are no saved " + b.Kind + " records to change.", Sources: []conversationSource{}}, "answered"
		return nil
	}
	if b.All {
		b.Targets = items
		return c.reviewBatchEdit(ctx, s)
	}
	b.Candidates = items
	c.State = "awaiting_batch_edit_targets"
	return nil
}

func (c *routingConversation) selectBatchEditTargets(ctx context.Context, s *store.Store, value string) bool {
	b := c.BatchEdit
	if b == nil {
		return false
	}
	selected := []store.BatchTarget{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(value, ",") {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
		found := false
		for _, item := range b.Candidates {
			if item.ID == id {
				selected = append(selected, item)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(selected) == 0 {
		return false
	}
	b.Targets, b.Candidates = selected, nil
	return c.reviewBatchEdit(ctx, s) == nil
}

func (c *routingConversation) reviewBatchEdit(ctx context.Context, s *store.Store) error {
	b := c.BatchEdit
	if b == nil {
		return routing.ErrContract
	}
	b.Effects = []string{}
	for i := range b.Targets {
		t := &b.Targets[i]
		if t.Kind == "task" {
			state, err := s.TaskState(ctx, t.ID)
			if err != nil {
				return err
			}
			t.LinkedReminders = []string{}
			if b.Status == "completed" {
				for _, r := range state.Reminders {
					if r.Status == "scheduled" || r.Status == "due" {
						t.LinkedReminders = append(t.LinkedReminders, r.ID+":"+r.UpdatedAt)
						b.Effects = append(b.Effects, "Linked reminder “"+r.Title+"” will be cancelled.")
					}
				}
			} else {
				for _, r := range state.Reminders {
					if r.Status == "scheduled" || r.Status == "due" {
						t.LinkedReminders = append(t.LinkedReminders, r.ID+":"+r.UpdatedAt)
					}
				}
			}
		} else if t.Kind == "reminder" {
			state, err := s.ReminderState(ctx, t.ID)
			if err != nil {
				return err
			}
			if state.Reminder.Repeat != "" {
				c.State = "unsupported"
				c.Route.Plan = &routing.ActionPlan{Action: "unsupported", Clarification: "A repeating reminder is in this set. Acknowledge its current occurrence separately; no bulk changes were made."}
				return nil
			}
		}
	}
	c.State = "awaiting_batch_edit_confirmation"
	return nil
}

func (c *routingConversation) commitBatchEdit(ctx context.Context, s *store.Store, key string) error {
	b := c.BatchEdit
	if b == nil {
		return routing.ErrContract
	}
	var err error
	if b.Kind == "note" {
		err = s.EditBatchNoteRequest(ctx, key, b.Field, b.Value, b.Targets)
	} else {
		err = s.EditBatchStatusRequest(ctx, key, b.Kind, b.Status, b.Targets)
	}
	if err != nil {
		return err
	}
	if b.Kind == "note" {
		c.Answer = &conversationAnswer{Text: fmt.Sprintf("Set %s on %d note(s) to “%s”.", b.Field, len(b.Targets), lookupExcerpt(b.Value)), Sources: []conversationSource{}}
	} else {
		c.Answer = &conversationAnswer{Text: fmt.Sprintf("Set %d %s record(s) to %s.", len(b.Targets), b.Kind, b.Status), Sources: []conversationSource{}}
	}
	c.State = "answered"
	return nil
}
