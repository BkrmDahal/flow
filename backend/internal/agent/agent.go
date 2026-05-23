package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/llm"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/tools"
)

// maxToolIterations prevents infinite loops in the agent turn.
const maxToolIterations = 50

// maxWebSearchPerTurn limits how many web_search/fetch_url calls the agent
// can make in a single turn before we inject a "use your knowledge" nudge.
const maxWebSearchPerTurn = 3

// minIterationInterval is the minimum delay between consecutive LLM API
// calls within one turn. This rate-limits prompt-injection attempts that
// try to burn through API credits via rapid tool-call loops.
const minIterationInterval = 500 * time.Millisecond

// ── Prompt Section Constants ──
// These are composable sections injected by the structured prompt builder.
// They are NOT appended blindly — the builder selects which sections to
// include based on ChatMode and other Deps fields.

const sectionCodeOutput = `
## Code Output

Use the write_file tool when the user is asking you to **build, save, or modify** something they'll run or keep — scripts, projects, config they want on disk. Pick clear filenames (e.g. "app.py", "index.html").

For conversational answers, demonstration snippets, examples in an explanation, or short edits the user just wants to read — just include the code inline in your response. Don't materialize a file every time you mention code.
`

const sectionBrevity = `
## Response Style (IMPORTANT)

Keep your responses **short and to the point**. Be concise — no lengthy explanations, preambles, or unnecessary detail. Answer directly. Use bullet points only when listing multiple items.
`

const sectionPlanning = `
## Task Planning (CRITICAL — READ CAREFULLY)

For ANY task that requires more than one step, you MUST call todo_write FIRST — before ANY other tool call.

### Rules:
1. **Plan FIRST.** Your very first tool call in any multi-step task MUST be todo_write. Do NOT call web_search, read_file, run_bash, or any other tool before the plan exists.
2. **Write GOALS, not tool names.** Each plan item describes WHAT you want to achieve in plain language the user can understand. NEVER write a tool name as a plan item.
3. **Update as you go.** After completing each step, call todo_write with merge=true to mark it completed and the next item in_progress.

### Examples:

✅ GOOD plan (high-level goals):
- "Research current NY state tax brackets for married couples"
- "Calculate federal income tax on $200k"
- "Calculate NY state tax (excluding city tax)"
- "Summarize total tax liability"

❌ BAD plan (these are just tool names — NEVER do this):
- "web search"
- "fetch url"
- "run_bash"
- "read_file"
- "write_file"

✅ GOOD plan:
- "Create the invoice template"
- "Convert invoice to PDF using Python"
- "Verify the PDF output is correct"

❌ BAD plan:
- "call write_file to save template"
- "run python script"
- "use read_file to check output"

The user sees this plan. They need to understand WHAT you are doing and WHY, not WHICH tool you are calling.
`


const sectionOptions = `
## Clarifying Questions & Options (IMPORTANT)
Avoid asking too many clarifying questions. Be proactive: make safe, reasonable assumptions whenever possible to keep moving forward. Only ask questions when you are genuinely blocked or need the user to make a key choice.
When you DO need clarification, a decision, or to present choices to the user, ALWAYS provide 2-4 explicit, concise option suggestions that the user can choose from.
Format these options at the very end of your response inside an <options> block, with one option per line prefixed by a bullet (-).
Example:
Would you like to keep them in the same folder or move them?
<options>
- Keep them in the same folder
- Move them to ~/Downloads/code_demo/screenshots/
- Rename them to screenshot_1.png, screenshot_2.png, ...
</options>
Keep options extremely short, clear, and action-oriented. Do not include markdown formatting inside option lines.
`

const sectionSafety = `
## Safety Guardrails

- NEVER delete files outside the session workspace without explicit user permission.
- NEVER install global packages without asking.
- NEVER modify system files (/etc, /System, /Library, /usr).
- NEVER run commands that require sudo without asking.
- NEVER expose API keys or credentials in output.
- NEVER read from ~/.ssh, ~/.aws, ~/.kube, ~/.gnupg, or similar sensitive directories.
`

const sectionMultimodal = `
## Multimodal & Vision Capabilities (CRITICAL)

You have native multimodal and vision capabilities, allowing you to directly see, describe, and transcribe any image, screenshot, or document attached to the chat history.

### Rules:
1. **Use native vision directly.** When a user asks you to describe, OCR, or transcribe an attached image or screenshot, always perform the task directly using your native vision capabilities.
2. **Do NOT use workspace files or write code/scripts** (like Tesseract, EasyOCR, or Pillow scripts) to read or OCR an image when it is already visible in the chat itself. Doing so is extremely redundant, slow, and unnecessary. Only use local OCR scripts if specifically requested by the user to build/test a local script for their own use outside this chat.
`

// buildToolGuidance returns the tool usage tips section, omitting entries
// for any tools in the disabled set so the LLM doesn't even know they exist.
func buildToolGuidance(disabledSet map[string]bool) string {
	var sb strings.Builder
	sb.WriteString("\n## Tool Usage Tips\n\n")
	sb.WriteString("- **Use your knowledge FIRST.** You already know common facts like tax brackets, programming languages, math formulas, geography, history, science, etc. Do NOT search for things you already know. Only search when you genuinely need very recent data, specific URLs, or niche information you are unsure about.\n")
	sb.WriteString("- **Limit tool calls.** Prefer fewer, targeted tool calls over many speculative ones. If a search returns no results, do NOT keep searching — use your training knowledge instead.\n")
	sb.WriteString("- **run_bash**: Prefer single-line commands. Chain with && for multi-step. Check exit codes. For Python, prefer 'python3 -c \"...\"' for one-liners. Commands run in a macOS sandbox with restricted write access and are killed after 60s. When writing output files, save them in the current working directory (the session workspace root).\n")
	sb.WriteString("- **write_file**: Save **final deliverables** (reports, documents, outputs the user asked for — any format: .md, .html, .xlsx, .pdf, .csv, .txt, .py, .js, etc.) directly in the workspace root (e.g. 'report.md', 'script.py', 'app.js'). Save **intermediate/scratch files** (helper scripts, temp data, build scripts) under '.scratch/' (e.g. '.scratch/build.py'). Parent directories are created automatically. For edits, read first then write back.\n")
	sb.WriteString("- **read_file**: Always read a file before attempting to overwrite it. Relative paths resolve within the workspace; absolute paths also work for reading external files.\n")

	if !disabledSet["web_search"] && !disabledSet["fetch_url"] {
		sb.WriteString("- **web_search / fetch_url**: ONLY use when you genuinely need current or niche information you don't already know. For common knowledge (tax rates, formulas, code syntax, etc.), just answer directly from your training data. Maximum 3 searches per task — after that, use your knowledge.\n")
	}
	if !disabledSet["capture_screen"] {
		sb.WriteString("- **capture_screen**: Use when the user asks about what they see on screen.\n")
	}

	sb.WriteString("- **save_memory / memory_search**: Use to persist and recall important information across sessions.\n")
	sb.WriteString("- **todo_write**: Use to create and track task plans visible to the user.\n")
	sb.WriteString("- **use_skill**: Load specialized instructions for specific task types.\n")
	return sb.String()
}

// Step is one intermediate step recorded during an agent turn.
type Step struct {
	Type      string `json:"type"` // "thinking", "tool_call", "tool_result"
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
	SessionMgr    *session.Manager
	LLMClient     llm.LLMClient
	ToolRegistry  *tools.Registry
	WorkDir       string   // per-session working directory
	BaseDir       string   // ~/.flow/
	ChatMode      bool     // true for chat sessions (concise), false for agent tasks (detailed)
	CommandBody   string   // optional plugin command context
	DisabledTools []string // tool names that are toggled off (e.g. "web_search", "fetch_url")
}

// StreamEvent is emitted during a streaming turn.
type StreamEvent struct {
	Type      string `json:"type"` // "thinking_start"|"thinking"|"text"|"tool_call"|"tool_result"|"todo_update"|"skill_used"|"file_created"|"done"|"error"
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
	Path      string `json:"path,omitempty"` // for file_created
	Name      string `json:"name,omitempty"` // for file_created (basename)
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

	// Build system prompt using the structured builder.
	systemPrompt = buildFullSystemPrompt(systemPrompt, deps)

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

	// Filter out disabled tools from definitions sent to the model.
	allDefs := deps.ToolRegistry.AllDefs()
	disabledSet := make(map[string]bool, len(deps.DisabledTools))
	for _, name := range deps.DisabledTools {
		disabledSet[name] = true
	}
	var toolDefs []llm.ToolDef
	for _, td := range allDefs {
		if !disabledSet[td.Name] {
			toolDefs = append(toolDefs, td)
		}
	}

	result := &TurnResult{}
	webSearchCount := 0 // tracks web_search + fetch_url calls for rate limiting
	turnStart := time.Now()
	totalToolCalls := 0

	// ── Turn Start Log ──
	modelName := deps.LLMClient.GetModel()
	enabledToolNames := make([]string, 0, len(toolDefs))
	for _, td := range toolDefs {
		enabledToolNames = append(enabledToolNames, td.Name)
	}
	log.Printf("[agent] ── TURN START ── session=%s model=%s prompt_size=%d history_msgs=%d tools=%d(%v) disabled=%v",
		sessionID, modelName, len(systemPrompt), len(history), len(toolDefs), enabledToolNames, deps.DisabledTools)

	for i := 0; i < maxToolIterations; i++ {
		// Rate-limit consecutive API calls to prevent credit burn.
		if i > 0 {
			select {
			case <-time.After(minIterationInterval):
			case <-ctx.Done():
				return result, ctx.Err()
			}
		}
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
		if deps.BaseDir != "" {
			if cfg, err := config.Load(deps.BaseDir); err == nil && cfg != nil {
				if aCfg, ok := cfg.Agents["main"]; ok {
					agentCfg = aCfg
				}
			}
		}
		llmStart := time.Now()
		log.Printf("[agent] ── LLM CALL ── session=%s iter=%d/%d history_msgs=%d model=%s",
			sessionID, i+1, maxToolIterations, len(history), modelName)

		resp, err := deps.LLMClient.SendMessagesStream(ctx, systemPrompt, history, toolDefs, agentCfg.EnableThinking, onDelta)
		llmDuration := time.Since(llmStart)
		if err != nil {
			log.Printf("[agent] ── LLM ERROR ── session=%s iter=%d err=%v elapsed=%s",
				sessionID, i+1, err, llmDuration)
			emit(StreamEvent{Type: "error", Content: err.Error()})
			return nil, fmt.Errorf("llm: %w", err)
		}
		var toolNames []string
		for _, tb := range resp.ToolUseBlocks() {
			toolNames = append(toolNames, tb.Name)
		}
		toolsLog := ""
		if len(toolNames) > 0 {
			toolsLog = " tools=" + strings.Join(toolNames, ",")
		}
		log.Printf("[agent] ── LLM RESP ── session=%s iter=%d elapsed=%s content_blocks=%d has_tool_use=%v%s",
			sessionID, i+1, llmDuration, len(resp.Content), resp.HasToolUse(), toolsLog)
		if len(resp.Content) == 0 {
			log.Printf("[agent] streaming response was empty for %s; retrying without stream", sessionID)
			resp, err = deps.LLMClient.SendMessages(ctx, systemPrompt, history, toolDefs, agentCfg.EnableThinking)
			if err != nil {
				emit(StreamEvent{Type: "error", Content: err.Error()})
				return nil, fmt.Errorf("llm fallback: %w", err)
			}
			if len(resp.Content) == 0 {
				err := fmt.Errorf("model returned an empty response")
				emit(StreamEvent{Type: "error", Content: err.Error()})
				return nil, err
			}
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

			// Scan workspace for deliverable files and emit file_created events.
			// This catches output files created by bash scripts (xlsx, pdf, csv, etc.)
			// that aren't tracked by write_file's file_created event.
			if workDir != "" {
				emitDeliverableFiles(workDir, emit)
			}

			log.Printf("[agent] ── TURN END ── session=%s iterations=%d tool_calls=%d final_len=%d elapsed=%s",
				sessionID, i+1, totalToolCalls, len(result.FinalText), time.Since(turnStart))
			return result, nil
		}

		var toolResults []map[string]interface{}
		for _, tb := range resp.ToolUseBlocks() {
			// ── Disabled tools: silently reject without emitting UI events ──
			if disabledSet[tb.Name] {
				log.Printf("[agent]   tool BLOCKED (disabled) name=%s", tb.Name)
				toolResults = append(toolResults, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": tb.ID,
					"content":     "This tool is not available. Proceed without it and use your own knowledge.",
				})
				continue
			}

			// Record step and emit event for visible tools only.
			totalToolCalls++
			log.Printf("[agent]   tool CALL #%d name=%s input_size=%d",
				totalToolCalls, tb.Name, len(tb.Input))
			result.Steps = append(result.Steps, Step{
				Type:      "tool_call",
				ToolName:  tb.Name,
				ToolInput: string(tb.Input),
			})
			emit(StreamEvent{Type: "tool_call", ToolName: tb.Name, ToolInput: string(tb.Input)})

			// ── Web search rate limiting ──
			if tb.Name == "web_search" || tb.Name == "fetch_url" {
				webSearchCount++
				if webSearchCount > maxWebSearchPerTurn {
					toolOutput := fmt.Sprintf(
						"LIMIT REACHED: You have already made %d web searches this turn. "+
							"STOP searching and use your training knowledge to answer instead. "+
							"You already know common facts like tax rates, formulas, code syntax, etc. "+
							"Proceed with the information you already have.",
						webSearchCount)
					result.Steps = append(result.Steps, Step{
						Type:     "tool_result",
						Content:  toolOutput,
						ToolName: tb.Name,
					})
					emit(StreamEvent{Type: "tool_result", ToolName: tb.Name, Content: toolOutput})
					toolResults = append(toolResults, map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": tb.ID,
						"content":     toolOutput,
					})
					continue
				}
			}

			toolStart := time.Now()
			toolOutput, err := deps.ToolRegistry.Execute(ctx, tb.Name, tb.Input)
			toolDuration := time.Since(toolStart)
			if err != nil {
				log.Printf("[agent]   tool ERROR name=%s err=%v elapsed=%s",
					tb.Name, err, toolDuration)
				toolOutput = fmt.Sprintf("Tool execution error: %v", err)
			} else {
				log.Printf("[agent]   tool DONE  name=%s output_size=%d elapsed=%s",
					tb.Name, len(toolOutput), toolDuration)
			}

			truncated := truncateSmart(toolOutput, 4000)
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

			// Auto-save scripts to .scratch/ subdirectory (not visible in Working Folder).
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
					scratchDir := filepath.Join(workDir, ".scratch")
					os.MkdirAll(scratchDir, 0o755)
					savePath := filepath.Join(scratchDir, filepath.Clean(filename))
					if err := os.WriteFile(savePath, []byte(codeInput.Code), 0o644); err != nil {
						log.Printf("[agent] scratch save failed: %v", err)
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

// truncateSmart uses a head+tail pattern so the model sees both the
// beginning and end of long output, plus a count of omitted lines.
func truncateSmart(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	lines := strings.Split(s, "\n")
	totalLines := len(lines)

	// Budget: first 60%, last 40%.
	headBudget := maxBytes * 6 / 10
	tailBudget := maxBytes - headBudget - 60 // 60 bytes for the separator
	if tailBudget < 0 {
		tailBudget = 0
	}

	// Build head.
	var head []string
	headLen := 0
	for _, ln := range lines {
		if headLen+len(ln)+1 > headBudget {
			break
		}
		head = append(head, ln)
		headLen += len(ln) + 1
	}

	// Build tail (walk backward).
	var tail []string
	tailLen := 0
	for i := len(lines) - 1; i >= len(head); i-- {
		if tailLen+len(lines[i])+1 > tailBudget {
			break
		}
		tail = append([]string{lines[i]}, tail...)
		tailLen += len(lines[i]) + 1
	}

	omitted := totalLines - len(head) - len(tail)
	if omitted <= 0 {
		return s[:maxBytes] + "\n... [truncated]"
	}

	return strings.Join(head, "\n") +
		fmt.Sprintf("\n\n... [%d lines omitted] ...\n\n", omitted) +
		strings.Join(tail, "\n")
}

// truncate is a simple byte-based fallback (kept for backward compat).
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}

// emitDeliverableFiles scans the workspace directory for user-visible output
// files (excluding .scratch/, hidden files, and .jsonl session logs) and emits
// file_created events so they appear in the Working Folder panel and chat.
func emitDeliverableFiles(workDir string, emit func(StreamEvent)) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		filePath := filepath.Join(workDir, e.Name())
		emit(StreamEvent{
			Type:    "file_created",
			Content: filePath,
			Path:    filePath,
			Name:    e.Name(),
		})
	}
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

// ── Structured System Prompt Builder ──

const defaultSystemPrompt = `# Cowork — System Prompt

You are Cowork, a powerful AI pair-programmer and terminal assistant built into the Flow app.
You have direct macOS terminal access to execute commands, read/write files, and automate coding workflows.

## Cognitive Guidelines

- **Precision-First**: Use exact file paths and fully qualified parameters. Never guess or speculate about the state of files — read them first to verify their contents.
- **Production-Ready Deliverables**: When writing code or creating files, write clean, modular, self-documenting, and robustly error-handled code. Do not use placeholders or write incomplete code blocks.
- **Robust Verification**: Always verify the correctness of your work. After creating or editing code, execute it or write tests/validations to confirm it runs correctly and produces the expected output.
- **Concise Explanations**: Focus on giving highly technical, direct answers. Keep conversational fluff, greetings, and generic preambles to a minimum.
`

// promptFileName is the unified system prompt file.
const promptFileName = "system_prompt.md"

// legacyPromptFiles are older prompt file locations we migrate from.
var legacyPromptFiles = []string{
	"cowork_prompt.md",
	"workspace/Master_prompt.md",
}

// buildFullSystemPrompt composes the final system prompt from all sections.
// If the caller already supplied a systemPrompt (from agentapp.go's
// loadCoworkSystemPrompt), it's used as the base; otherwise we load from
// the unified file or fall back to the default.
func buildFullSystemPrompt(supplied string, deps Deps) string {
	var sections []string

	// 1. Base prompt: user-supplied > file > default.
	base := supplied
	if strings.TrimSpace(base) == "" && deps.BaseDir != "" {
		base = loadUnifiedPrompt(deps.BaseDir)
	}
	if strings.TrimSpace(base) == "" {
		base = defaultSystemPrompt
	}
	sections = append(sections, strings.TrimSpace(base))

	// 2. Environment context (date, OS, cwd, python).
	if deps.BaseDir != "" || deps.WorkDir != "" {
		sections = append(sections, buildEnvironmentSection(deps))
	}

	// 3. Tool guidance (dynamically excludes disabled tools).
	disabledSet := make(map[string]bool, len(deps.DisabledTools))
	for _, name := range deps.DisabledTools {
		disabledSet[name] = true
	}
	sections = append(sections, buildToolGuidance(disabledSet))

	// 4. Safety guardrails.
	sections = append(sections, sectionSafety)

	// Multimodal & Vision instructions.
	sections = append(sections, sectionMultimodal)

	// 5. Mode-specific sections.
	if deps.ChatMode {
		sections = append(sections, sectionBrevity)
	} else {
		sections = append(sections, sectionPlanning)
	}
	sections = append(sections, sectionCodeOutput)
	sections = append(sections, sectionOptions)

	// 6. Active command (plugin context).
	if deps.CommandBody != "" {
		sections = append(sections, "\n## Active Command\n\nThe user invoked a custom command. Follow the instructions below:\n\n"+deps.CommandBody)
	}

	// 7. Memory index.
	if deps.BaseDir != "" {
		if mem := buildMemoryIndex(deps.BaseDir); mem != "" {
			sections = append(sections, mem)
		}
	}

	// 8. Skill index.
	if deps.BaseDir != "" {
		if sk := buildSkillIndex(deps.BaseDir); sk != "" {
			sections = append(sections, sk)
		}
	}

	return strings.Join(sections, "\n")
}

// loadUnifiedPrompt loads the system prompt from the unified file location,
// migrating from legacy locations if needed.
func loadUnifiedPrompt(baseDir string) string {
	unifiedPath := filepath.Join(baseDir, promptFileName)

	// Try the unified file first.
	if data, err := os.ReadFile(unifiedPath); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}

	// Try legacy locations and migrate.
	for _, legacy := range legacyPromptFiles {
		legacyPath := filepath.Join(baseDir, legacy)
		if data, err := os.ReadFile(legacyPath); err == nil {
			if s := strings.TrimSpace(string(data)); s != "" {
				// Migrate: write to unified path.
				_ = os.WriteFile(unifiedPath, data, 0o644)
				log.Printf("[agent] migrated prompt from %s to %s", legacy, promptFileName)
				return s
			}
		}
	}

	// Bootstrap the default.
	_ = os.WriteFile(unifiedPath, []byte(defaultSystemPrompt), 0o644)
	return defaultSystemPrompt
}

// buildEnvironmentSection injects runtime context into the prompt.
func buildEnvironmentSection(deps Deps) string {
	now := time.Now()
	date := now.Format("2006-01-02")
	timeStr := now.Format("15:04 MST")
	osInfo := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

	workDir := deps.WorkDir
	if workDir == "" {
		workDir = "(not set)"
	}

	// Load Python path from settings.
	pythonPath := "python3"
	if deps.BaseDir != "" {
		if cfg, err := config.Load(deps.BaseDir); err == nil && cfg != nil && cfg.PythonPath != "" {
			pythonPath = cfg.PythonPath
		}
	}

	// Probe for standard tools on the host.
	commonTools := []string{"git", "pdftotext", "ffmpeg", "sqlite3", "curl", "wget", "npm", "cargo"}
	var available []string
	for _, cmd := range commonTools {
		if _, err := exec.LookPath(cmd); err == nil {
			available = append(available, cmd)
		}
	}
	availableStr := "(none detected)"
	if len(available) > 0 {
		availableStr = strings.Join(available, ", ")
	}

	var b strings.Builder
	b.WriteString("\n## Environment\n\n")
	fmt.Fprintf(&b, "- **Date:** %s (%s)\n", date, timeStr)
	fmt.Fprintf(&b, "- **OS:** macOS (%s)\n", osInfo)
	fmt.Fprintf(&b, "- **Working Directory:** %s\n", workDir)
	fmt.Fprintf(&b, "- **Python:** %s\n", pythonPath)
	fmt.Fprintf(&b, "- **Available CLI Utilities:** %s\n", availableStr)

	return b.String()
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
