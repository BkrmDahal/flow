package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoItem represents a single item in the agent's planning checklist.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// TodoCallback is called whenever the todo list is updated.
type TodoCallback func(items []TodoItem)

type todoCallbackKey struct{}

// WithTodoCallback returns a context that carries a callback for todo updates.
func WithTodoCallback(ctx context.Context, cb TodoCallback) context.Context {
	return context.WithValue(ctx, todoCallbackKey{}, cb)
}

// TodoCallbackFromContext extracts the todo callback from the context.
func TodoCallbackFromContext(ctx context.Context) TodoCallback {
	cb, _ := ctx.Value(todoCallbackKey{}).(TodoCallback)
	return cb
}

// blockedTodoContent is a set of tool names and low-effort strings that
// must not appear as the entire content of a todo item. The agent should
// write high-level goals, not tool names.
var blockedTodoContent = map[string]bool{
	"web search":     true,
	"web_search":     true,
	"fetch url":      true,
	"fetch_url":      true,
	"read file":      true,
	"read_file":      true,
	"write file":     true,
	"write_file":     true,
	"run bash":       true,
	"run_bash":       true,
	"save memory":    true,
	"save_memory":    true,
	"memory search":  true,
	"memory_search":  true,
	"list memories":  true,
	"list_memories":  true,
	"delete memory":  true,
	"delete_memory":  true,
	"capture screen": true,
	"capture_screen": true,
	"use skill":      true,
	"use_skill":      true,
	"todo write":     true,
	"todo_write":     true,
}

// TodoWriteTool allows the agent to create and update a planning checklist.
type TodoWriteTool struct {
	mu    sync.Mutex
	items []TodoItem
}

func NewTodoWriteTool() *TodoWriteTool {
	return &TodoWriteTool{}
}

func (t *TodoWriteTool) Name() string { return "todo_write" }

func (t *TodoWriteTool) Description() string {
	return `Create or update a planning checklist visible to the user. Each item must be a concise, plain-language GOAL describing WHAT you want to achieve — NOT a tool name.

GOOD items: "Research NY tax brackets", "Build the invoice PDF", "Verify output is correct"
BAD items (REJECTED): "web search", "fetch url", "run_bash", "read_file"

Each item has an id, content (goal description), and status (pending, in_progress, completed).
Pass merge=false to replace the full list, or merge=true to update specific items by id.
Skip this tool entirely for simple single-step tasks.`
}

func (t *TodoWriteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"todos": map[string]interface{}{
				"type":        "array",
				"description": "Array of todo items. Each item has 'id' (unique string), 'content' (high-level goal description — NOT a tool name), and 'status' ('pending', 'in_progress', or 'completed').",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Unique identifier for this todo item (e.g. '1', '2', 'setup-db')",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "High-level goal in plain language (e.g. 'Research tax brackets'). Must NOT be a tool name like 'web search' or 'run_bash'.",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "Current status of this item",
						},
					},
					"required": []string{"id", "content", "status"},
				},
			},
			"merge": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, merge items by id (update existing, add new). If false, replace the entire list. Default: false.",
			},
		},
		"required": []string{"todos"},
	}
}

type todoWriteInput struct {
	Todos []TodoItem `json:"todos"`
	Merge bool       `json:"merge"`
}

func (t *TodoWriteTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in todoWriteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	if len(in.Todos) == 0 {
		return "Error: at least one todo item is required", nil
	}

	for _, item := range in.Todos {
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return fmt.Sprintf("Error: invalid status %q for item %q — must be pending, in_progress, or completed", item.Status, item.ID), nil
		}

		// Reject items that are just tool names or very short non-descriptive strings.
		normalized := strings.ToLower(strings.TrimSpace(item.Content))
		if blockedTodoContent[normalized] {
			return fmt.Sprintf(
				"Error: todo item %q has content %q which is just a tool name. "+
					"Plan items must be high-level goals in plain language that describe WHAT you want to achieve. "+
					"Example: instead of 'web search', write 'Research current NY state tax brackets'. "+
					"Please rewrite your plan with descriptive goals and try again.",
				item.ID, item.Content), nil
		}
		// Also reject items shorter than 5 chars (too terse to be a real goal).
		if len(normalized) < 5 && item.Status != "completed" {
			return fmt.Sprintf(
				"Error: todo item %q has content %q which is too short to be a meaningful goal. "+
					"Write a clear, descriptive plan step. Example: 'Calculate federal tax on $200k income'.",
				item.ID, item.Content), nil
		}
	}

	t.mu.Lock()
	if in.Merge {
		idxMap := make(map[string]int, len(t.items))
		for i, item := range t.items {
			idxMap[item.ID] = i
		}
		for _, newItem := range in.Todos {
			if idx, ok := idxMap[newItem.ID]; ok {
				if newItem.Content != "" {
					t.items[idx].Content = newItem.Content
				}
				t.items[idx].Status = newItem.Status
			} else {
				t.items = append(t.items, newItem)
			}
		}
	} else {
		t.items = make([]TodoItem, len(in.Todos))
		copy(t.items, in.Todos)
	}

	snapshot := make([]TodoItem, len(t.items))
	copy(snapshot, t.items)
	t.mu.Unlock()

	if cb := TodoCallbackFromContext(ctx); cb != nil {
		cb(snapshot)
	}

	pending, inProgress, completed := 0, 0, 0
	for _, item := range snapshot {
		switch item.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	return fmt.Sprintf("Todo list updated: %d total (%d pending, %d in progress, %d completed)",
		len(snapshot), pending, inProgress, completed), nil
}

// Items returns a copy of the current todo items.
func (t *TodoWriteTool) Items() []TodoItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	snapshot := make([]TodoItem, len(t.items))
	copy(snapshot, t.items)
	return snapshot
}

// Reset clears the todo list.
func (t *TodoWriteTool) Reset() {
	t.mu.Lock()
	t.items = nil
	t.mu.Unlock()
}
