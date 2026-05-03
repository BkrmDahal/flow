<script>
  import { onDestroy } from 'svelte'
  import { Backend, Events } from '../lib/wails.js'

  export let open = false
  export let onClose = () => {}

  const PRESETS = [
    { id: 'lmstudio', label: 'LM Studio', baseUrl: 'http://localhost:1234/v1', apiKey: 'lm-studio' },
    { id: 'llamacpp', label: 'llama.cpp', baseUrl: 'http://127.0.0.1:8080/v1', apiKey: '' },
    { id: 'custom',   label: 'Custom',    baseUrl: '',                          apiKey: '' },
  ]

  const REFINE_OPTIONS = [
    { value: 'off',       label: 'Off' },
    { value: 'clean',     label: 'Clean' },
    { value: 'summarize', label: 'Summarize' },
    { value: 'bullets',   label: 'Bullets' },
    { value: 'custom',    label: 'Custom' },
  ]

  const REFINE_PROMPTS = {
    clean: "You are a transcription cleanup assistant. Fix grammar, punctuation, and disfluencies in the user's voice transcript. Preserve their voice and meaning. Output only the cleaned text.",
    summarize: 'Summarize the voice note in 2 to 3 sentences. Output only the summary.',
    bullets: 'Convert the voice note into a clean bullet list of the key points. Output only the bullet list using - markers.',
    custom: '',
  }

  const MODIFIER_OPTIONS = [
    { value: 'left_option',  label: '⌥ Left Option' },
    { value: 'right_option', label: '⌥ Right Option' },
    { value: 'left_cmd',     label: '⌘ Left Command' },
    { value: 'right_cmd',    label: '⌘ Right Command' },
    { value: 'left_ctrl',    label: '⌃ Left Control' },
    { value: 'right_ctrl',   label: '⌃ Right Control' },
  ]

  const SPEECH_PROVIDERS = [
    { value: 'local',    label: 'Local (Whisper, on-device)' },
    { value: 'whisper',  label: 'OpenAI Whisper' },
    { value: 'deepgram', label: 'Deepgram' },
  ]

  const SPEECH_MODELS_OPENAI = [
    { value: 'gpt-4o-mini-transcribe', label: 'GPT-4o Mini Transcribe' },
    { value: 'gpt-4o-transcribe',      label: 'GPT-4o Transcribe' },
    { value: 'whisper-1',              label: 'Whisper-1 (legacy)' },
  ]

  const SPEECH_MODELS_LOCAL = [
    { value: 'tiny.en',   label: 'tiny.en (~75 MB, fastest)' },
    { value: 'base.en',   label: 'base.en (~142 MB, recommended)' },
    { value: 'small.en',  label: 'small.en (~466 MB)' },
    { value: 'medium.en', label: 'medium.en (~1.5 GB, most accurate)' },
  ]

  const modifierLabels = {
    left_option:  '⌥ Left Option',
    right_option: '⌥ Right Option',
    left_cmd:     '⌘ Left Command',
    right_cmd:    '⌘ Right Command',
    left_ctrl:    '⌃ Left Control',
    right_ctrl:   '⌃ Right Control',
  }

  // ── Tab state ──
  let activeTab = 'general' // 'general' | 'voice' | 'hotkeys'

  // ── General / model settings ──
  let providerLabel = 'LM Studio'
  let baseUrl = 'http://localhost:1234/v1'
  let apiKey = 'lm-studio'
  let model = ''
  let availableModels = []
  let testStatus = ''
  let testMessage = ''
  let autoRefineAction = 'off'
  let autoRefineCustomPrompt = ''
  let lastAutoRefineAction = 'off'
  let saving = false
  let saveError = ''
  let llamaManagedEnabled = false
  let llamaModelPath = ''
  let llamaPort = 8080
  let llamaContextSize = 4096
  let llamaStatus = { state: 'stopped', running: false, baseUrl: 'http://127.0.0.1:8080/v1', port: 8080 }
  let llamaBusy = false
  let llamaMessage = ''
  let llamaError = ''
  let llamaDownloadURL = ''
  let llamaDownloading = false
  let llamaDownloadFilename = ''
  let llamaDownloadDownloaded = 0
  let llamaDownloadTotal = 0

  // ── Voice settings ──
  let speechProvider = 'local'
  let speechApiKey = ''
  let speechModel = 'base.en'
  let speechLanguage = 'en'
  let speechPrompt = ''

  // ── Hotkey settings ──
  let hotkeyEnabled = false
  let hotkeyModifier = 'right_option'
  let hotkeyListening = false

  $: presetId = derivePreset(providerLabel)

  function derivePreset(label) {
    const match = PRESETS.find((p) => p.label === label)
    return match ? match.id : 'custom'
  }

  function applyPreset(id) {
    const p = PRESETS.find((x) => x.id === id)
    if (!p) return
    providerLabel = p.label
    llamaManagedEnabled = p.id === 'llamacpp'
    if (p.id !== 'custom') {
      baseUrl = p.baseUrl
      apiKey = p.apiKey
    }
    if (p.id === 'llamacpp') {
      baseUrl = `http://127.0.0.1:${llamaPort || 8080}/v1`
      apiKey = ''
    }
  }

  function getModifierLabel(value) {
    return modifierLabels[value] || value
  }

  // ── Hotkey capture ──
  function windowHotkeyHandler(e) {
    e.preventDefault()
    e.stopPropagation()
    e.stopImmediatePropagation()

    const isLeft  = e.location === 1
    const isRight = e.location === 2
    let detected = ''

    if (e.key === 'Alt')     { detected = isRight ? 'right_option' : 'left_option' }
    else if (e.key === 'Meta')    { detected = isRight ? 'right_cmd'    : 'left_cmd'    }
    else if (e.key === 'Control') { detected = isRight ? 'right_ctrl'   : 'left_ctrl'   }
    else if (e.key === 'Escape')  { stopHotkeyCapture(); return }

    if (detected) {
      hotkeyModifier = detected
      stopHotkeyCapture()
    }
  }

  function startHotkeyCapture() {
    hotkeyListening = true
    window.addEventListener('keydown', windowHotkeyHandler, true)
  }

  function stopHotkeyCapture() {
    hotkeyListening = false
    window.removeEventListener('keydown', windowHotkeyHandler, true)
  }

  // ── Backend ──
  async function loadSettings() {
    try {
      const s = await Backend.GetSettings()
      providerLabel     = s.providerLabel     || 'LM Studio'
      baseUrl           = s.baseUrl           || 'http://localhost:1234/v1'
      apiKey            = s.apiKey            || ''
      model             = s.model             || ''
      llamaManagedEnabled = s.llamaManagedEnabled || providerLabel === 'llama.cpp'
      llamaModelPath    = s.llamaModelPath    || ''
      llamaPort         = s.llamaPort         || 8080
      llamaContextSize  = s.llamaContextSize  || 4096
      autoRefineAction  = s.autoRefineAction  || 'off'
      autoRefineCustomPrompt = s.autoRefineCustomPrompt || REFINE_PROMPTS[autoRefineAction] || ''
      lastAutoRefineAction = autoRefineAction
      hotkeyEnabled     = s.hotkeyEnabled     || false
      hotkeyModifier    = s.hotkeyModifier    || 'right_option'
      speechProvider    = s.speechProvider    || 'local'
      speechApiKey      = s.speechApiKey      || ''
      speechModel       = s.speechModel       || (speechProvider === 'local' ? 'base.en' : 'gpt-4o-mini-transcribe')
      speechLanguage    = s.speechLanguage    || 'en'
      speechPrompt      = s.speechPrompt      || ''
      if (llamaManagedEnabled) baseUrl = `http://127.0.0.1:${llamaPort}/v1`
      await refreshLlamaStatus()
    } catch (e) {
      console.warn('GetSettings failed:', e)
    }
  }

  async function refreshLlamaStatus() {
    try {
      llamaStatus = await Backend.GetLlamaServerStatus()
      if (llamaStatus?.running && llamaStatus?.baseUrl && llamaManagedEnabled) {
        baseUrl = llamaStatus.baseUrl
      }
    } catch (e) {
      console.warn('GetLlamaServerStatus failed:', e)
    }
  }

  function formatBytes(n) {
    if (!n || n <= 0) return ''
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let v = n
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
    return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
  }

  $: llamaDownloadPercent = llamaDownloadTotal > 0
    ? Math.min(100, Math.floor((llamaDownloadDownloaded / llamaDownloadTotal) * 100))
    : 0

  async function downloadLlamaModel() {
    llamaError = ''
    llamaMessage = ''
    if (!llamaDownloadURL.trim()) {
      llamaError = 'Paste a Hugging Face GGUF URL first'
      return
    }
    llamaDownloading = true
    llamaDownloadDownloaded = 0
    llamaDownloadTotal = 0
    llamaDownloadFilename = ''
    try {
      const path = await Backend.DownloadLlamaModel(llamaDownloadURL.trim())
      if (path) {
        llamaModelPath = path
        llamaMessage = `Downloaded ${llamaDownloadFilename || 'model'}`
        llamaDownloadURL = ''
      }
    } catch (e) {
      llamaError = String(e?.message ?? e)
    } finally {
      llamaDownloading = false
    }
  }

  function handleDownloadProgress(payload) {
    if (!payload) return
    if (payload.filename) llamaDownloadFilename = payload.filename
    if (typeof payload.downloaded === 'number') llamaDownloadDownloaded = payload.downloaded
    if (typeof payload.total === 'number') llamaDownloadTotal = payload.total
  }

  async function chooseLlamaModel() {
    llamaError = ''
    try {
      const path = await Backend.PickLlamaModel()
      if (path) llamaModelPath = path
    } catch (e) {
      llamaError = String(e?.message ?? e)
    }
  }

  async function startLlamaServer() {
    llamaBusy = true
    llamaError = ''
    llamaMessage = 'Starting llama.cpp...'
    try {
      const result = await Backend.StartLlamaServer(llamaModelPath, Number(llamaPort) || 8080, Number(llamaContextSize) || 4096)
      llamaStatus = result?.status || llamaStatus
      availableModels = result?.models ?? []
      baseUrl = llamaStatus?.baseUrl || `http://127.0.0.1:${llamaPort || 8080}/v1`
      apiKey = ''
      if (!model && availableModels.length > 0) model = availableModels[0].id
      if (availableModels.length === 1 && (!model || model === '')) model = availableModels[0].id
      llamaMessage = `${availableModels.length} model${availableModels.length === 1 ? '' : 's'} available`
      testStatus = 'ok'
      testMessage = llamaMessage
    } catch (e) {
      llamaError = String(e?.message ?? e)
      testStatus = 'err'
      testMessage = llamaError
      availableModels = []
      await refreshLlamaStatus()
    } finally {
      llamaBusy = false
    }
  }

  async function stopLlamaServer() {
    llamaBusy = true
    llamaError = ''
    llamaMessage = ''
    try {
      llamaStatus = await Backend.StopLlamaServer()
      testStatus = ''
      testMessage = ''
    } catch (e) {
      llamaError = String(e?.message ?? e)
    } finally {
      llamaBusy = false
    }
  }

  async function testConnection() {
    testStatus = 'testing'
    testMessage = ''
    try {
      const list = await Backend.ListLocalModels(baseUrl, apiKey)
      availableModels = list ?? []
      testStatus = 'ok'
      testMessage = `${availableModels.length} model${availableModels.length === 1 ? '' : 's'} available`
      if (!model && availableModels.length > 0) model = availableModels[0].id
    } catch (e) {
      testStatus = 'err'
      testMessage = String(e?.message ?? e)
      availableModels = []
    }
  }

  async function save() {
    saving = true
    saveError = ''
    try {
      await Backend.SaveSettings({
        providerType: 'local-openai',
        providerLabel,
        baseUrl,
        apiKey,
        model,
        llamaManagedEnabled,
        llamaModelPath,
        llamaPort: Number(llamaPort) || 8080,
        llamaContextSize: Number(llamaContextSize) || 4096,
        hotkeyEnabled,
        hotkeyModifier,
        speechProvider,
        speechApiKey,
        speechModel,
        speechLanguage,
        speechPrompt,
        autoRefineAction,
        autoRefineCustomPrompt,
      })
      // Notify other components (e.g. FlowPanel banner) that settings changed.
      window.dispatchEvent(new CustomEvent('flow:settings-saved'))
      onClose()
    } catch (e) {
      saveError = String(e?.message ?? e)
    } finally {
      saving = false
    }
  }

  function handleClose() {
    stopHotkeyCapture()
    onClose()
  }

  $: if (autoRefineAction !== lastAutoRefineAction) {
    const previousDefault = REFINE_PROMPTS[lastAutoRefineAction] || ''
    const shouldUseDefault = !autoRefineCustomPrompt.trim() || autoRefineCustomPrompt === previousDefault
    if (shouldUseDefault) {
      autoRefineCustomPrompt = REFINE_PROMPTS[autoRefineAction] || ''
    }
    lastAutoRefineAction = autoRefineAction
  }

  $: if (llamaManagedEnabled) {
    baseUrl = `http://127.0.0.1:${Number(llamaPort) || 8080}/v1`
    apiKey = ''
  }

  $: if (open) {
    loadSettings()
    activeTab = 'general'
    Events.on('flow:llama:download:progress', handleDownloadProgress)
  }
  $: if (!open) {
    Events.off?.('flow:llama:download:progress')
  }

  onDestroy(() => {
    window.removeEventListener('keydown', windowHotkeyHandler, true)
    Events.off?.('flow:llama:download:progress')
  })
</script>

{#if open}
  <div class="overlay" on:click={handleClose} on:keydown={(e) => e.key === 'Escape' && handleClose()} role="presentation">
    <div class="modal" on:click|stopPropagation on:keydown|stopPropagation role="dialog" aria-modal="true" aria-labelledby="settings-title">
      <header>
        <h2 id="settings-title">Settings</h2>
        <button class="close" on:click={handleClose} title="Close settings">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
          </svg>
        </button>
      </header>

      <!-- Tab bar -->
      <div class="tab-bar">
        <button class="tab-btn" class:tab-active={activeTab === 'general'} on:click={() => activeTab = 'general'} type="button">General</button>
        <button class="tab-btn" class:tab-active={activeTab === 'voice'}   on:click={() => activeTab = 'voice'}   type="button">Voice / STT</button>
        <button class="tab-btn" class:tab-active={activeTab === 'hotkeys'} on:click={() => activeTab = 'hotkeys'} type="button">Hotkeys</button>
      </div>

      <div class="modal-body">

        <!-- ── General Tab ── -->
        {#if activeTab === 'general'}
          <section>
            <span class="row-label">Provider</span>
            <div class="presets">
              {#each PRESETS as p}
                <button class="preset" class:active={presetId === p.id} on:click={() => applyPreset(p.id)}>{p.label}</button>
              {/each}
            </div>
          </section>

          <section>
            <label class="row-label" for="baseUrl">Base URL</label>
            <input id="baseUrl" type="text" bind:value={baseUrl} placeholder="http://localhost:1234/v1" readonly={llamaManagedEnabled} />
          </section>

          {#if llamaManagedEnabled}
            <section class="llama-panel">
              <div class="llama-panel-header">
                <div>
                  <span class="row-label">Managed llama.cpp</span>
                  <p class="hint panel-hint">Flow starts llama-server locally with your GGUF model.</p>
                </div>
                <span class="status-pill" class:status-running={llamaStatus?.running} class:status-error={llamaStatus?.state === 'error'}>
                  {llamaStatus?.state || 'stopped'}
                </span>
              </div>

              <label class="row-label" for="llamaModelPath">Model file</label>
              <div class="row">
                <input id="llamaModelPath" type="text" bind:value={llamaModelPath} placeholder="/path/to/model.gguf" />
                <button class="secondary" on:click={chooseLlamaModel} disabled={llamaBusy || llamaDownloading}>Choose model</button>
              </div>

              <label class="row-label download-label" for="llamaDownloadURL">Or download from Hugging Face</label>
              <div class="row">
                <input
                  id="llamaDownloadURL"
                  type="text"
                  bind:value={llamaDownloadURL}
                  placeholder="https://huggingface.co/.../model.gguf"
                  disabled={llamaDownloading}
                />
                <button class="secondary" on:click={downloadLlamaModel} disabled={llamaDownloading || !llamaDownloadURL.trim()}>
                  {llamaDownloading ? 'Downloading…' : 'Download'}
                </button>
              </div>
              {#if llamaDownloading || (llamaDownloadFilename && llamaDownloadDownloaded > 0)}
                <div class="download-progress">
                  <div class="download-bar">
                    <div class="download-bar-fill" style="width: {llamaDownloadPercent}%"></div>
                  </div>
                  <div class="download-meta">
                    <span>{llamaDownloadFilename || 'model.gguf'}</span>
                    <span>
                      {formatBytes(llamaDownloadDownloaded)}{llamaDownloadTotal > 0 ? ` / ${formatBytes(llamaDownloadTotal)}` : ''}
                      {llamaDownloadTotal > 0 ? ` (${llamaDownloadPercent}%)` : ''}
                    </span>
                  </div>
                </div>
              {/if}
              <p class="hint">Replaces any previously-downloaded model in Flow's managed folder.</p>

              <div class="llama-grid">
                <label>
                  <span class="row-label">Port</span>
                  <input type="number" min="1024" max="65535" bind:value={llamaPort} />
                </label>
                <label>
                  <span class="row-label">Context</span>
                  <input type="number" min="512" step="512" bind:value={llamaContextSize} />
                </label>
              </div>

              <div class="row llama-actions">
                {#if llamaStatus?.running}
                  <button class="secondary" on:click={stopLlamaServer} disabled={llamaBusy}>Stop</button>
                {:else}
                  <button class="secondary" on:click={startLlamaServer} disabled={llamaBusy || llamaDownloading || !llamaModelPath}>
                    {llamaBusy ? 'Starting...' : 'Start'}
                  </button>
                {/if}
                <button class="secondary" on:click={testConnection} disabled={testStatus === 'testing'}>
                  {testStatus === 'testing' ? 'Testing...' : 'Test connection'}
                </button>
              </div>
              {#if llamaMessage}
                <div class="feedback ok">{llamaMessage}</div>
              {/if}
              {#if llamaError}
                <div class="feedback err">{llamaError}</div>
              {/if}
              {#if llamaStatus?.logExcerpt}
                <pre class="llama-log">{llamaStatus.logExcerpt}</pre>
              {/if}
            </section>
          {:else}
            <section>
              <label class="row-label" for="apiKey">API key (optional)</label>
              <input id="apiKey" type="password" bind:value={apiKey} placeholder="lm-studio" />
            </section>
          {/if}

          <section>
            <label class="row-label" for="modelSelect">Model</label>
            <div class="row">
              {#if availableModels.length > 0}
                <select id="modelSelect" bind:value={model}>
                  {#each availableModels as m}
                    <option value={m.id}>{m.id}</option>
                  {/each}
                </select>
              {:else}
                <input id="modelSelect" type="text" bind:value={model} placeholder="qwen2.5-coder-7b-instruct" />
              {/if}
              {#if !llamaManagedEnabled}
                <button class="secondary" on:click={testConnection} disabled={testStatus === 'testing'}>
                  {testStatus === 'testing' ? 'Testing…' : 'Test connection'}
                </button>
              {/if}
            </div>
            {#if testStatus === 'ok'}
              <div class="feedback ok">✓ {testMessage}</div>
            {:else if testStatus === 'err'}
              <div class="feedback err">✗ {testMessage}</div>
            {/if}
          </section>

          <section>
            <label class="row-label" for="autoRefine">Auto-refine on stop</label>
            <select id="autoRefine" bind:value={autoRefineAction}>
              {#each REFINE_OPTIONS as o}
                <option value={o.value}>{o.label}</option>
              {/each}
            </select>
            {#if autoRefineAction !== 'off'}
              <label class="row-label prompt-label" for="autoRefinePrompt">Prompt</label>
              <textarea
                id="autoRefinePrompt"
                class="custom-prompt-input"
                bind:value={autoRefineCustomPrompt}
                placeholder="Tell the LLM how to transform each recording after stop."
              ></textarea>
            {/if}
            <p class="hint">When set, every recording is automatically piped through the LLM for cleanup.</p>
          </section>

        <!-- ── Voice / STT Tab ── -->
        {:else if activeTab === 'voice'}
          <section>
            <label class="row-label" for="speechProvider">Transcription Provider</label>
            <select id="speechProvider" bind:value={speechProvider}>
              {#each SPEECH_PROVIDERS as p}
                <option value={p.value}>{p.label}</option>
              {/each}
            </select>
            {#if speechProvider === 'local'}
              <p class="hint">Runs whisper.cpp on-device. The model file (~140 MB for base.en) downloads automatically the first time you record.</p>
            {/if}
          </section>

          <section>
            <label class="row-label" for="speechModel">Transcription Model</label>
            {#if speechProvider === 'local'}
              <select id="speechModel" bind:value={speechModel}>
                {#each SPEECH_MODELS_LOCAL as m}
                  <option value={m.value}>{m.label}</option>
                {/each}
              </select>
            {:else}
              <select id="speechModel" bind:value={speechModel}>
                {#each SPEECH_MODELS_OPENAI as m}
                  <option value={m.value}>{m.label}</option>
                {/each}
              </select>
            {/if}
          </section>

          {#if speechProvider !== 'local'}
            <section>
              <label class="row-label" for="speechApiKey">
                {speechProvider === 'deepgram' ? 'Deepgram API Key' : 'OpenAI API Key'}
              </label>
              <input id="speechApiKey" type="password" bind:value={speechApiKey}
                placeholder={speechProvider === 'deepgram' ? 'dg-...' : 'sk-...'} />
            </section>
          {/if}

        <!-- ── Hotkeys Tab ── -->
        {:else if activeTab === 'hotkeys'}
          <div class="hotkeys-section">
            <h3 class="section-title">Push-to-Talk Dictation</h3>
            <p class="section-desc">
              Hold a modifier key anywhere on your Mac to record. Release to transcribe and paste text into the focused app.
            </p>

            <div class="dictation-toggle-row">
              <span class="dictation-toggle-label">Enable global hotkey</span>
              <button
                class="toggle-switch"
                class:toggle-active={hotkeyEnabled}
                on:click={() => hotkeyEnabled = !hotkeyEnabled}
                type="button"
                role="switch"
                aria-checked={hotkeyEnabled}
              >
                <span class="toggle-knob"></span>
              </button>
            </div>

            {#if hotkeyEnabled}
              <label class="row-label" for="hotkey-capture" style="margin-top: 18px;">Hotkey (hold to record)</label>
              <div
                id="hotkey-capture"
                class="hotkey-capture-input"
                class:hotkey-listening={hotkeyListening}
                role="button"
                tabindex="0"
                on:click={hotkeyListening ? stopHotkeyCapture : startHotkeyCapture}
                on:keydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    hotkeyListening ? stopHotkeyCapture() : startHotkeyCapture()
                  }
                }}
              >
                {#if hotkeyListening}
                  <span class="hotkey-listening-text">Press a modifier key…</span>
                {:else}
                  <span class="hotkey-current-key">{getModifierLabel(hotkeyModifier)}</span>
                  <span class="hotkey-change-hint">Click to change</span>
                {/if}
              </div>
              <p class="hint" style="margin-top: 6px;">
                {#if hotkeyListening}
                  Press <strong>Option</strong>, <strong>Command</strong>, or <strong>Control</strong> (left or right). Press <strong>Escape</strong> to cancel.
                {:else}
                  Hold this key to record, release to transcribe. Short taps (&lt;300ms) are ignored.
                {/if}
              </p>

              <div class="hotkey-shortcuts-card">
                <div class="hotkey-shortcut-row">
                  <kbd class="kbd">{getModifierLabel(hotkeyModifier)}</kbd>
                  <span class="shortcut-desc">Hold to record, release to transcribe &amp; paste</span>
                </div>
                <div class="hotkey-shortcut-row">
                  <kbd class="kbd">⌘ Shift R</kbd>
                  <span class="shortcut-desc">Toggle recording in Flow window</span>
                </div>
              </div>
            {/if}
          </div>
        {/if}

      </div>

      <footer>
        {#if saveError}
          <span class="save-error">{saveError}</span>
        {/if}
        <button class="btn-cancel" on:click={handleClose}>Cancel</button>
        <button class="primary" on:click={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </footer>
    </div>
  </div>
{/if}

<style>
  /* ── Overlay / Modal ── */
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
    animation: fadeIn 0.15s ease;
  }
  @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

  .modal {
    width: 500px;
    height: 580px;
    max-width: calc(100vw - 32px);
    max-height: calc(100vh - 64px);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 14px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    animation: modalSlideUp 0.2s ease;
    overflow: hidden;
    color: var(--text-primary);
  }
  @keyframes modalSlideUp {
    from { opacity: 0; transform: translateY(12px) scale(0.98); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 18px 20px 0;
    flex-shrink: 0;
  }
  h2 { margin: 0; font-size: 16px; font-weight: 600; }

  .close {
    display: flex; align-items: center; justify-content: center;
    width: 28px; height: 28px;
    background: transparent; border: none; border-radius: 6px;
    color: var(--text-muted); cursor: pointer;
    transition: all 0.15s ease;
  }
  .close:hover { background: var(--bg-hover); color: var(--text-primary); }

  /* ── Tab Bar ── */
  .tab-bar {
    display: flex;
    padding: 0 20px;
    margin-top: 12px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }
  .tab-btn {
    position: relative;
    padding: 10px 16px;
    background: transparent; border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    font-size: 13px; font-weight: 500; font-family: inherit;
    cursor: pointer;
    transition: all 0.15s ease;
    margin-bottom: -1px;
  }
  .tab-btn:hover { color: var(--text-secondary); }
  .tab-btn.tab-active { color: var(--text-primary); border-bottom-color: var(--accent); }

  /* ── Body ── */
  .modal-body {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
  }

  section { margin-bottom: 18px; }
  .row-label {
    display: block; font-size: 11px; text-transform: uppercase;
    letter-spacing: 0.08em; color: var(--text-secondary); margin-bottom: 8px;
  }

  .presets { display: flex; gap: 8px; }
  .preset {
    flex: 1; background: var(--bg-card); color: var(--text-secondary);
    border: 1px solid var(--border-subtle); padding: 8px 10px;
    border-radius: var(--radius-sm); font-size: 13px; transition: all 0.12s;
  }
  .preset:hover { background: var(--bg-hover); }
  .preset.active { background: var(--bg-elevated); border-color: var(--accent); color: var(--text-primary); }

  input, select, textarea {
    width: 100%; background: var(--bg-app); color: var(--text-primary);
    border: 1px solid var(--border-subtle); border-radius: var(--radius-sm);
    padding: 8px 10px; font-size: 13px; font-family: inherit;
    box-sizing: border-box;
  }
  input:focus, select:focus, textarea:focus { outline: none; border-color: var(--accent); }

  .custom-prompt-input {
    resize: vertical; min-height: 60px; margin-top: 8px;
  }

  .prompt-label {
    margin-top: 10px;
  }

  .row { display: flex; gap: 8px; }
  .row > select, .row > input { flex: 1; }

  .secondary {
    background: var(--bg-card); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 8px 14px; font-size: 12px; white-space: nowrap;
    transition: background 0.12s; cursor: pointer;
  }
  .secondary:hover { background: var(--bg-elevated); }
  .secondary:disabled { opacity: 0.6; cursor: not-allowed; }

  .feedback { margin-top: 8px; font-size: 12px; }
  .feedback.ok { color: var(--accent); }
  .feedback.err { color: #f87171; }
  .hint { color: var(--text-muted); font-size: 11px; margin-top: 6px; line-height: 1.4; }

  .llama-panel {
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md, 8px);
    padding: 14px;
  }
  .llama-panel-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .panel-hint { margin: -2px 0 0; }
  .status-pill {
    flex-shrink: 0;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-muted);
    font-size: 11px;
    padding: 3px 8px;
    text-transform: capitalize;
  }
  .status-pill.status-running {
    border-color: rgba(45, 212, 191, 0.45);
    color: var(--accent);
  }
  .status-pill.status-error {
    border-color: rgba(248, 113, 113, 0.45);
    color: #f87171;
  }
  .llama-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
    margin-top: 12px;
  }
  .llama-actions { margin-top: 12px; }
  .download-label { margin-top: 12px; }
  .download-progress { margin-top: 10px; }
  .download-bar {
    height: 6px;
    background: var(--bg-app);
    border: 1px solid var(--border-subtle);
    border-radius: 999px;
    overflow: hidden;
  }
  .download-bar-fill {
    height: 100%;
    background: var(--accent);
    transition: width 0.15s ease;
  }
  .download-meta {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    margin-top: 6px;
    font-size: 11px;
    color: var(--text-muted);
  }
  .llama-log {
    max-height: 90px;
    overflow: auto;
    margin: 10px 0 0;
    padding: 8px;
    background: var(--bg-app);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 11px;
    white-space: pre-wrap;
  }

  /* ── Hotkeys Tab ── */
  .hotkeys-section { padding-bottom: 4px; }

  .section-title {
    font-size: 14px; font-weight: 600;
    color: var(--text-primary); margin: 0 0 8px;
  }
  .section-desc {
    font-size: 12px; color: var(--text-muted);
    margin: 0 0 20px; line-height: 1.5;
  }

  .dictation-toggle-row {
    display: flex; align-items: center;
    justify-content: space-between; margin-bottom: 6px;
  }
  .dictation-toggle-label { font-size: 13px; font-weight: 500; color: var(--text-primary); }

  .toggle-switch {
    position: relative; width: 44px; height: 24px;
    background: var(--bg-app); border: 1px solid var(--border);
    border-radius: 12px; cursor: pointer;
    transition: all 0.2s ease; padding: 0; flex-shrink: 0;
  }
  .toggle-switch.toggle-active { background: var(--accent); border-color: var(--accent); }
  .toggle-knob {
    position: absolute; top: 2px; left: 2px;
    width: 18px; height: 18px; border-radius: 50%;
    background: var(--text-muted); transition: all 0.2s ease;
  }
  .toggle-switch.toggle-active .toggle-knob { left: 22px; background: white; }

  /* Hotkey capture widget */
  .hotkey-capture-input {
    width: 100%; padding: 10px 12px;
    background: var(--bg-app); border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm); font-size: 13px;
    display: flex; align-items: center; justify-content: space-between;
    cursor: pointer; user-select: none; min-height: 40px;
    box-sizing: border-box; transition: border-color 0.15s, box-shadow 0.15s;
  }
  .hotkey-capture-input:hover { border-color: var(--border); }
  .hotkey-capture-input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px rgba(45,212,191,0.15); }
  .hotkey-capture-input.hotkey-listening {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(45,212,191,0.15);
    animation: hotkey-pulse 1.5s ease-in-out infinite;
  }
  @keyframes hotkey-pulse {
    0%, 100% { box-shadow: 0 0 0 3px rgba(45,212,191,0.15); }
    50%       { box-shadow: 0 0 0 5px rgba(45,212,191,0.2), 0 0 12px rgba(45,212,191,0.1); }
  }
  .hotkey-listening-text {
    color: var(--accent); font-weight: 500;
    animation: hotkey-blink 1s ease-in-out infinite;
  }
  @keyframes hotkey-blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
  .hotkey-current-key { font-weight: 500; color: var(--text-primary); }
  .hotkey-change-hint { font-size: 11px; color: var(--text-muted); }

  /* Shortcuts reference card */
  .hotkey-shortcuts-card {
    margin-top: 18px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md, 8px);
    padding: 12px 14px;
    display: flex; flex-direction: column; gap: 10px;
  }
  .hotkey-shortcut-row {
    display: flex; align-items: center; gap: 12px;
  }
  .kbd {
    display: inline-block; padding: 3px 8px;
    background: var(--bg-app);
    border: 1px solid var(--border);
    border-radius: 5px;
    font-family: inherit; font-size: 11px; font-weight: 600;
    color: var(--text-primary); white-space: nowrap; flex-shrink: 0;
    letter-spacing: 0.2px;
  }
  .shortcut-desc { font-size: 12px; color: var(--text-muted); }

  /* ── Footer ── */
  footer {
    display: flex; justify-content: flex-end; align-items: center;
    gap: 8px; padding: 0 20px 20px;
    border-top: 1px solid var(--border-subtle);
    padding-top: 14px; flex-shrink: 0;
  }
  .save-error { color: #f87171; font-size: 12px; flex: 1; }

  .btn-cancel {
    padding: 8px 16px; background: transparent;
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    color: var(--text-secondary); font-size: 13px; font-family: inherit;
    cursor: pointer; transition: all 0.15s ease;
  }
  .btn-cancel:hover { background: var(--bg-hover); color: var(--text-primary); }

  .primary {
    background: var(--accent); color: var(--bg-app);
    border: none; border-radius: var(--radius-sm);
    padding: 9px 22px; font-size: 13px; font-weight: 500;
    transition: background 0.12s; cursor: pointer;
  }
  .primary:hover { background: var(--accent-dim); }
  .primary:disabled { opacity: 0.6; cursor: not-allowed; }
</style>
