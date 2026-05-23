# Release Notes — v0.6.0 🚀

Flow v0.6.0 introduces a massive set of features, UI refinements, speech-to-text optimizations, and macOS security/packaging improvements designed to provide a premium, seamless, and powerful desktop productivity workspace.

---

## 🤖 Cowork (AI Agent Workspace)
- **Descriptive Multi-Step Planning**: Transformed the agent planning loop to utilize clean, human-readable steps instead of code blocks, and hidden internal `todo_write` execution details from chat logs.
- **Premium Resizers & Layouts**: Added interactive sidebar drag resizers, smooth edge-fade text gradients for overflowing lists, and aligned user chat bubbles with standard right-alignment.
- **Top Fixed Header & Renaming**: Implemented a fixed top workspace header featuring inline renaming, proper spacing, and unified layout constraints.
- **Proactive Clarifications**: Replaced standard back-and-forth prompts with responsive interactive option pills and an inline custom write-in input box.
- **Temporary Approval Memory**: Added session-level persistence for approved terminal command folders to minimize safety prompts.

## 🎙️ Flow (Voice Recorder & Refiner)
- **Whisper CLI Stability**: Bundled dynamic libraries and OpenMP dependencies inside the `whisper-cli` package to resolve macOS crashes on newer Apple Silicon chips.
- **UI & Hover Polish**: Resolved nested DOM button warnings in the sidebar list, corrected delete/edit state bugs, and added standard trash-can SVGs matching Cowork.
- **Keyboard Shortcut Upgrade**: Retired `Cmd+Shift+R` toggle and replaced it with a sleek, double-tap grammar-correction hotkey guide.

## 🧩 Toolkit & Customization (Customize)
- **Integrated Snippets Expansion**: Fully integrated custom text expansions directly into all transcript pipelines.
- **Unified Customize Panel**: Relabeled the old Toolkit as "Customize" in the footer, using a modern sliders icon, and structured the panels into **Cowork**, **Flow**, and **System** groupings.
- **Persistent Memory Interface**: Launched a full-featured CRUD interface under System Memory for managing the agent's persistent long-term knowledge base.

## ⚙️ Settings & Providers
- **Card-Based Provider Setup**: Consolidated and redesigned the LLM configuration panel into dedicated Local vs. Cloud cards.
- **Clear Default States**: Unselected both Local and Cloud providers by default on new setups for absolute onboarding clarity.

## 🛡️ macOS Packaging, Code Signing & Notarization
- **Hardened Runtime Signing**: Integrated comprehensive nested binary signing (including `whisper-cli` and `llama-server`) with valid Apple Developer ID certifications.
- **Automated Notarization**: Configured full integration with Apple Notary Service (`notarytool`) and implemented a retry loop to account for CloudKit propagation lag during stapling.
- **Robust DMG Creation**: Upgraded `hdiutil` commands inside `flow.sh` with a retry loop to gracefully bypass transient macOS "Resource busy" errors during high-speed packaging.
- **MIT License & Brand Upgrade**: Transitioned to an MIT License and fully upgraded the brand identity with a high-fidelity Big Sur-compliant squircle app icon.
