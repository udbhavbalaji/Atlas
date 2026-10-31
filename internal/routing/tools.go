package routing

// ToolDefinition describes a capability Jev may select. Arguments are a
// proposal: the channel and store still validate and authorize execution.
type ToolDefinition struct {
	ID          string         `json:"id"`
	Category    string         `json:"category"`
	Channel     string         `json:"channel"`
	Description string         `json:"description"`
	ReadOnly    bool           `json:"read_only"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolCriteria struct {
	id, category, channel, description string
	action, kind                       string
	fields                             []string
	readOnly                           bool
}

var toolRegistry = []toolCriteria{
	{"task", "create", "tasks", "Create a task or event; this tool can also save a linked note and reminder in one operation.", "task", "task", []string{"action", "kind", "title", "details", "due_at", "reminder_at", "note_body", "repeat"}, false},
	{"reminder", "create", "reminders", "Create a reminder, optionally with an attached note or a link to an existing task.", "reminder", "reminder", []string{"action", "kind", "title", "reminder_at", "linked_task_id", "note_body", "repeat"}, false},
	{"note", "create", "notes", "Save a standalone note or a note linked to an existing task.", "note", "note", []string{"action", "kind", "note_body", "linked_task_id"}, false},
	{"lookup_task", "read", "lookup", "Read task information, including linked notes and reminders.", "lookup", "task", []string{"action", "kind", "target_id", "target_query"}, true},
	{"lookup_reminder", "read", "lookup", "Read reminders, including old, completed, and dismissed reminders.", "lookup", "reminder", []string{"action", "kind", "target_id", "target_query"}, true},
	{"lookup_note", "read", "lookup", "Read notes and their task links.", "lookup", "note", []string{"action", "kind", "target_id", "target_query"}, true},
	{"lookup_all", "read", "lookup", "Read across tasks, reminders, and notes.", "lookup", "", []string{"action", "kind", "target_id", "target_query"}, true},
	{"edit_task", "change", "edit", "Edit an existing task, including a recently discussed task and related records when the store's workflow requires it.", "edit", "task", []string{"action", "kind", "target_id", "target_query", "field", "value"}, false},
	{"edit_reminder", "change", "edit", "Edit an existing reminder, including its repeat rule or a recently discussed reminder.", "edit", "reminder", []string{"action", "kind", "target_id", "target_query", "field", "value"}, false},
	{"edit_note", "change", "edit", "Edit an existing note, including a recently discussed note.", "edit", "note", []string{"action", "kind", "target_id", "target_query", "field", "value"}, false},
	{"delete_task", "remove", "delete", "Delete an existing task, including a recently discussed task.", "delete", "task", []string{"action", "kind", "target_id", "target_query"}, false},
	{"delete_reminder", "remove", "delete", "Delete an existing reminder, including a recently discussed reminder.", "delete", "reminder", []string{"action", "kind", "target_id", "target_query"}, false},
	{"delete_note", "remove", "delete", "Delete an existing note, including a recently discussed note.", "delete", "note", []string{"action", "kind", "target_id", "target_query"}, false},
}

// Tools returns a fresh snapshot so callers cannot change the registry.
func Tools() []ToolDefinition {
	out := make([]ToolDefinition, 0, len(toolRegistry))
	for _, spec := range toolRegistry {
		out = append(out, toolDefinition(spec))
	}
	return out
}

func Tool(id string) (ToolDefinition, bool) {
	for _, spec := range toolRegistry {
		if spec.id == id {
			return toolDefinition(spec), true
		}
	}
	return ToolDefinition{}, false
}

func toolDefinition(spec toolCriteria) ToolDefinition {
	return ToolDefinition{ID: spec.id, Category: spec.category, Channel: spec.channel, Description: spec.description, ReadOnly: spec.readOnly, InputSchema: toolSchema(spec)}
}

func toolSchema(spec toolCriteria) map[string]any {
	base := planSchema()["properties"].(map[string]any)
	properties := make(map[string]any, len(spec.fields))
	for _, field := range spec.fields {
		properties[field] = base[field]
	}
	properties["action"] = map[string]any{"type": "string", "enum": []string{spec.action}}
	if spec.kind == "" {
		properties["kind"] = map[string]any{"type": "string", "enum": []string{"", "task", "reminder", "note"}}
	} else {
		properties["kind"] = map[string]any{"type": "string", "enum": []string{spec.kind}}
	}
	if spec.action == "edit" {
		allowed := map[string][]string{
			"task":     {"title", "details", "due_at", "status"},
			"reminder": {"title", "scheduled_at", "status", "repeat"},
			"note":     {"title", "body"},
		}
		properties["field"] = map[string]any{"type": "string", "enum": append([]string{""}, allowed[spec.kind]...)}
	}
	return map[string]any{"type": "object", "properties": properties, "required": append([]string(nil), spec.fields...), "additionalProperties": false}
}

// ToolChoices is the Jev choice set for one category. Clarify remains a choice
// rather than an executable tool.
func ToolChoices(category string) []Action {
	out := []Action{}
	for _, spec := range toolRegistry {
		if spec.category == category {
			out = append(out, Action{ID: spec.id, Description: spec.description})
		}
	}
	if len(out) > 0 {
		prompt := "The record type is genuinely unclear."
		if category == "change" || category == "remove" {
			prompt = "The target record type is genuinely unclear."
		} else if category == "read" {
			prompt = "The requested information is genuinely unclear."
		}
		out = append(out, Action{ID: "clarify", Description: prompt})
	}
	return out
}

// ToolPlanMatches ensures an extractor cannot substitute a different action
// or record kind for the tool Jev selected.
func ToolPlanMatches(id string, plan ActionPlan) bool {
	for _, spec := range toolRegistry {
		if spec.id == id {
			if plan.Action != spec.action {
				return false
			}
			if spec.kind == "" {
				return plan.Kind == "" || plan.Kind == "task" || plan.Kind == "reminder" || plan.Kind == "note"
			}
			if plan.Kind == spec.kind {
				return true
			}
			return plan.Kind == "" && (spec.action == "task" || spec.action == "reminder" || spec.action == "note" || spec.action == "lookup")
		}
	}
	return false
}
