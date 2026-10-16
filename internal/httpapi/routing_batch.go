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
		for _, word := range tokens {
			if word == kind || word == kind+"s" {
				mentions++
			}
			if word == kind+"s" {
				plural = true
			}
		}
	}
	if (quantified && mentions > 0) || plural || (strings.Contains(lower, " and ") && mentions >= 2) {
		return true, nil
	}
	items, err := batchInventory(ctx, s, []string{"task", "reminder", "note"})
	if err != nil {
		return false, err
	}
	return len(exactTitleMatches(lower, items)) >= 2, nil
}

// A local batch route must not discard another action in the same request.
// Send these requests to a visible clarification before either action starts.
func compoundBatchRequest(text string) bool {
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(words) == 0 {
		return false
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
	if i >= len(words) || (words[i] != "delete" && words[i] != "remove" && words[i] != "clear") {
		return false
	}
	// A second clause may be separated by punctuation, not just "and" or
	// "then". Do not route a deletion locally if it would drop that clause.
	secondAction := map[string]bool{"add": true, "create": true, "make": true, "remind": true, "schedule": true, "send": true, "email": true, "edit": true, "update": true, "move": true, "rename": true, "complete": true, "dismiss": true}
	for _, clause := range strings.FieldsFunc(lower, func(r rune) bool { return r == '.' || r == ';' || r == '!' || r == '?' || r == ',' })[1:] {
		parts := strings.Fields(clause)
		for len(parts) > 0 && (parts[0] == "please" || parts[0] == "then" || parts[0] == "and" || parts[0] == "also") {
			parts = parts[1:]
		}
		if len(parts) > 0 && secondAction[parts[0]] {
			return true
		}
	}
	for i := 1; i+1 < len(words); i++ {
		if words[i] != "and" && words[i] != "then" && words[i] != "also" {
			continue
		}
		j := i + 1
		for j < len(words) && (words[j] == "please" || words[j] == "then" || words[j] == "also") {
			j++
		}
		if j < len(words) && secondAction[words[j]] {
			return true
		}
	}
	return false
}

// These are distinct product operations that the conversational action plan
// cannot represent. Recognize only explicit commands; model routing handles
// less direct language and still has an unsupported choice.
func unsupportedWorkflowReason(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	for len(words) > 0 && (words[0] == "please" || words[0] == "hey") {
		words = words[1:]
	}
	if len(words) >= 2 && (words[0] == "can" || words[0] == "could" || words[0] == "would") && words[1] == "you" {
		words = words[2:]
	}
	if len(words) >= 3 && words[0] == "i" && words[1] == "want" && words[2] == "to" {
		words = words[3:]
	}
	if len(words) == 0 {
		return ""
	}
	has := func(targets ...string) bool {
		for _, word := range words {
			for _, target := range targets {
				if word == target {
					return true
				}
			}
		}
		return false
	}
	verb := words[0]
	object := words[1:]
	for len(object) > 0 && (object[0] == "a" || object[0] == "an" || object[0] == "the" || object[0] == "my" || object[0] == "new") {
		object = object[1:]
	}
	if len(object) > 0 && (object[0] == "project" || object[0] == "projects") && (verb == "create" || verb == "add" || verb == "make" || verb == "manage") {
		return "I can't create a project record in Atlas. I can save a task or note about the project."
	}
	if (verb == "send" || verb == "email" || verb == "text" || verb == "post") && has("email", "message", "text", "notification") {
		return "I can't send an email or message from Atlas. I can save a task or reminder to do it."
	}
	if (verb == "link" || verb == "unlink" || verb == "attach" || verb == "detach") && has("task", "tasks", "reminder", "reminders", "note", "notes") {
		return "I can't change links between existing records in this conversation. The direct Atlas API supports note and reminder links."
	}
	if verb == "remove" && has("from") && has("note", "notes", "reminder", "reminders") && has("task", "tasks", "reminder", "reminders") {
		return "I can't remove a link between existing records in this conversation. The direct Atlas API supports note and reminder links."
	}
	if (verb == "relate" || verb == "unrelate" || verb == "add" || verb == "remove") && has("relation", "relations", "relationship", "relationships") {
		return "I can't change record relations in this conversation. The direct Atlas API supports relations."
	}
	if (verb == "add" || verb == "remove" || verb == "make") && has("dependency", "dependencies", "depend", "depends") {
		return "I can't change task dependencies in this conversation. The direct Atlas API supports task dependencies."
	}
	if verb == "make" && has("before") && has("task", "tasks") {
		return "I can't change task dependencies in this conversation. The direct Atlas API supports task dependencies."
	}
	if (verb == "change" || verb == "update" || verb == "edit" || verb == "stop") && has("repeat", "repeating", "recurrence", "recurring") && has("reminder", "reminders") {
		return "I can't change an existing reminder's repeat rule. I can create a new repeating reminder."
	}
	if (verb == "complete" || verb == "finish" || verb == "reopen" || verb == "reschedule" || verb == "edit" || verb == "update") && has("all", "every") && has("task", "tasks", "reminder", "reminders", "note", "notes") {
		return "I can't change multiple saved records in one conversation action. Tell me which record to change first."
	}
	return ""
}

func localUnsupportedRoute(service routingService, name string, input routing.Request, reason string) routing.Result {
	result := localBatchRoute(service, name, input)
	result.State = "unsupported"
	result.SelectedTool = ""
	result.SelectedChannel = ""
	result.Category = "unsupported_workflow"
	result.ExtractionModel = "local_capability_guard"
	result.Plan = &routing.ActionPlan{Action: "unsupported", Clarification: reason}
	result.RoutingToken = service.sign(result)
	return result
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
	result := routing.Result{Version: routing.Version, RegistryVersion: routing.RegistryVersion, RequestID: input.RequestID, Provider: name, Mock: name == "mock", Category: "remove", SelectedTool: "delete_batch", SelectedChannel: "batch_delete", State: "routed", Policy: routing.DefaultPolicy,
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
	if !all && (len(b.Targets) < 2 || duplicateBatchTitle(b.Targets)) {
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

func duplicateBatchTitle(targets []store.BatchTarget) bool {
	seen := map[string]bool{}
	for _, target := range targets {
		title := strings.ToLower(strings.TrimSpace(target.Title))
		if seen[title] {
			return true
		}
		seen[title] = true
	}
	return false
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

func (c *routingConversation) commitBatch(ctx context.Context, s *store.Store, key string) error {
	if c.Batch == nil || len(c.Batch.Targets) == 0 {
		return routing.ErrContract
	}
	if err := s.DeleteBatchRequest(ctx, key, c.Batch.Targets); err != nil {
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
