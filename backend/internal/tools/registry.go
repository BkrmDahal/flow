package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/user/flow/backend/internal/llm"
)

// Tool is the interface every agent tool implements.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]interface{}
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds the registered tools and is shared across sessions.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// Execute runs a named tool. A missing tool is reported as a string error
// (not a Go error) so it can be fed back to the model as a tool_result.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	t := r.Get(name)
	if t == nil {
		return fmt.Sprintf("Error: unknown tool %q", name), nil
	}
	return t.Execute(ctx, input)
}

// AllDefs returns the JSON-schema definitions for every registered tool.
func (r *Registry) AllDefs() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return defs
}

// RegisterStandard wires the v1 Cowork tools (read_file, write_file, run_bash).
// Kept for backward compatibility with the Cowork tab.
func RegisterStandard(r *Registry) {
	r.Register(&ReadFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(NewRunBashTool(""))
}

// RegisterStandardTools wires all standard tools including memory, todo, and skill.
// baseDir is ~/.flow/
func RegisterStandardTools(r *Registry, baseDir string) {
	r.Register(&ReadFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(NewRunBashTool(baseDir))

	memoryDir := filepath.Join(baseDir, "memory")
	r.Register(NewSaveMemoryTool(memoryDir))
	r.Register(NewMemorySearchTool(memoryDir))
	r.Register(NewListMemoriesTool(memoryDir))
	r.Register(NewDeleteMemoryTool(memoryDir))

	r.Register(NewTodoWriteTool())
	r.Register(NewUseSkillTool(baseDir))
}

