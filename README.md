# Flow

Flow is a powerful, unified macOS desktop application built with [Wails](https://wails.io/) (Go + Svelte). It combines an AI agent workspace, voice-to-text dictation, transcript refinement, and extensible tooling into a single, beautiful native app — designed to supercharge your daily productivity.

---

## ✨ Features

### 🤖 Cowork — AI Agent Workspace

A full-featured, multi-turn chat workspace powered by LLMs with agentic tool-calling capabilities:

- **Streaming responses** with live token rendering
- **File attachments** — upload images, PDFs, CSVs, and text documents directly into the conversation
- **Tool use** — the agent can autonomously:
  - Read, write, and create files (`file_read`, `file_write`)
  - Execute shell commands with sandboxed approval (`command`)
  - Plan and track multi-step work via a todo list (`todo_write`)
  - Capture screenshots (`screen_capture`)
  - Search the web (`web_search`, `web_read`)
  - Persist key facts across sessions (`memory_read`, `memory_write`)
  - Load custom skills into context (`skill_load`)
- **Session management** — create, list, load, and delete chat sessions
- **Customisable system prompt** — edit `~/.flow/cowork_prompt.md` to tailor the agent's behaviour

### 🎙️ Flow — Voice Recorder & Transcript Refiner

Record voice notes and transcribe them with configurable speech-to-text providers:

- **One-click record/stop** with macOS native audio capture (M4A)
- **Cloud STT providers**: OpenAI Whisper, Deepgram
- **Local STT**: bundled whisper.cpp — fully offline, no API key required
- **Transcript refinement** via LLM (streaming):
  - **Clean** — fix grammar, punctuation, and disfluencies
  - **Summarize** — condense to 2-3 sentences
  - **Bullet points** — extract key points
  - **Custom prompt** — your own instruction
- **Auto-refine on stop** — optionally refine every transcript automatically after recording
- **Transcript library** — save, browse, and delete past transcriptions

### ⌨️ System-wide Dictation & Text Processing

- **Global hotkey** (configurable modifier) for instant dictation from any app
- **Double-tap hotkey** — grabs selected text and fixes grammar/spelling via LLM
- **Native macOS overlay** — visual recording indicator with live status
- **Menu bar icon** — pinwheel status indicator (idle / recording / transcribing)

### 🧩 Plugins — Commands, Skills & Snippets

- **Commands** — named prompt templates callable by name (e.g., `/summarize`)
- **Skills** — reusable instruction sets the agent can load into context at runtime
- **Snippets** — trigger/expansion text replacements applied to transcriptions
- Full CRUD from the Settings UI

### 🦙 Local LLM Support (llama.cpp)

- **Managed llama-server** — Flow can download, start, and stop a local llama.cpp inference server
- **One-click GGUF download** from Hugging Face with progress tracking
- **Model picker** — native file dialog for local `.gguf` files
- Auto-detection of served models via `/v1/models`

### ☁️ Multi-Provider LLM Support

- **Local / OpenAI-compatible** — LM Studio, Ollama, llama.cpp, or any `/v1/chat/completions` endpoint
- **Anthropic** (Claude)
- **OpenAI** (GPT-4o, etc.)
- **Google Gemini**
- **OpenRouter**
- **Custom cloud endpoint** with configurable base URL + API key
- Automatic provider fallback: primary → fallback → legacy agent config

### 🔒 Sandboxed Command Execution

- Shell commands run by the agent require **explicit user approval** via a frontend dialog
- Configurable **allow/block lists** persisted in `~/.flow/exec-approvals.json`
- Blocked patterns are automatically denied; allowed patterns are auto-approved

---

## 🏗️ Architecture

```
flow/
├── main.go                    # Wails app entry point
├── wails.json                 # Wails project configuration
├── flow.sh                    # Dev, build, DMG, sign, and utility CLI
├── dequarantine.sh            # macOS Gatekeeper quarantine removal
│
├── backend/                   # Go backend (Wails-bound)
│   ├── app.go                 # App struct, Startup/Shutdown lifecycle
│   ├── agentapp.go            # Agent & Cowork streaming, session CRUD
│   ├── flow.go                # Voice recording, transcription, transcript CRUD
│   ├── flow_refine.go         # LLM-powered transcript refinement (streaming)
│   ├── dictation.go           # System-wide dictation hotkey setup
│   ├── settings.go            # Settings load/save, LLM client management
│   ├── models.go              # OpenAI-compatible /v1/models listing
│   ├── llama.go               # Managed llama.cpp server + GGUF download
│   ├── plugins.go             # Commands & Skills CRUD (Wails facades)
│   ├── snippets.go            # Snippets CRUD (Wails facades)
│   └── internal/
│       ├── agent/             # Agentic loop: multi-turn tool-calling orchestration
│       ├── config/            # Config bootstrap, load/save, exec approvals
│       ├── llm/               # LLM client abstraction (Anthropic, OpenAI)
│       ├── llamacpp/          # llama-server process manager
│       ├── parser/            # Content parsing utilities
│       ├── plugins/           # Commands & Skills store (file-backed)
│       ├── session/           # Session persistence (JSONL-based)
│       ├── snippets/          # Snippets store (file-backed)
│       ├── speech/            # STT (Whisper, Deepgram, local), dictation, menu bar, overlay
│       ├── streaming/         # Stream manager, file attachments, content builder
│       └── tools/             # Tool registry + implementations (file, command, memory, todo, web, skill, screen)
│
├── frontend/                  # Svelte + Vite frontend
│   └── src/
│       ├── App.svelte         # Root app shell with tab navigation
│       ├── main.js            # Svelte mount point
│       ├── app.css            # Global styles
│       └── components/
│           ├── CoworkWorkspace.svelte   # Cowork agent chat UI
│           ├── FlowPanel.svelte         # Voice recorder & transcript viewer
│           ├── FlowRefineMenu.svelte    # Refinement action picker
│           ├── AgentWorkspace.svelte     # Agent task workspace
│           ├── AgentSidebar.svelte       # Agent session sidebar
│           ├── AgentInfoPanel.svelte     # Agent task info panel
│           ├── AgentFileCard.svelte      # File card in agent workspace
│           ├── AgentWelcome.svelte       # Agent welcome screen
│           ├── SettingsModal.svelte      # Full settings UI
│           ├── SkillToolkitPanel.svelte  # Skills & commands manager
│           ├── LoadingSpinner.svelte     # Loading indicator
│           └── TypingIndicator.svelte    # Chat typing animation
│
└── scripts/
    ├── sign.sh                # Apple code signing, notarization & stapling
    ├── release.sh             # GitHub release automation
    ├── fetch-llama-server.sh  # Download pre-built llama-server binary
    └── fetch-whisper-cli.sh   # Download pre-built whisper.cpp CLI
```

---

## 🚀 Prerequisites

- **macOS** (Apple Silicon or Intel)
- **Go** 1.24+
- **Node.js** 18+ and npm
- **Wails CLI** v2 — install with `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

---

## 💻 Development

Start the app in live development mode with hot-reloading for the Svelte frontend:

```bash
wails dev
```

Or use the project script:

```bash
./flow.sh dev
```

---

## 📦 Build

### Production App (Apple Silicon)

```bash
./flow.sh build
# → build/bin/Flow.app
```

### Universal Binary (Intel + Apple Silicon)

```bash
./flow.sh universal
```

### DMG Installer

```bash
./flow.sh dmg
# → build/Flow-Installer.dmg
```

### Code Signing & Notarization

```bash
./flow.sh sign
```

Requires an Apple Developer account and the signing environment variables configured in `scripts/sign.sh`.

---

## 🩺 Doctor

Check if your system has all the Wails development dependencies:

```bash
./flow.sh doctor
```

---

## ⚙️ Configuration

All configuration lives in `~/.flow/`:

| File | Purpose |
|------|---------|
| `config.json` | App settings (providers, API keys, hotkey, STT config) |
| `cowork_prompt.md` | Editable Cowork system prompt |
| `exec-approvals.json` | Allowed/blocked shell command patterns |
| `flow.log` | Application log file |
| `flow/` | Saved Flow transcripts (JSON) |
| `cowork/` | Cowork session data |
| `agents/` | Agent task sessions and working directories |
| `llamacpp/` | Managed llama-server binary and downloaded GGUF models |
| `plugins/` | Commands & Skills store |
| `snippets/` | Snippets store |
| `memory/` | Agent persistent memory |

---

## 🍎 macOS Gatekeeper & "Damaged App" Fix

When you first install the built Flow app (e.g. from a ZIP or DMG downloaded from the internet), macOS might show a warning message saying:
> **"Flow" is damaged and can't be opened. You should move it to the Trash.**

This is standard macOS Gatekeeper behavior for self-signed or unsigned applications downloaded from the web (which receive the `com.apple.quarantine` extended attribute).

To resolve this issue, we have provided an automated de-quarantine script:

```bash
./dequarantine.sh
```

Or run it via `flow.sh`:

```bash
./flow.sh dequarantine
```

*Note: The script automatically searches `/Applications/Flow.app`, `~/Applications/Flow.app`, and `./build/bin/Flow.app`. You can also pass a custom path as an argument:*

```bash
./dequarantine.sh /path/to/Flow.app
```

Alternatively, you can manually run this command in your terminal to remove the quarantine flag recursively:

```bash
sudo xattr -r -d com.apple.quarantine /Applications/Flow.app
```

---

## 📄 License

Private / All Rights Reserved
