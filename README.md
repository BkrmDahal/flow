# Flow

Flow is a powerful, unified macOS desktop application built with [Wails](https://wails.io/), Go, and Svelte. It is designed to boost productivity by integrating smart agent capabilities, dictation, and text processing directly into your daily workflow.

## Features

- **Cowork Workspace**: A dedicated multimodal workspace for interacting with agent infrastructure, supporting file uploads, and managing intelligent tasks.
- **System-wide Text Processing**: Seamless text-selection and grammar-fixing features powered by clipboard integration.
- **Voice & Dictation**: Built-in STT (Speech-to-Text) capabilities supporting both local and cloud-based speech providers.
- **Global Hotkeys**: Quick-action hotkey capture for instant access to Flow's features from anywhere in your OS.
- **Modern UI**: A beautiful, Claude-inspired warm dark theme with polished, responsive Svelte components.

## Development

To run the application in live development mode (which provides hot-reloading for the Svelte frontend):

```bash
wails dev
```

## Build

To build a redistributable, production-ready macOS application package:

```bash
wails build
```

## macOS Gatekeeper & "Damaged App" Fix

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

## Structure

- `frontend/` - Contains the Svelte/Vite frontend code, including the `CoworkWorkspace` and `FlowPanel` components.
- `backend/` - Contains the Go backend code, managing system integrations, clipboard actions, audio recording, and hotkeys.
- `main.go` - The main application entry point that initializes the Wails application and binds the backend methods.
- `flow.sh` - Development and utility script for the project.
