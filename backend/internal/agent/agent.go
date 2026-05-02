package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/llm"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/tools"
)

// maxToolIterations prevents infinite loops in the agent turn.
const maxToolIterations = 50

// agentCodeFileSuffix is appended to the system prompt for agent tasks to
// ensure generated code is always saved as files in the workspace directory.
const agentCodeFileSuffix = `

## Code Output Rules (IMPORTANT)

You MUST always save any code you produce as files using the write_file tool. NEVER just display code in your response without also writing it to a file.

- **Every code snippet** (scripts, configs, HTML, CSS, JSON, YAML, etc.) MUST be saved as a file with an appropriate name and extension.
- If the user asks you to write a program, build something, or generate any code, create the file(s) first using write_file, then explain what you created.
- Use clear, descriptive filenames (e.g. "app.py", "index.html", "schema.sql").
- After writing files, you may still show key parts of the code in your response for explanation, but the file MUST exist.
`

// chatBrevitySuffix keeps replies short for chat mode.
const chatBrevitySuffix = `

## Response Style (IMPORTANT)

Keep your responses **short and to the point**. Be concise — no lengthy explanations, preambles, or unnecessary detail. Answer directly. Use bullet points only when listing multiple items.
`

// todoPromptSuffix instructs the agent to use todo_write for task planning.
const todoPromptSuffix = `

## Task Planning (IMPORTANT)

For any multi-step task, you MUST use the todo_write tool to create a visible plan:

1. **Before doing anything else**, call todo_write with a list of concrete, specific steps (status "pending", first one "in_progress").
2. **As you complete each step**, call todo_write with merge=true to mark it "completed" and the next one "in_progress".
3. **When finished**, ensure all items are "completed".

Keep items short and descriptive. This gives the user real-time visibility into your progress.
`

// Step is one intermediate step recorded during an agent turn.
type Step struct {
	Type      string `json:"type"`                // "thinking", "tool_call", "tool_result"
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
}

// TurnResult is the structured result of a full agent turn.
type TurnResult struct {
	Steps     []Step `json:"steps"`
	FinalText string `json:"final_text"`
}

// Deps bundles the dependencies an agent turn needs.
type Deps struct {
	SessionMgr   *session.Manager
	LLMClient    llm.LLMClient
	ToolRegistry *tools.Registry
	WorkDir      string  // per-session working directory
	BaseDir      string  // ~/.flow/
	ChatMode     bool    // true for chat sessions (concise), false for agent tasks (detailed)
	CommandBody  string  // optional plugin command context
}

// StreamEvent is emitted during a streaming turn.
type StreamEvent struct {
	Type      string `json:"type"`                // "thinking_start"|"thinking"|"text"|"tool_call"|"tool_result"|"todo_update"|"skill_used"|"file_created"|"done"|"error"
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
	Path      string `json:"path,omitempty"`   // for file_created
	Name      string `json:"name,omitempty"`   // for file_created (basename)
	// TodoItems carries the full todo list snapshot for "todo_update" events.
	TodoItems []tools.TodoItem `json:"todo_items,omitempty"`
}

// RunTurnStream executes one agent turn end-to-end with streaming.
// It accepts either a plain text string or a pre-marshalled json.RawMessage as user content.
// Use userContent for multimodal messages; pass nil to use userText.
func RunTurnStream(ctx context.Context, sessionID, systemPrompt, userText string, deps Deps, emit func(StreamEvent)) (*TurnResult, error) {
	return runStreamInternal(ctx, sessionID, systemPrompt, nil, userText, deps, emit)
}

// RunTurnStreamWithContent is like RunTurnStream but accepts pre-marshalled multimodal content.
func RunTurnStreamWithContent(ctx context.Context, sessionID, systemPrompt string, userContent json.RawMessage, deps Deps, emit func(StreamEvent)) (*TurnResult, error) {
	return runStreamInternal(ctx, sessionID, systemPrompt, userContent, "", deps, emit)
}

func runStreamInternal(ctx context.Context, sessionID, systemPrompt string, userContent json.RawMessage, userText string, deps Deps, emit func(StreamEvent)) (*TurnResult, error) {
	if deps.LLMClient == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}
	if deps.SessionMgr == nil {
		return nil, fmt.Errorf("session manager not configured")
	}

	// Determine workspace directory.
	workDir := deps.WorkDir
	if workDir == "" && deps.BaseDir != "" {
		workDir = filepath.Join(deps.BaseDir, "sessions", sessionID)
	}
	if workDir != "" {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			log.Printf("[agent] mkdir workdir: %v", err)
		}
		ctx = tools.WithSessionDir(ctx, workDir)
	}

	// Build system prompt if not supplied.
	if systemPrompt == "" && deps.BaseDir != "" {
		systemPrompt = buildSystemPrompt(deps)
	} else if deps.BaseDir != "" {
		// Inject memory + skill indices even if system prompt is supplied.
		systemPrompt += buildMemoryIndex(deps.BaseDir)
		systemPrompt += buildSkillIndex(deps.BaseDir)
	}

	if deps.CommandBody != "" {
		systemPrompt += "\n\n## Active Command\n\nThe user invoked a custom command. Follow the instructions below:\n\n" + deps.CommandBody + "\n"
	}

	if deps.ChatMode {
		systemPrompt += chatBrevitySuffix
	} else {
		systemPrompt += todoPromptSuffix
	}
	systemPrompt += agentCodeFileSuffix

	// Wire up todo_write callback for real-time progress updates.
	ctx = tools.WithTodoCallback(ctx, func(items []tools.TodoItem) {
		emit(StreamEvent{
			Type:      "todo_update",
			TodoItems: items,
		})
	})

	deps.SessionMgr.Lock(sessionID)
	defer deps.SessionMgr.Unlock(sessionID)

	history, err := deps.SessionMgr.Load(sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	history, synth := repairDanglingToolUse(history)
	if len(synth) > 0 {
		log.Printf("[agent] repaired %d dangling tool_use block(s) in %s", len(synth), sessionID)
		if err := deps.SessionMgr.Append(sessionID, synth...); err != nil {
			log.Printf("[agent] persist repair: %v", err)
		}
	}

	// Build user message content.
	if userContent == nil {
		var err error
		userContent, err = json.Marshal(userText)
		if err != nil {
			return nil, fmt.Errorf("encode user text: %w", err)
		}
	}
	userMsg := session.Message{Role: "user", Content: userContent}
	history = append(history, userMsg)
	if err := deps.SessionMgr.Append(sessionID, userMsg); err != nil {
		return nil, fmt.Errorf("append user msg: %w", err)
	}

	toolDefs := deps.ToolRegistry.AllDefs()
	result := &TurnResult{}

	for i := 0; i < maxToolIterations; i++ {
		onDelta := func(delta llm.StreamDelta) {
			switch delta.Type {
			case "thinking_start":
				emit(StreamEvent{Type: "thinking_start"})
			case "thinking":
				emit(StreamEvent{Type: "thinking", Content: delta.Content})
			case "text":
				if delta.Content != "" {
					emit(StreamEvent{Type: "text", Content: delta.Content})
				}
			}
		}

		agentCfg := config.AgentConfig{}
		resp, err := deps.LLMClient.SendMessagesStream(ctx, systemPrompt, history, toolDefs, agentCfg.EnableThinking, onDelta)
		if err != nil {
			emit(StreamEvent{Type: "error", Content: err.Error()})
			return nil, fmt.Errorf("llm: %w", err)
		}

		// Collect thinking steps.
		if thinking := resp.ThinkingContent(); thinking != "" {
			result.Steps = append(result.Steps, Step{
				Type:    "thinking",
				Content: thinking,
			})
		}

		assistantMsg := session.Message{
			Role:    "assistant",
			Content: resp.ContentToRaw(),
		}
		history = append(history, assistantMsg)
		if err := deps.SessionMgr.Append(sessionID, assistantMsg); err != nil {
			return nil, fmt.Errorf("append assistant msg: %w", err)
		}

		if !resp.HasToolUse() {
			result.FinalText = resp.TextContent()
			return result, nil
		}

		var toolResults []map[string]interface{}
		for _, tb := range resp.ToolUseBlocks() {
			result.Steps = append(result.Steps, Step{
				Type:      "tool_call",
				ToolName:  tb.Name,
				ToolInput: string(tb.Input),
			})
			emit(StreamEvent{Type: "tool_call", ToolName: tb.Name, ToolInput: string(tb.Input)})

			toolOutput, err := deps.ToolRegistry.Execute(ctx, tb.Name, tb.Input)
			if err != nil {
				toolOutput = fmt.Sprintf("Tool execution error: %v", err)
			}

			truncated := truncate(toolOutput, 2000)
			result.Steps = append(result.Steps, Step{
				Type:     "tool_result",
				Content:  truncated,
				ToolName: tb.Name,
			})
			emit(StreamEvent{Type: "tool_result", ToolName: tb.Name, Content: truncated})

			// Emit skill_used event when use_skill is called successfully.
			if tb.Name == "use_skill" && !strings.HasPrefix(toolOutput, "Error") {
				var skillInput struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(tb.Input, &skillInput) == nil && skillInput.Name != "" {
					emit(StreamEvent{Type: "skill_used", Content: skillInput.Name})
				}
			}

			// Emit file_created events for file-writing tools.
			var createdFilePath string
			switch tb.Name {
			case "write_file":
				var fi struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(tb.Input, &fi) == nil {
					createdFilePath = fi.Path
				}
			}
			if createdFilePath != "" {
				resolvedPath := createdFilePath
				if workDir != "" && !filepath.IsAbs(resolvedPath) {
					resolvedPath = filepath.Join(workDir, filepath.Clean(resolvedPath))
				}
				emit(StreamEvent{
					Type:    "file_created",
					Content: resolvedPath,
					Path:    resolvedPath,
					Name:    filepath.Base(resolvedPath),
				})
			}

			// Auto-save execute_code results.
			if tb.Name == "run_bash" && workDir != "" &&
				!strings.HasPrefix(toolOutput, "Exit code") &&
				!strings.HasPrefix(toolOutput, "Error") {
				var codeInput struct {
					Language string `json:"language"`
					Code     string `json:"code"`
					Filename string `json:"filename"`
				}
				if json.Unmarshal(tb.Input, &codeInput) == nil && codeInput.Code != "" {
					filename := codeInput.Filename
					if filename == "" {
						ext := ".txt"
						switch codeInput.Language {
						case "python":
							ext = ".py"
						case "javascript":
							ext = ".js"
						case "go":
							ext = ".go"
						}
						filename = fmt.Sprintf("script_%d%s", time.Now().Unix(), ext)
					}
					savePath := filepath.Join(workDir, filepath.Clean(filename))
					if err := os.WriteFile(savePath, []byte(codeInput.Code), 0o644); err == nil {
						emit(StreamEvent{
							Type:    "file_created",
							Content: savePath,
							Path:    savePath,
							Name:    filepath.Base(savePath),
						})
					}
				}
			}

			toolResults = append(toolResults, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": tb.ID,
				"content":     toolOutput,
			})
		}

		raw, err := json.Marshal(toolResults)
		if err != nil {
			return nil, fmt.Errorf("marshal tool results: %w", err)
		}
		toolResultMsg := session.Message{Role: "user", Content: raw}
		history = append(history, toolResultMsg)
		if err := deps.SessionMgr.Append(sessionID, toolResultMsg); err != nil {
			return nil, fmt.Errorf("append tool result msg: %w", err)
		}
	}

	emit(StreamEvent{Type: "error", Content: fmt.Sprintf("agent exceeded %d tool iterations", maxToolIterations)})
	return result, fmt.Errorf("agent exceeded %d tool iterations", maxToolIterations)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}

func extractWritePath(input json.RawMessage, workDir string) string {
	var w struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(input, &w) != nil || w.Path == "" {
		return ""
	}
	return filepath.Join(workDir, filepath.Clean(w.Path))
}

// buildSystemPrompt loads the master prompt file and injects memory + skill indices.
func buildSystemPrompt(deps Deps) string {
	promptPath := ""
	if deps.BaseDir != "" {
		promptPath = filepath.Join(deps.BaseDir, "workspace", "Master_prompt.md")
	}
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return "You are a helpful assistant."
	}
	prompt := string(data)
	prompt += buildMemoryIndex(deps.BaseDir)
	prompt += buildSkillIndex(deps.BaseDir)
	return prompt
}

// buildMemoryIndex scans the memory directory and returns a lightweight index.
func buildMemoryIndex(baseDir string) string {
	memDir := filepath.Join(baseDir, "memory")
	pattern := filepath.Join(memDir, "*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}

	var lines []string
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		modTime := info.ModTime().Format(time.DateOnly)
		lines = append(lines, fmt.Sprintf("- %s (updated %s)", name, modTime))
	}

	if len(lines) == 0 {
		return ""
	}
	return "\n\n## Available Memories\nThe following memories are stored. Use `memory_search` or `list_memories` to retrieve details.\n" + strings.Join(lines, "\n") + "\n"
}

// skillMeta holds lightweight skill metadata for the skill index.
type skillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// buildSkillIndex reads plugin skill metadata from plugins.json.
func buildSkillIndex(baseDir string) string {
	pluginsPath := filepath.Join(baseDir, "plugins.json")
	data, err := os.ReadFile(pluginsPath)
	if err != nil {
		return ""
	}

	var pd struct {
		Skills []skillMeta `json:"skills"`
	}
	if json.Unmarshal(data, &pd) != nil || len(pd.Skills) == 0 {
		return ""
	}

	var lines []string
	for _, sk := range pd.Skills {
		desc := sk.Description
		if desc == "" {
			desc = "No description"
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %s", sk.Name, desc))
	}

	return "\n\n## Available Skills\n\nThe following skills provide specialized instructions for specific tasks. When the user's request matches a skill, use the `use_skill` tool to load it before proceeding.\n\n" + strings.Join(lines, "\n") + "\n"
}
