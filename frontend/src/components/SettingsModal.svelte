<script>
  import { onDestroy } from 'svelte'
  import { Backend, Events } from '../lib/wails.js'
  import appIcon from '../assets/appicon.png'

  export let open = false
  export let onClose = () => {}

  // ── Update state ──
  let appVersion = ''
  let updateInfo = null       // { latestVersion, downloadUrl, releaseNotes, releaseUrl }
  let updateChecking = false
  let updateDownloading = false
  let updateProgress = { downloaded: 0, total: 0, message: '' }
  let showUpdatePanel = false

  const LLAMA_MANAGED_LABEL = 'llama.cpp (managed)'
  const DEFAULT_LOCAL_BASE_URL = 'http://localhost:1234/v1'

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
  let activeTab = 'general' // 'general' | 'llm' | 'voice'

  const CLOUD_PROVIDERS = [
    { id: 'openai',     label: 'OpenAI',     keyPlaceholder: 'sk-...',     modelPlaceholder: 'gpt-4o-mini' },
    { id: 'anthropic',  label: 'Claude',     keyPlaceholder: 'sk-ant-...', modelPlaceholder: 'claude-sonnet-4-5-20250929' },
    { id: 'openrouter', label: 'OpenRouter', keyPlaceholder: 'sk-or-...',  modelPlaceholder: 'anthropic/claude-sonnet-4.5' },
    { id: 'custom',     label: 'Custom',     keyPlaceholder: '',           modelPlaceholder: '' },
  ]

  // ── General / model settings ──
  let providerLabel = 'Local'
  let baseUrl = DEFAULT_LOCAL_BASE_URL
  let apiKey = ''
  let preManagedBaseUrl = ''
  let preManagedApiKey = ''
  let model = 'gemma-4-E2B-it-Q4_K_M.gguf'
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
  let llamaDownloadURL = 'https://huggingface.co/unsloth/gemma-4-E2B-it-GGUF/resolve/main/gemma-4-E2B-it-Q4_K_M.gguf?download=true'
  let llamaDownloading = false
  let llamaDownloadFilename = ''
  let llamaDownloadDownloaded = 0
  let llamaDownloadTotal = 0

  // ── Cloud settings ──
  let providerMode = 'none'  // 'local' | 'cloud' | 'none'
  let cloudProvider = 'openai'  // 'openai' | 'anthropic' | 'openrouter' | 'custom'
  let cloudModel = ''
  let openaiKey = ''
  let anthropicKey = ''
  let openRouterKey = ''
  let customCloudURL = ''
  let customCloudKey = ''
  let cloudTestStatus = ''
  let cloudTestMessage = ''

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

  // ── Quick Agent HUD hotkey ──
  let quickAskHotkeyEnabled = false
  let quickAskHotkeyModifier = 'left_option'
  let screenPermission = null   // null = unknown, true/false = checked
  let checkingScreenPerm = false
  let axPermission = null        // Accessibility permission
  let checkingAxPerm = false

  async function refreshScreenPermission() {
    try {
      screenPermission = await Backend.CheckScreenPermission()
    } catch (e) {
      console.warn('screen permission check failed', e)
    }
  }

  async function grantScreenPermission() {
    checkingScreenPerm = true
    try {
      // Triggers the system prompt the first time, or opens System Settings if
      // previously denied.
      screenPermission = await Backend.RequestScreenPermission()
    } catch (e) {
      console.warn('screen permission request failed', e)
    } finally {
      checkingScreenPerm = false
    }
  }

  async function refreshAxPermission() {
    try {
      axPermission = await Backend.CheckAccessibilityPermission()
    } catch (e) {
      console.warn('accessibility permission check failed', e)
    }
  }

  async function grantAxPermission() {
    checkingAxPerm = true
    try {
      axPermission = await Backend.RequestAccessibilityPermission()
    } catch (e) {
      console.warn('accessibility permission request failed', e)
    } finally {
      checkingAxPerm = false
    }
  }

  // Check both permissions the Quick Agent HUD relies on.
  function refreshHudPermissions() {
    refreshScreenPermission()
    refreshAxPermission()
  }

  // ── Command Approvals settings ──
  let allowedCommands = []
  let blockedCommands = []
  let newAllowedCommand = ''
  let pythonPath = 'python3'

  async function loadApprovals() {
    try {
      const approvals = await Backend.GetExecApprovals()
      allowedCommands = approvals?.allowed || []
      blockedCommands = approvals?.blocked || []
    } catch (e) {
      console.warn('Failed to load approvals:', e)
    }
  }

  function addAllowedCommand() {
    const cmd = newAllowedCommand.trim()
    if (!cmd) return
    if (!allowedCommands.includes(cmd)) {
      allowedCommands = [...allowedCommands, cmd]
    }
    newAllowedCommand = ''
  }

  function removeAllowedCommand(cmd) {
    allowedCommands = allowedCommands.filter(c => c !== cmd)
  }

  function setManagedLlama(enabled) {
    if (enabled && !llamaManagedEnabled) {
      // Capture user's URL/key so we can restore them when toggling off.
      preManagedBaseUrl = baseUrl
      preManagedApiKey = apiKey
    }
    llamaManagedEnabled = enabled
    if (enabled) {
      providerLabel = LLAMA_MANAGED_LABEL
      baseUrl = `http://127.0.0.1:${Number(llamaPort) || 8080}/v1`
      apiKey = ''
    } else {
      providerLabel = 'Local'
      baseUrl = preManagedBaseUrl || DEFAULT_LOCAL_BASE_URL
      apiKey = preManagedApiKey || ''
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
      providerLabel     = s.providerLabel     || 'Local'
      baseUrl           = s.baseUrl           || DEFAULT_LOCAL_BASE_URL
      apiKey            = s.apiKey            || ''
      model             = s.model             || 'gemma-4-E2B-it-Q4_K_M.gguf'
      llamaManagedEnabled = s.llamaManagedEnabled || providerLabel === 'llama.cpp' || providerLabel === LLAMA_MANAGED_LABEL
      // Remember pre-managed values so toggling managed off restores user's URL/key.
      if (!llamaManagedEnabled) {
        preManagedBaseUrl = baseUrl
        preManagedApiKey = apiKey
      }
      llamaModelPath    = s.llamaModelPath    || ''
      llamaDownloadURL  = s.llamaDownloadURL  || 'https://huggingface.co/unsloth/gemma-4-E2B-it-GGUF/resolve/main/gemma-4-E2B-it-Q4_K_M.gguf?download=true'
      llamaPort         = s.llamaPort         || 8080
      llamaContextSize  = s.llamaContextSize  || 4096
      autoRefineAction  = s.autoRefineAction  || 'off'
      autoRefineCustomPrompt = s.autoRefineCustomPrompt || REFINE_PROMPTS[autoRefineAction] || ''
      lastAutoRefineAction = autoRefineAction
      hotkeyEnabled     = s.hotkeyEnabled     || false
      hotkeyModifier    = s.hotkeyModifier    || 'right_option'
      quickAskHotkeyEnabled  = s.quickAskHotkeyEnabled  || false
      quickAskHotkeyModifier = s.quickAskHotkeyModifier || 'left_option'
      if (quickAskHotkeyEnabled) refreshHudPermissions()
      speechProvider    = s.speechProvider    || 'local'
      speechApiKey      = s.speechApiKey      || ''
      speechModel       = s.speechModel       || (speechProvider === 'local' ? 'base.en' : 'gpt-4o-mini-transcribe')
      speechLanguage    = s.speechLanguage    || 'en'
      speechPrompt      = s.speechPrompt      || ''
      providerMode      = s.providerMode      || 'none'
      cloudProvider     = s.cloudProvider     || 'openai'
      cloudModel        = s.cloudModel        || ''
      openaiKey         = s.openaiKey         || ''
      anthropicKey      = s.anthropicKey      || ''
      openRouterKey     = s.openRouterKey     || ''
      customCloudURL    = s.customCloudURL    || ''
      customCloudKey    = s.customCloudKey    || ''
      if (llamaManagedEnabled) baseUrl = `http://127.0.0.1:${llamaPort}/v1`
      pythonPath        = s.pythonPath        || 'python3'
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
      const modelName = llamaModelPath.split('/').pop() || 'Local Model'
      window.dispatchEvent(new CustomEvent('flow:local-llm-loading', {
        detail: { loading: true, modelName }
      }))
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
      window.dispatchEvent(new CustomEvent('flow:local-llm-loading', {
        detail: { loading: false }
      }))
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

  // ── Cloud-tab helpers ──
  function cloudKeyFor(p) {
    if (p === 'openai')     return openaiKey
    if (p === 'anthropic')  return anthropicKey
    if (p === 'openrouter') return openRouterKey
    if (p === 'custom')     return customCloudKey
    return ''
  }

  async function testCloudConnection() {
    cloudTestStatus = 'testing'
    cloudTestMessage = ''
    if (!cloudProvider) {
      cloudTestStatus = 'err'
      cloudTestMessage = 'Pick a provider first'
      return
    }
    if (!cloudKeyFor(cloudProvider)) {
      cloudTestStatus = 'err'
      cloudTestMessage = 'API key required'
      return
    }
    // Anthropic doesn't expose /v1/models in OpenAI shape — skip live check.
    if (cloudProvider === 'anthropic') {
      cloudTestStatus = 'ok'
      cloudTestMessage = 'Key set (live check not supported for Anthropic)'
      return
    }
    let url = ''
    if (cloudProvider === 'openai')     url = 'https://api.openai.com/v1'
    if (cloudProvider === 'openrouter') url = 'https://openrouter.ai/api/v1'
    if (cloudProvider === 'custom')     url = customCloudURL.replace(/\/chat\/completions$/, '')
    if (!url) {
      cloudTestStatus = 'err'
      cloudTestMessage = 'Custom URL required'
      return
    }
    try {
      const list = await Backend.ListLocalModels(url, cloudKeyFor(cloudProvider))
      cloudTestStatus = 'ok'
      cloudTestMessage = `${(list ?? []).length} model${(list ?? []).length === 1 ? '' : 's'} available`
    } catch (e) {
      cloudTestStatus = 'err'
      cloudTestMessage = String(e?.message ?? e)
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
        llamaDownloadURL,
        llamaPort: Number(llamaPort) || 8080,
        llamaContextSize: Number(llamaContextSize) || 4096,
        hotkeyEnabled,
        hotkeyModifier,
        quickAskHotkeyEnabled,
        quickAskHotkeyModifier,
        speechProvider,
        speechApiKey,
        speechModel,
        speechLanguage,
        speechPrompt,
        autoRefineAction,
        autoRefineCustomPrompt,
        providerMode,
        cloudProvider,
        cloudModel,
        openaiKey,
        anthropicKey,
        openRouterKey,
        customCloudURL,
        customCloudKey,
        pythonPath,
      })
      // Save approvals
      await Backend.SaveExecApprovals(allowedCommands, blockedCommands);

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
    loadApprovals()
    loadAppVersion()
    activeTab = 'general'
    Events.on('flow:llama:download:progress', handleDownloadProgress)
    Events.on('flow:update:available', handleUpdateAvailable)
    Events.on('flow:update:progress', handleUpdateProgress)
  }
  $: if (!open) {
    Events.off?.('flow:llama:download:progress')
    Events.off?.('flow:update:available')
    Events.off?.('flow:update:progress')
  }

  onDestroy(() => {
    window.removeEventListener('keydown', windowHotkeyHandler, true)
    Events.off?.('flow:llama:download:progress')
    Events.off?.('flow:update:available')
    Events.off?.('flow:update:progress')
  })

  async function loadAppVersion() {
    try {
      appVersion = await Backend.GetAppVersion()
    } catch (e) {
      appVersion = ''
    }
  }

  function handleUpdateAvailable(payload) {
    if (!payload) return
    updateInfo = {
      latestVersion: payload.latestVersion || '',
      downloadUrl: payload.downloadUrl || '',
      releaseNotes: payload.releaseNotes || '',
      releaseUrl: payload.releaseUrl || '',
    }
  }

  function handleUpdateProgress(payload) {
    if (!payload) return
    updateProgress = {
      downloaded: payload.downloaded || 0,
      total: payload.total || 0,
      message: payload.message || '',
    }
  }

  async function checkForUpdates() {
    updateChecking = true
    try {
      const info = await Backend.CheckForUpdates()
      if (info && info.available) {
        updateInfo = {
          latestVersion: info.latestVersion,
          downloadUrl: info.downloadUrl,
          releaseNotes: info.releaseNotes,
          releaseUrl: info.releaseUrl,
        }
      } else {
        updateInfo = null
      }
    } catch (e) {
      console.warn('Update check failed:', e)
    }
    updateChecking = false
  }

  async function installUpdate() {
    if (!updateInfo?.downloadUrl) return
    updateDownloading = true
    updateProgress = { downloaded: 0, total: 0, message: 'Starting…' }
    try {
      await Backend.DownloadAndInstallUpdate(updateInfo.downloadUrl)
    } catch (e) {
      updateDownloading = false
      updateProgress.message = `Error: ${e?.message || e}`
    }
  }
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
        <button class="tab-btn" class:tab-active={activeTab === 'llm'}     on:click={() => activeTab = 'llm'}     type="button">LLM Settings</button>
        <button class="tab-btn" class:tab-active={activeTab === 'voice'}   on:click={() => activeTab = 'voice'}   type="button">Voice / STT</button>
      </div>

      <div class="modal-body">

        <!-- ── General Tab (Allowed Commands Only) ── -->
        {#if activeTab === 'general'}
          <section>
            <span class="row-label">Allowed Bash Commands</span>
            <p class="hint" style="margin-bottom: 10px;">
              Specify command prefixes that Flow is allowed to run. Enter a command prefix and press Enter or click Add.
            </p>
            <div class="allowed-commands-list">
              {#each allowedCommands as cmd}
                <span class="command-tag">
                  <code>{cmd}</code>
                  <button type="button" class="command-remove-btn" on:click={() => removeAllowedCommand(cmd)} title="Remove command">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none">
                      <path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
                    </svg>
                  </button>
                </span>
              {/each}
              {#if allowedCommands.length === 0}
                <span style="font-size: 12px; color: var(--text-muted);">No commands allowed. Bash access is disabled.</span>
              {/if}
            </div>
            
            <div class="row" style="margin-top: 10px;">
              <input
                type="text"
                bind:value={newAllowedCommand}
                placeholder="e.g., git, npm, ls, python"
                on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), addAllowedCommand())}
              />
              <button type="button" class="secondary" on:click={addAllowedCommand}>Add</button>
            </div>
          </section>

          <!-- Divider line to separate Allowed Bash Commands from Python Config -->
          <div style="height: 1px; background: var(--border-subtle); margin: 20px 0;"></div>

          <section>
            <label class="row-label" for="pythonPath">Python Executable Path</label>
            <p class="hint" style="margin-bottom: 8px;">
              Specify the path to the Python executable Flow should use (e.g. python3, venv/bin/python, or absolute path).
            </p>
            <div class="row">
              <input
                id="pythonPath"
                type="text"
                bind:value={pythonPath}
                placeholder="e.g., python3, /usr/bin/python3, or venv path"
              />
            </div>
          </section>

          <!-- Divider line to separate Bash Approvals from App Info -->
          <div style="height: 1px; background: var(--border-subtle); margin: 24px 0 16px;"></div>

          <!-- About / Update section -->
          <div class="about-section" style="padding: 4px 0;">
            <div style="display: flex; align-items: center; justify-content: space-between;">
              <div style="display: flex; align-items: center; gap: 8px;">
                <img src={appIcon} alt="Flow" style="width: 22px; height: 22px; border-radius: 4px; object-fit: cover;" />
                <div style="display: flex; flex-direction: column; line-height: 1.2;">
                  <span style="font-size: 11px; font-weight: 600; color: var(--text-primary);">Flow Developer Agent</span>
                  <span style="font-size: 10px; color: var(--text-muted);">
                    Version {appVersion || '…'}
                    {#if updateInfo}
                      <span style="color: #4ade80; margin-left: 6px;">→ v{updateInfo.latestVersion} available</span>
                    {/if}
                  </span>
                </div>
              </div>
              <div style="display: flex; align-items: center; gap: 8px;">
                {#if updateInfo}
                  <button class="update-badge" on:click={() => showUpdatePanel = !showUpdatePanel}>
                    Update available
                  </button>
                {:else if updateChecking}
                  <span style="font-size: 10px; color: var(--text-muted);">Checking…</span>
                {:else}
                  <button class="check-update-btn" on:click={checkForUpdates}>Check for updates</button>
                {/if}
                <span style="font-size: 10px; color: var(--text-muted); opacity: 0.85;">© 2026 Flow</span>
              </div>
            </div>

            {#if showUpdatePanel && updateInfo}
              <div class="update-panel">
                <div class="update-panel-header">
                  <span class="update-panel-title">Flow v{updateInfo.latestVersion}</span>
                  <span style="font-size: 10px; color: var(--text-muted);">from v{appVersion}</span>
                </div>
                {#if updateInfo.releaseNotes}
                  <div class="update-notes">{updateInfo.releaseNotes}</div>
                {/if}
                {#if updateDownloading}
                  <div class="update-progress">
                    <div class="update-progress-bar">
                      <div class="update-progress-fill" style="width: {updateProgress.total > 0 ? (updateProgress.downloaded / updateProgress.total * 100) : 0}%"></div>
                    </div>
                    <span class="update-progress-text">{updateProgress.message}</span>
                  </div>
                {:else}
                  <div class="update-actions">
                    <button class="update-install-btn" on:click={installUpdate}>Download & Install</button>
                    <button class="update-release-link" on:click={() => updateInfo.releaseUrl && window.open(updateInfo.releaseUrl, '_blank')}>View on GitHub</button>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        {/if}

        <!-- ── LLM Settings Tab (Local/Cloud LLM Setup) ── -->
        {#if activeTab === 'llm'}
          <div class="config-sections-wrapper" style="margin-top: 8px;">
            <!-- Local Endpoint Configuration Card -->
            <div class="config-card" class:active-card={providerMode === 'local'} class:inactive-card={providerMode !== 'local'}>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="card-header clickable-header" on:click={() => providerMode = 'local'} role="button" tabindex="0">
                <div class="header-left">
                  <span class="radio-indicator"></span>
                  <span class="card-title">Local Endpoint</span>
                </div>
                {#if providerMode === 'local'}
                  <span class="active-badge">Active</span>
                {/if}
              </div>

              <section>
                <div class="managed-toggle-row">
                  <div class="managed-toggle-text">
                    <span class="managed-toggle-title">Run llama.cpp locally (managed)</span>
                    <p class="hint">Flow downloads and starts llama-server for you.</p>
                  </div>
                  <button
                    class="toggle-switch"
                    class:toggle-active={llamaManagedEnabled}
                    on:click={() => setManagedLlama(!llamaManagedEnabled)}
                    type="button"
                    role="switch"
                    aria-checked={llamaManagedEnabled}
                    aria-label="Run llama.cpp locally (managed)"
                  >
                    <span class="toggle-knob"></span>
                  </button>
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
              {/if}

              <section>
                <label class="row-label" for="apiKey">API key (optional)</label>
                <input id="apiKey" type="password" bind:value={apiKey} placeholder="lm-studio" disabled={llamaManagedEnabled} />
                {#if llamaManagedEnabled}
                  <p class="hint">Not used in managed mode.</p>
                {/if}
              </section>

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
            </div>

            <!-- Cloud Endpoint Configuration Card -->
            <div class="config-card" class:active-card={providerMode === 'cloud'} class:inactive-card={providerMode !== 'cloud'}>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="card-header clickable-header" on:click={() => providerMode = 'cloud'} role="button" tabindex="0">
                <div class="header-left">
                  <span class="radio-indicator"></span>
                  <span class="card-title">Cloud Endpoint</span>
                </div>
                {#if providerMode === 'cloud'}
                  <span class="active-badge">Active</span>
                {/if}
              </div>

              <section>
                <label class="row-label" for="cloudProvider">Cloud Provider</label>
                <select id="cloudProvider" bind:value={cloudProvider}>
                  {#each CLOUD_PROVIDERS as p}
                    <option value={p.id}>{p.label}</option>
                  {/each}
                </select>
              </section>

              {#if cloudProvider === 'openai'}
                <section>
                  <label class="row-label" for="cloudOpenAIKey">OpenAI API Key</label>
                  <input id="cloudOpenAIKey" type="password" bind:value={openaiKey} placeholder="sk-..." />
                </section>
              {:else if cloudProvider === 'anthropic'}
                <section>
                  <label class="row-label" for="cloudAnthropicKey">Anthropic API Key</label>
                  <input id="cloudAnthropicKey" type="password" bind:value={anthropicKey} placeholder="sk-ant-..." />
                </section>
              {:else if cloudProvider === 'openrouter'}
                <section>
                  <label class="row-label" for="cloudOpenRouterKey">OpenRouter API Key</label>
                  <input id="cloudOpenRouterKey" type="password" bind:value={openRouterKey} placeholder="sk-or-..." />
                </section>
              {:else if cloudProvider === 'custom'}
                <section>
                  <label class="row-label" for="cloudCustomURL">Base URL</label>
                  <input id="cloudCustomURL" type="text" bind:value={customCloudURL} placeholder="https://example.com/v1" />
                </section>
                <section>
                  <label class="row-label" for="cloudCustomKey">API Key</label>
                  <input id="cloudCustomKey" type="password" bind:value={customCloudKey} placeholder="optional" />
                </section>
              {/if}

              <section>
                <label class="row-label" for="cloudModel">Model</label>
                <div class="row">
                  <input
                    id="cloudModel"
                    type="text"
                    bind:value={cloudModel}
                    placeholder={(CLOUD_PROVIDERS.find(p => p.id === cloudProvider) || {}).modelPlaceholder || ''}
                  />
                  <button class="secondary" on:click={testCloudConnection} disabled={cloudTestStatus === 'testing'}>
                    {cloudTestStatus === 'testing' ? 'Testing…' : 'Test connection'}
                  </button>
                </div>
                {#if cloudTestStatus === 'ok'}
                  <div class="feedback ok">✓ {cloudTestMessage}</div>
                  {:else if cloudTestStatus === 'err'}
                  <div class="feedback err">✗ {cloudTestMessage}</div>
                {/if}
              </section>
            </div>
          </div>
        {/if}

        <!-- ── Voice / STT Tab ── -->
        {#if activeTab === 'voice'}
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

          <!-- Divider line to separate Transcription from Dictation -->
          <div style="height: 1px; background: var(--border-subtle); margin: 20px 0;"></div>

          <!-- ── Push-to-Talk Dictation Section ── -->
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
                  <kbd class="kbd">Double-tap {getModifierLabel(hotkeyModifier)}</kbd>
                  <span class="shortcut-desc">Fix grammar of selected text</span>
                </div>
              </div>
            {/if}
          </div>

          <!-- Divider line to separate Dictation from Quick Agent HUD -->
          <div style="height: 1px; background: var(--border-subtle); margin: 20px 0;"></div>

          <!-- ── Quick Agent HUD Section ── -->
          <div class="hotkeys-section">
            <h3 class="section-title">HUD &amp; Quick Ask</h3>
            <p class="section-desc">
              Hold a key to speak a request to the agent in a floating window — it reads your current screen, can act across apps, and asks before each step. Tap (don't hold) to open it with context-aware suggestions.
            </p>

            <div class="dictation-toggle-row">
              <span class="dictation-toggle-label">Enable quick-ask hotkey</span>
              <button
                class="toggle-switch"
                class:toggle-active={quickAskHotkeyEnabled}
                on:click={() => { quickAskHotkeyEnabled = !quickAskHotkeyEnabled; if (quickAskHotkeyEnabled) refreshHudPermissions(); }}
                type="button"
                role="switch"
                aria-checked={quickAskHotkeyEnabled}
              >
                <span class="toggle-knob"></span>
              </button>
            </div>

            {#if quickAskHotkeyEnabled}
              <!-- Screen Recording permission status -->
              <div class="perm-row" style="margin-top: 18px;">
                <div class="perm-info">
                  <span class="perm-status-dot" class:granted={screenPermission === true} class:denied={screenPermission === false}></span>
                  <div>
                    <div class="perm-title">Screen Recording</div>
                    <div class="perm-desc">
                      {#if screenPermission === true}
                        Granted — Flow can read your screen for on-screen context.
                      {:else if screenPermission === false}
                        Not granted. The HUD can't see your screen until you allow it.
                      {:else}
                        Required so the HUD can act on what's on your screen.
                      {/if}
                    </div>
                  </div>
                </div>
                {#if screenPermission !== true}
                  <button class="perm-grant-btn" type="button" on:click={grantScreenPermission} disabled={checkingScreenPerm}>
                    {checkingScreenPerm ? 'Opening…' : 'Grant'}
                  </button>
                {:else}
                  <button class="perm-grant-btn secondary" type="button" on:click={refreshScreenPermission}>Re-check</button>
                {/if}
              </div>

              <!-- Accessibility permission status -->
              <div class="perm-row" style="margin-top: 10px;">
                <div class="perm-info">
                  <span class="perm-status-dot" class:granted={axPermission === true} class:denied={axPermission === false}></span>
                  <div>
                    <div class="perm-title">Accessibility</div>
                    <div class="perm-desc">
                      {#if axPermission === true}
                        Granted — Flow can read your selection and act across apps.
                      {:else if axPermission === false}
                        Not granted. Needed to read selected text and drive other apps.
                      {:else}
                        Lets the HUD read your selection and move between apps.
                      {/if}
                    </div>
                  </div>
                </div>
                {#if axPermission !== true}
                  <button class="perm-grant-btn" type="button" on:click={grantAxPermission} disabled={checkingAxPerm}>
                    {checkingAxPerm ? 'Opening…' : 'Grant'}
                  </button>
                {:else}
                  <button class="perm-grant-btn secondary" type="button" on:click={refreshAxPermission}>Re-check</button>
                {/if}
              </div>

              <label class="row-label" for="quickask-modifier" style="margin-top: 18px;">Hotkey (hold to talk)</label>
              <select id="quickask-modifier" bind:value={quickAskHotkeyModifier}>
                {#each Object.entries(modifierLabels) as [value, label]}
                  <option {value}>{label}</option>
                {/each}
              </select>
              {#if quickAskHotkeyModifier === hotkeyModifier && hotkeyEnabled}
                <p class="hint" style="margin-top: 6px; color: var(--warning, #d97706);">
                  This is the same key as Push-to-Talk Dictation — pick a different one to avoid conflicts.
                </p>
              {:else}
                <p class="hint" style="margin-top: 6px;">
                  Hold to speak a request; tap to see suggestions for what's on screen. Needs Microphone, Screen&nbsp;Recording, and Accessibility permissions.
                </p>
              {/if}
            {/if}
          </div>

          <!-- Divider line to separate Quick Agent HUD from Auto-Refine -->
          <div style="height: 1px; background: var(--border-subtle); margin: 20px 0;"></div>

          <!-- ── Auto-Refine Section ── -->
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

  .segmented {
    display: flex;
    gap: 0;
    padding: 3px;
    background: var(--bg-app);
    border: 1px solid var(--border-subtle);
    border-radius: 999px;
    transition: opacity 0.15s ease;
  }
  .segment {
    flex: 1;
    background: transparent;
    color: var(--text-muted);
    border: none;
    padding: 6px 12px;
    border-radius: 999px;
    font-size: 13px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    transition: background 0.12s ease, color 0.12s ease, box-shadow 0.12s ease;
  }
  .segment:hover:not(.segment-active) { color: var(--text-secondary); }
  .segment-active {
    background: var(--bg-elevated);
    color: var(--text-primary);
    font-weight: 600;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25), inset 0 0 0 1px var(--accent);
  }
  .segment:disabled { cursor: default; }

  .managed-toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 12px;
    padding: 10px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
  }
  .managed-toggle-text { min-width: 0; }
  .managed-toggle-title {
    display: block;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .managed-toggle-text .hint { margin-top: 2px; }

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

  /* Allowed commands list styles */
  .allowed-commands-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 10px;
    background: var(--bg-app);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    min-height: 48px;
    box-sizing: border-box;
  }
  .command-tag {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
    color: var(--text-primary);
  }
  .command-tag code {
    font-family: var(--font-mono);
    color: var(--accent);
  }
  .command-remove-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    padding: 0;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    transition: all 0.1s ease;
  }
  .command-remove-btn:hover {
    color: #f87171;
    background: rgba(248, 113, 113, 0.1);
  }

  /* ─── Premium Decoupled LLM Settings Layout ─── */
  .config-sections-wrapper {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-top: 16px;
  }
  
  .config-card {
    background: rgba(255, 255, 255, 0.015);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md, 10px);
    padding: 16px;
    position: relative;
    transition: all 0.22s ease-in-out;
  }
  
  .config-card.active-card {
    background: rgba(45, 212, 191, 0.012);
    border-color: rgba(45, 212, 191, 0.45);
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15), 0 0 14px rgba(45, 212, 191, 0.04);
  }
  
  .config-card.inactive-card {
    opacity: 0.65;
  }
  
  .config-card.inactive-card:hover,
  .config-card.inactive-card:focus-within {
    opacity: 0.95;
    background: rgba(255, 255, 255, 0.03);
    border-color: var(--border);
  }
  
  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 14px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .card-header.clickable-header {
    cursor: pointer;
    user-select: none;
    transition: background 0.15s ease;
    border-radius: 8px 8px 0 0;
    margin: -16px -16px 14px;
    padding: 12px 16px;
  }

  .card-header.clickable-header:hover {
    background: rgba(255, 255, 255, 0.02);
  }

  .config-card.active-card .card-header.clickable-header:hover {
    background: rgba(45, 212, 191, 0.02);
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .radio-indicator {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 1.5px solid var(--text-muted);
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
    flex-shrink: 0;
  }

  .config-card.active-card .radio-indicator {
    border-color: var(--accent);
  }

  .radio-indicator::after {
    content: '';
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    transform: scale(0);
    transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  .config-card.active-card .radio-indicator::after {
    transform: scale(1);
  }
  
  .card-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .config-card.active-card .card-title {
    color: var(--accent);
  }
  
  .active-badge {
    background: rgba(45, 212, 191, 0.12);
    color: var(--accent);
    font-size: 9px;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border: 1px solid rgba(45, 212, 191, 0.2);
    box-shadow: 0 1px 4px rgba(45, 212, 191, 0.05);
  }

  /* ── Screen Recording permission row ── */
  .perm-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 14px;
    border: 1px solid var(--border-subtle);
    border-radius: 10px;
    background: var(--bg-subtle, rgba(255, 255, 255, 0.02));
  }
  .perm-info {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
  }
  .perm-status-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    margin-top: 5px;
    flex: none;
    background: #a1a1aa; /* unknown */
  }
  .perm-status-dot.granted { background: #22c55e; }
  .perm-status-dot.denied  { background: #ef4444; }
  .perm-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary, #f4f4f5);
  }
  .perm-desc {
    font-size: 12px;
    color: var(--text-secondary, #a1a1aa);
    margin-top: 2px;
  }
  .perm-grant-btn {
    flex: none;
    padding: 6px 14px;
    border-radius: 8px;
    border: none;
    background: #2563eb;
    color: #fff;
    font-size: 12.5px;
    font-weight: 500;
    cursor: pointer;
  }
  .perm-grant-btn:hover:not(:disabled) { background: #1d4ed8; }
  .perm-grant-btn:disabled { opacity: 0.5; cursor: default; }
  .perm-grant-btn.secondary {
    background: transparent;
    color: var(--text-secondary, #a1a1aa);
    border: 1px solid var(--border-subtle);
  }
  .perm-grant-btn.secondary:hover { color: var(--text-primary, #f4f4f5); }

  /* ── Update badge & panel ── */
  .update-badge {
    background: rgba(74, 222, 128, 0.14);
    border: 1px solid rgba(74, 222, 128, 0.4);
    color: #4ade80;
    font-size: 10px;
    font-weight: 600;
    padding: 3px 10px;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .update-badge:hover {
    background: rgba(74, 222, 128, 0.22);
  }
  .check-update-btn {
    background: transparent;
    border: 1px solid var(--border-subtle);
    color: var(--text-muted);
    font-size: 10px;
    padding: 3px 10px;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .check-update-btn:hover {
    color: var(--text-primary);
    border-color: rgba(255, 255, 255, 0.18);
  }
  .update-panel {
    margin-top: 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    padding: 12px 14px;
  }
  .update-panel-header {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 8px;
  }
  .update-panel-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary, #f4f4f5);
  }
  .update-notes {
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-secondary, #d4d4d8);
    white-space: pre-wrap;
    max-height: 200px;
    overflow-y: auto;
    margin-bottom: 10px;
  }
  .update-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .update-install-btn {
    background: #4ade80;
    color: #0a0a0b;
    border: none;
    font-size: 12px;
    font-weight: 600;
    padding: 7px 16px;
    border-radius: 7px;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .update-install-btn:hover { background: #36d96e; }
  .update-release-link {
    background: transparent;
    border: 1px solid var(--border-subtle);
    color: var(--text-muted);
    font-size: 11px;
    padding: 6px 12px;
    border-radius: 7px;
    cursor: pointer;
  }
  .update-release-link:hover { color: var(--text-primary); }
  .update-progress {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .update-progress-bar {
    height: 5px;
    background: rgba(255, 255, 255, 0.08);
    border-radius: 3px;
    overflow: hidden;
  }
  .update-progress-fill {
    height: 100%;
    background: #4ade80;
    border-radius: 3px;
    transition: width 0.3s ease;
  }
  .update-progress-text {
    font-size: 11px;
    color: var(--text-muted);
  }
</style>
