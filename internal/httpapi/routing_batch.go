package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type batchMutation struct {
	Kinds      []string            `json:"kinds"`
	Candidates []store.BatchTarget `json:"candidates,omitempty"`
	Targets    []store.BatchTarget `json:"targets"`
	Effects    []string            `json:"effects"`
}

// Only a complete, explicit bulk command is handled locally. A qualifier or
// another operation leaves the request with the normal router for clarification.
func explicitDeleteAll(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	if len(words) == 0 {
		return nil
	}
	i := 0
	if words[i] == "please" {
		i++
	}
	if i+1 < len(words) && (words[i] == "can" || words[i] == "could") && words[i+1] == "you" {
		i += 2
	}
	if i >= len(words) || (words[i] != "delete" && words[i] != "remove" && words[i] != "clear") {
		return nil
	}
	i++
	if i < len(words) && words[i] == "out" {
		i++
	}
	if i >= len(words) || (words[i] != "all" && words[i] != "every") {
		return nil
	}
	i++
	for i < len(words) && (words[i] == "of" || words[i] == "my" || words[i] == "the") {
		i++
	}
	kinds := []string{}
	seen := map[string]bool{}
	for i < len(words) {
		if words[i] == "and" {
			if i+1 == len(words) {
				return nil
			}
			i++
			continue
		}
		kind := strings.TrimSuffix(words[i], "s")
		if kind != "task" && kind != "reminder" && kind != "note" || seen[kind] {
			return nil
		}
		seen[kind] = true
		kinds = append(kinds, kind)
		i++
	}
	return kinds
}

func batchDeleteIntent(ctx context.Context, s *store.Store, text string) (bool, error) {
	if len(explicitDeleteAll(text)) > 0 {
		return true, nil
	}
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return false, nil
	}
	i := 0
	if words[i] == "please" {
		i++
	}
	if i+1 < len(words) && (words[i] == "can" || words[i] == "could") && words[i+1] == "you" {
		i += 2
	}
	if i+2 < len(words) && words[i] == "i" && words[i+1] == "want" && words[i+2] == "to" {
		i += 3
	}
	if i >= len(words) {
		return false, nil
	}
	verb := words[i]
	if verb != "delete" && verb != "remove" && verb != "clear" {
		return false, nil
	}
	lower := strings.ToLower(text)
	kindCount := 0
	mentions := 0
	plural := false
	quantified := false
	tokens := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range tokens {
		if word == "all" || word == "every" {
			quantified = true
		}
	}
	for _, kind := range []string{"task", "reminder", "note"} {
		if strings.Contains(lower, kind) {
			kindCount++
		}
		for _, word := range tokens {
			if word == kind || word == kind+"s" {
				mentions++
			}
			if word == kind+"s" {
				plural = true
			}
		}
	}
	if kindCount >= 2 || (quantified && kindCount > 0) || (strings.Contains(lower, " and ") && (mentions >= 2 || plural)) {
		return true, nil
	}
	items, err := batchInventory(ctx, s, []string{"task", "reminder", "note"})
	if err != nil {
		return false, err
	}
	return len(exactTitleMatches(lower, items)) >= 2, nil
}

func exactTitleMatches(lower string, items []store.BatchTarget) []store.BatchTarget {
	matched := []store.BatchTarget{}
	for _, item := range items {
		if len([]rune(item.Title)) >= 3 && strings.Contains(lower, strings.ToLower(item.Title)) {
			matched = append(matched, item)
		}
	}
	return matched
}

func batchInventory(ctx context.Context, s *store.Store, kinds []string) ([]store.BatchTarget, error) {
	items := []store.BatchTarget{}
	for _, kind := range kinds {
		switch kind {
		case "task":
			rows, err := s.Tasks(ctx)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				items = append(items, store.BatchTarget{Kind: kind, ID: row.ID, Title: row.Title, UpdatedAt: row.UpdatedAt})
			}
		case "reminder":
			rows, err := s.Reminders(ctx)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				items = append(items, store.BatchTarget{Kind: kind, ID: row.ID, Title: row.Title, UpdatedAt: row.UpdatedAt})
			}
		case "note":
			rows, err := s.Notes(ctx)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				items = append(items, store.BatchTarget{Kind: kind, ID: row.ID, Title: noteLabel(row), UpdatedAt: row.UpdatedAt})
			}
		}
	}
	return items, nil
}

func localBatchRoute(service routingService, name string, input routing.Request) routing.Result {
	result := routing.Result{Version: routing.Version, RegistryVersion: routing.RegistryVersion, RequestID: input.RequestID, Provider: name, Category: "remove", SelectedTool: "delete_batch", SelectedChannel: "batch_delete", State: "routed", Policy: routing.DefaultPolicy,
		Input: routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), RecentConversation: input.RecentConversation, Context: []provider.ContextRecord{}}, ExtractionModel: "local_batch_scope"}
	result.RoutingToken = service.sign(result)
	return result
}

func (c *routingConversation) prepareBatch(ctx context.Context, s *store.Store) error {
	kinds := explicitDeleteAll(c.Route.Input.Text)
	all := len(kinds) > 0
	if !all {
		kinds = []string{"task", "reminder", "note"}
	}
	items, err := batchInventory(ctx, s, kinds)
	if err != nil {
		return err
	}
	b := &batchMutation{Kinds: kinds, Targets: []store.BatchTarget{}, Effects: []string{}}
	if all {
		b.Targets = items
	} else {
		b.Targets = exactTitleMatches(strings.ToLower(c.Route.Input.Text), items)
	}
	c.Batch = b
	if len(items) == 0 {
		c.Answer = &conversationAnswer{Text: "There are no saved " + strings.Join(kinds, " or ") + " records to delete.", Sources: []conversationSource{}}
		c.State = "answered"
		return nil
	}
	if !all && len(b.Targets) < 2 {
		b.Targets = []store.BatchTarget{}
		b.Candidates = items
		c.State = "awaiting_batch_targets"
		return nil
	}
	if !all {
		b.Kinds = []string{}
		for _, target := range b.Targets {
			if !containsKind(b.Kinds, target.Kind) {
				b.Kinds = append(b.Kinds, target.Kind)
			}
		}
	}
	if err := c.batchEffects(ctx, s); err != nil {
		return err
	}
	c.State = "awaiting_delete_confirmation"
	return nil
}

func (c *routingConversation) selectBatchTargets(value string) bool {
	if c.Batch == nil {
		return false
	}
	ids := strings.Split(value, ",")
	if len(ids) == 0 {
		return false
	}
	selected := []store.BatchTarget{}
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
		found := false
		for _, candidate := range c.Batch.Candidates {
			if candidate.ID == id {
				selected = append(selected, candidate)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	c.Batch.Targets = selected
	c.Batch.Kinds = []string{}
	for _, target := range selected {
		if !containsKind(c.Batch.Kinds, target.Kind) {
			c.Batch.Kinds = append(c.Batch.Kinds, target.Kind)
		}
	}
	c.State = "awaiting_delete_confirmation"
	return true
}

func (c *routingConversation) batchEffects(ctx context.Context, s *store.Store) error {
	if c.Batch == nil {
		return nil
	}
	c.Batch.Effects = []string{}
	selected := map[string]bool{}
	for _, target := range c.Batch.Targets {
		selected[target.Kind+":"+target.ID] = true
	}
	count := 0
	for i := range c.Batch.Targets {
		target := &c.Batch.Targets[i]
		if target.Kind != "task" {
			continue
		}
		current, err := s.TaskState(ctx, target.ID)
		if err != nil {
			return err
		}
		target.LinkedReminders = []string{}
		for _, reminder := range current.Reminders {
			if reminder.Status == "scheduled" || reminder.Status == "due" {
				target.LinkedReminders = append(target.LinkedReminders, reminder.ID+":"+reminder.UpdatedAt)
				if !selected["reminder:"+reminder.ID] {
					count++
				}
			}
		}
		sort.Strings(target.LinkedReminders)
	}
	if count > 0 {
		c.Batch.Effects = append(c.Batch.Effects, fmt.Sprintf("%d linked reminder(s) will be cancelled.", count))
	}
	return nil
}

func containsKind(kinds []string, kind string) bool {
	for _, current := range kinds {
		if current == kind {
			return true
		}
	}
	return false
}

func (c *routingConversation) commitBatch(ctx context.Context, s *store.Store) error {
	if c.Batch == nil || len(c.Batch.Targets) == 0 {
		return routing.ErrContract
	}
	if err := s.DeleteBatch(ctx, c.Batch.Targets); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, target := range c.Batch.Targets {
		counts[target.Kind]++
	}
	parts := []string{}
	for _, kind := range c.Batch.Kinds {
		if counts[kind] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
		}
	}
	c.Answer = &conversationAnswer{Text: "Deleted " + strings.Join(parts, " and ") + ".", Sources: []conversationSource{}}
	c.State = "answered"
	return nil
}
