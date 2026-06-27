# Changelog

All notable changes to **Flow** will be documented in this file.

---

## [0.8.0] - 2026-06-27

### ☁️ Quick Ask — Floating Agent HUD
* **Hotkey-invoked floating agent**: A new dedicated push-to-talk hotkey opens a floating, always-on-top popup (the **HUD**) that runs the full Cowork agent without leaving the current app. Hold to speak a request, or tap to open it with context-aware suggestions; also reachable from the menu bar (**Open Quick Ask**).
* **One unified, smooth popup**: The same window flows through **Listening** (live waveform with ✕ cancel / ✓ send) → **Transcribing** → **Thinking** (current-step pill) → **streamed answer**, with an animated, content-fitting panel. Implemented as a native `NSPanel` hosting a `WKWebView`, served by an internal localhost HUD server over SSE (Go→JS) and fetch (JS→Go); pre-warmed at startup for an instant first show.
* **Acts from on-screen context**: Reads the frontmost app and text selection, and attaches a **screenshot captured the moment you ask** (before the HUD covers the screen) so the agent sees what you're looking at. Self-contained questions (math, conversions, definitions) skip the screenshot for a fast reply. Vision-capable models also power the tap-to-open suggestion chips.
* **Cross-app actions & in-HUD approvals**: Can drive other apps via macOS automation (`osascript` / `open`); when a step needs a command, the HUD shows a compact **Needs approval** card. Cross-app automation is pre-allowed per HUD session for low-friction multi-step tasks.
* **Conversational**: Type follow-ups, **Copy** any response, start a fresh chat with **+**, or **open in app** to continue the same session in the full Cowork window. Quick Ask chats now appear live in the Cowork list (no restart needed).
* **Non-intrusive window**: The HUD floats over your current app without stealing focus (non-activating panel) while still responding to a single click.
* **Settings & permissions**: New **HUD & Quick Ask** settings section with a configurable hotkey and inline **Screen Recording** and **Accessibility** permission checks (grant buttons).

### ⏰ Cron-based Task Scheduling
* **Scheduled agent tasks**: Added a cron-based scheduling system so Cowork agent tasks can run automatically on a recurring schedule.

## [0.7.0] - 2026-05-26

### ⚡ Consolidated Cowork Agent Tool Execution & PDF/Excel Parsing
* **Efficiency Cognitive Guideline**: Integrated a high-priority system-level prompt rule (`Consolidated Execution`) urging the model to write robust self-contained scripts rather than running sequential chains of terminal one-liners.
* **Native Rich Document Reading**: Highlighted native `.pdf`, `.xlsx`, `.docx`, and `.pptx` reading capabilities of the `read_file` tool directly in the builder's prompt context. This discourages the agent from writing custom python parsers and running multiple tool iterations.
* **Streamlined Bash Tool Tips**: Refactored the `run_bash` tool usage tips to promote self-contained scripts run in a single tool call and suppress raw CLI `python3 -c` one-liners.

### 🎙️ Smooth macOS Dictation Overlay Morphing & Dynamic Labels
* **Fluid CoreAnimation Morphing**: Redesigned the native macOS dictation overlay in Cocoa (`dictation_darwin.m`) to pre-allocate `recContainer` and `thinkContainer` views, using CoreAnimation (`NSAnimationContext`) with ease-in-ease-out timing functions to smoothly morph panel dimensions and cross-fade container opacities over 220ms.
* **Dynamic Label Sizing & States**: Replaced the static, hardcoded "Thinking" message with support for custom dynamic label text, automatically calculating precise string widths in Cocoa to seamlessly scale the overlay width on state transitions (e.g., "Translating...", "Refining...").

### 🧠 Local Whisper Transcription Thread & Delay Optimizations
* **Dynamic Physical Core Detection**: Configured the local Whisper dictation engine (`transcribe_local_darwin.go`) to query performance cores (`hw.perflevel0.physicalcpu`) via `sysctl` to run transcription on dedicated high-performance CPU cores for Apple Silicon, falling back to a safe core count.
* **Bypassed Temperature Fallbacks**: Appended the `-nf` (no fallback) flag to `whisper-cli` arguments to disable recursive temperature-based retries, ensuring near-instantaneous transcription speed for quick voice inputs.

### ⚙️ Suppressed Tool Definitions in Chat-Only Mode
* **Drastic Context Footprint Reductions**: Enhanced the Go backend agent execution flow (`backend/internal/agent/agent.go`) to completely bypass compiling and transmitting tool definitions to the LLM when both `DisableSystemPrompt` and `ChatMode` are active. This significantly reduces token overhead and input latency during casual chat sessions.

### 📂 Persistent Local Application Configurations
* **Decoupled Local App Preferences**: Migrated critical application settings and UI state (such as left sidebar width, right sidebar width, and pinned model selections) from browser-managed `localStorage` to a robust, local backend configuration file managed by the Go service (`app.go`).
* **Synchronized Backend & Svelte Bridges**: Integrated new Go backend bindings `SavePinnedModels`, `GetPinnedModels`, `SaveUIWidths`, and `GetUIWidths` to dynamically sync preferences across app updates and reinstalls.

## [0.6.9] - 2026-05-25

### 🎙️ Background Voice Execution & Window Close Fix
* **Minimize on Close**: Configured the application to hide the window instead of quitting when the window close button ("X") is clicked, allowing background voice recording and dictation services to continue uninterrupted.
* **Mac Status Menu Integration**: Registered a native Go hook linking the Cocoa status bar "Show Flow" action to the Wails runtime window restoration API, ensuring the hidden window is successfully brought back to the foreground and focused.

## [0.6.8] - 2026-05-23

### 🧠 Google Vision OCR Fallback for Non-Multimodal Models
* **On-Device Vision OCR Fallback**: Integrated native support for macOS Vision OCR to automatically parse user-attached images when running local GGUF models or cloud models that do not support multimodal image inputs.
* **Spatial Layout Bounding Boxes**: Reconstructs spatial layout metadata with bounding box coordinates (`[Top, Left, Width, Height]`) and layout-aware Markdown directly in the chat history.
* **Responsive Model Selector Updates**: Optimized the model selector dropdown in the frontend for smooth handling of OCR and vision configurations.

### 🐍 Hermetic Pyenv Virtualenv & Sandbox Improvements
* **Precise Python Command Rewriter**: Swapped basic word boundary checks with robust trailing boundary matches (`(\s|[|&;)]|$)`) to prevent false-positive matching of hyphenated Python packages (e.g., `python-pptx`, `python-docx`).
* **Subprocess Environment Isolation**: Dynamically prepends the active virtualenv's `bin` directory to the subprocess `PATH` environment variable, guaranteeing nested scripts and sub-spawned commands run cleanly in the virtualenv.
* **Sandbox Write Exception Whitelist**: Whitelisted virtualenv root directories and user pip caches (`~/Library/Caches/pip`, `~/.cache/pip`) in the macOS `sandbox-exec` profile to allow warning-free package installation.

## [0.6.7] - 2026-05-23

### 🧠 Native Reasoning & Thinking Support (DeepSeek-R1)
* **Reasoning Stream Extraction**: Integrated native support for streaming thinking blocks (`<thinking>` / `<reasoning>`) from models like DeepSeek-R1 natively in the LLM execution pipeline, maintaining a clean distinction between thought processes and final assistant outputs.
* **Workspace Image Persistence**: Modified attached image handling to auto-save input images directly to the active session workspace. This allows local scripts and bash commands to run against attached visual files seamlessly.
* **Smart Vision Recommendations**: Implemented friendly, actionable diagnostics that catch OpenRouter or llama.cpp vision-less model errors and suggest switching to active vision-capable models instead of throwing raw API stack traces.

### ⏱️ Local LLM Loading Indicator
* **Global Initializing Status**: Integrated a custom window event (`flow:local-llm-loading`) between Wails backend and Svelte frontend to track model booting/initializing lifecycles.
* **UI Micro-Spinner & Reactive Text**: Replaced the static status light in the composer's model selector with a pulsing green micro-spinner and a label that dynamically shifts to `Loading [Model Name]...`.
* **Interaction Blocking**: Disabled interaction with the Model Selector and the prompt bar during server boots to prevent concurrent api/port crashes.

### ⚙️ Chat-Only System Prompt Toggle
* **Dropdown Option Integration**: Developed a premium toggle switch row directly inside the Model Selector dropdown menu next to the tools button.
* **Context Suppression**: Bypasses system prompt composition inside the Go backend (`agent.go`) exclusively in Chat Mode, allowing raw prompt queries to minimize context window size, reduce response latency, and save token costs.
* **Protected Multi-Step Agent Tasks**: Ensures multi-step workspace tasks remain completely unaffected, maintaining detailed tool instructions, safety guidelines, and plan compilers active.

## [0.6.6] - 2026-05-23

### 🔍 Web Search & Bot Loop Protection
* **Tavily API Integration**: Added support for premium Tavily web search inline next to the web-search toggle. If a Tavily API key is set in settings, Flow will prioritize Tavily for fast, high-quality, and structured web results.
* **Smart Bot Loop Protection**: Fallback DuckDuckGo search now detects CAPTCHAs, bot protection, and rate-limiting. Instead of retrying indefinitely, the agent is instructed to stop searching and immediately proceed using its existing training knowledge.

### 🧠 Prompt Caching & Dynamic Thinking Budget (Anthropic)
* **Prompt Caching Beta Support**: Integrated Anthropic prompt caching (`prompt-caching-2024-07-31` beta). System blocks and tool definitions are tagged with `cache_control: { type: "ephemeral" }` to drastically reduce token costs and speed up response times on multi-turn conversations.
* **Configurable Thinking Budget**: Added customizable thinking budget support for Claude models. The thinking budget token limit is loaded dynamically from the agent configurations.

### 📂 Native Directory Explorer & Rich Document Support
* **Native `list_dir` Tool**: Added a dedicated `list_dir` tool to list workspace files and folders safely, eliminating the need to run shell `ls` commands.
* **Smart Rich Document Reading**: Integrated text extraction parser directly into `read_file`. The tool now automatically parses and reads formatting in `.pdf`, `.xlsx`, `.docx`, and `.pptx` files.
* **Actionable Workspace Error Handling**: When trying to read a non-existent file, the tool lists all files currently in the workspace, making it easier for the agent to recover from filename typos.

### 🧩 Resilient Skill Resolution
* **Multi-Path Skill Resolution**: The `use_skill` tool now searches for markdown skills in both `plugins/skills/` and `memory/` folders. It also handles name normalization, resolving skills with or without the `skill-` prefix.

### ⏱️ Performance Metrics & UX Polish
* **Real-Time Generation Metrics**: Added token performance indicators in the chat workspace. Displays live tokens-per-second (t/s) and total response duration next to the message copy button.
* **Sleek Dynamic Model Selector**: Developed a premium settings-based `ModelSelector` component supporting local, custom, and cloud providers (OpenAI, Anthropic, Gemini, OpenRouter) with dynamic model fetching and managed llama-server auto control.

### 🧹 Llama Server Reliability
* **Zombie Server Cleanup**: Integrated robust process management to ensure any stray or orphaned `llama-server` processes are completely terminated (`KillStrayServers` via `pkill` and `killall`) on app startup, shutdown, and close, preventing port conflicts.

## [0.6.5] - 2026-05-23


### 🤖 Cowork Agent Harnessing & Best Practices

Re-engineered Cowork's agent harness and execution pipeline to align with modern best practices from state-of-the-art developer systems (like Pi coding agent, OpenCode, and Claude tool-use frameworks).

#### 1. Structured Composable Prompting
* **Modular Prompt Builder**: Replaced flat system prompts with a composable pipeline that builds identity, dynamic date/time, OS environment specs, sandboxed safety constraints, planning/todo regulations, custom tool-use guides, active skills, and persistent memories per turn.
* **Granular Safety Guardrails**: Hardcoded strong negative rulesets into the structured prompt (preventing modification of system directories, access to sensitive external key folders, or deletion of files outside the workspace).

#### 2. Advanced Output Diagnostics & Visibility
* **Smart Output Truncation**: Replaced standard tail-only output truncation with a head-and-tail byte-based chunking algorithm (4KB fallback). It preserves the initial 60% and trailing 40% of lengthy logs, ensuring the LLM maintains context of both startup configs and crash stacks.
* **Backend Diagnostic Console Logs**: Log tool names invoked during streaming (`tools=todo_write,run_bash`) directly inside the main backend console outputs (`── LLM RESP ──` logs) for effortless local debugging.

#### 3. Workspace Cleanliness & Sandboxing
* **Workspace Cleanliness (`.scratch/`)**: Reconfigured `run_bash` scripts to auto-save executed script blocks to a hidden `.scratch/` subdirectory, keeping intermediate clutter out of the user's view.
* **Automatic Deliverable Scanning**: Implemented an end-of-turn directory scanner in the backend to automatically pick up and surface deliverables (like `.xlsx`, `.pdf`, `.csv`, `.py`, `.js` files) generated inside the sandbox.
* **Formal Script Deliverables**: Updated agent prompt instructions to explicitly recognize and structure `.py` and `.js` formats as valid final deliverables in the root folder when requested.
* **Long-Run Capacity Increase**: Increased the agent iteration capacity from **25 to 50 steps** to accommodate highly complex multi-stage tasks.
* **Interactive Plan & Card Deduplication**: Deduplicated inline file cards on the frontend so identical deliverables surfaced by tool executions and post-turn directory scans only appear once.
