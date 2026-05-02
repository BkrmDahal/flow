package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
	return `Create or update a planning checklist for the current task. Call this tool at the start of a task to lay out your plan, then call it again as you complete steps to update their status. Each item has an id, content (description), and status (pending, in_progress, or completed). You can pass the full list each time (merge=false to replace all) or pass only changed items (merge=true to update by id).`
}

func (t *TodoWriteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"todos": map[string]interface{}{
				"type":        "array",
				"description": "Array of todo items. Each item has 'id' (unique string), 'content' (description), and 'status' ('pending', 'in_progress', or 'completed').",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Unique identifier for this todo item (e.g. '1', '2', 'setup-db')",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Short description of what needs to be done",
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
