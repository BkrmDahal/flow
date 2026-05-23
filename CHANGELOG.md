# Changelog

All notable changes to **Flow** will be documented in this file.

---

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
