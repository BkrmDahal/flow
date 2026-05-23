<script>
  import { onMount, onDestroy } from 'svelte'
  import { Backend, Events } from '../lib/wails.js'
  import FlowRefineMenu from './FlowRefineMenu.svelte'

  export let onOpenSettings = () => {}

  let transcripts = []
  let selectedId = null
  let viewing = null              // FlowTranscript currently displayed
  let liveText = ''               // live partial during recording
  let isRecording = false
  let isTranscribing = false
  let recordStartedAt = 0
  let elapsedSec = 0
  let timerHandle = null
  let errorMsg = ''
  let searchQuery = ''
  let modelDownload = null        // { downloaded, total } while bundled model downloads

  // ── Resizer state ──
  let sidebarWidth = 220;
  let isResizing = false;

  function startResize(e) {
    e.preventDefault();
    isResizing = true;
    window.addEventListener('mousemove', handleResize);
    window.addEventListener('mouseup', stopResize);
  }

  function handleResize(e) {
    if (!isResizing) return;
    sidebarWidth = Math.max(160, Math.min(450, e.clientX));
  }

  function stopResize() {
    isResizing = false;
    window.removeEventListener('mousemove', handleResize);
    window.removeEventListener('mouseup', stopResize);
    localStorage.setItem('flow-recordings-sidebar-width', sidebarWidth.toString());
  }

  // ── Click and Type/Inline Edit variables ──
  let lastViewingId = null
  let viewingText = ''
  let originalViewingText = ''

  $: if (viewing) {
    if (viewing.id !== lastViewingId) {
      viewingText = viewing.text
      originalViewingText = viewing.text
      lastViewingId = viewing.id
    }
  } else {
    lastViewingId = null
    viewingText = ''
    originalViewingText = ''
  }

  async function saveTypedText() {
    if (!liveText.trim()) return
    errorMsg = ''
    try {
      const id = await Backend.SaveFlowTranscript(liveText, 0)
      await refreshList()
      await selectTranscript(id)
      await maybeAutoRefine(id)
    } catch (e) {
      errorMsg = String(e)
    }
  }

  async function saveViewingEdits() {
    if (!viewing || viewingText === originalViewingText) return
    errorMsg = ''
    try {
      await Backend.UpdateFlowTranscript(viewing.id, viewingText)
      originalViewingText = viewingText
      await refreshList()
      viewing = await Backend.LoadFlowTranscript(viewing.id)
    } catch (e) {
      errorMsg = String(e)
    }
  }

  async function handleViewingBlur() {
    if (viewingText !== originalViewingText) {
      await saveViewingEdits()
    }
  }

  function autoResize(node) {
    const adjust = () => {
      node.style.height = 'auto'
      node.style.height = `${node.scrollHeight}px`
    }
    node.addEventListener('input', adjust)
    setTimeout(adjust, 0)
    return {
      update() {
        adjust()
      },
      destroy() {
        node.removeEventListener('input', adjust)
      }
    }
  }


  // ── Hotkey banner state ──
  let hotkeyEnabled = false
  let hotkeyModifier = 'right_option'
  let hotkeyLoaded = false
  let hotkeyBannerDismissed = false

  const modifierLabels = {
    left_option:  '⌥ Left Option',
    right_option: '⌥ Right Option',
    left_cmd:     '⌘ Left Command',
    right_cmd:    '⌘ Right Command',
    left_ctrl:    '⌃ Left Control',
    right_ctrl:   '⌃ Right Control',
  }
  function modifierDisplayName(mod) {
    return modifierLabels[mod] || mod || '⌥ Right Option'
  }

  async function loadHotkeyStatus() {
    try {
      const s = await Backend.GetSettings()
      hotkeyEnabled = s.hotkeyEnabled || false
      hotkeyModifier = s.hotkeyModifier || 'right_option'
      hotkeyLoaded = true
    } catch (_) {
      hotkeyLoaded = true
    }
  }

  function onSettingsSaved() {
    hotkeyBannerDismissed = false
    loadHotkeyStatus()
  }

  $: wordCount = (viewing ? viewingText : liveText).trim().split(/\s+/).filter(Boolean).length
  $: filteredTranscripts = searchQuery.trim()
    ? transcripts.filter(t => (t.title || '').toLowerCase().includes(searchQuery.toLowerCase()))
    : transcripts

  // Group transcripts by date
  $: groupedTranscripts = groupByDate(filteredTranscripts)

  function groupByDate(list) {
    const groups = []
    const now = new Date()
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    const yesterday = new Date(today - 86400000)

    for (const t of list) {
      const d = new Date(t.timestamp)
      const tDate = new Date(d.getFullYear(), d.getMonth(), d.getDate())
      let label
      if (tDate >= today) label = 'Today'
      else if (tDate >= yesterday) label = 'Yesterday'
      else label = d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }).toUpperCase()

      if (groups.length === 0 || groups[groups.length - 1].label !== label) {
        groups.push({ label, items: [] })
      }
      groups[groups.length - 1].items.push(t)
    }
    return groups
  }

  async function refreshList() {
    try {
      transcripts = (await Backend.ListFlowTranscripts?.()) ?? []
    } catch (e) {
      console.error('list transcripts:', e)
    }
  }

  async function selectTranscript(id) {
    selectedId = id
    try {
      viewing = await Backend.LoadFlowTranscript(id)
      liveText = ''
    } catch (e) {
      errorMsg = String(e)
    }
  }

  async function startRecording() {
    errorMsg = ''
    liveText = ''
    viewing = null
    selectedId = null
    isRecording = true
    recordStartedAt = Date.now()
    elapsedSec = 0
    timerHandle = setInterval(() => {
      elapsedSec = Math.floor((Date.now() - recordStartedAt) / 1000)
    }, 250)
    try {
      await Backend.StartFlow('en-US')
    } catch (e) {
      errorMsg = String(e)
      stopTimer()
      isRecording = false
    }
  }

  async function stopRecording() {
    if (!isRecording) return
    isRecording = false
    isTranscribing = true
    stopTimer()
    let text = ''
    try {
      text = await Backend.StopFlow()
    } catch (e) {
      errorMsg = String(e)
      isTranscribing = false
      return
    } finally {
      modelDownload = null
    }
    isTranscribing = false
    liveText = text || ''
    if (liveText.trim()) {
      try {
        const id = await Backend.SaveFlowTranscript(liveText, elapsedSec)
        await refreshList()
        await selectTranscript(id)
        await maybeAutoRefine(id)
      } catch (e) {
        errorMsg = String(e)
      }
    }
  }

  async function maybeAutoRefine(id) {
    try {
      const s = await Backend.GetSettings()
      const action = s?.autoRefineAction
      if (!action || action === 'off' || !s?.model) return
      await Backend.RefineFlowText(id, action, s?.autoRefineCustomPrompt || '')
      await selectTranscript(id)
    } catch (e) {
      console.warn('auto-refine failed:', e)
    }
  }

  function stopTimer() {
    if (timerHandle) {
      clearInterval(timerHandle)
      timerHandle = null
    }
  }

  async function deleteTranscript(id, evt) {
    evt?.stopPropagation()
    if (!confirm('Delete this transcript?')) return
    try {
      await Backend.DeleteFlowTranscript(id)
      if (selectedId === id) {
        selectedId = null
        viewing = null
      }
      await refreshList()
    } catch (e) {
      errorMsg = String(e)
    }
  }

  function copyText(text) {
    navigator.clipboard?.writeText(text)
  }

  function clearText() {
    liveText = ''
    viewing = null
    selectedId = null
  }

  function formatTime(secs) {
    const m = Math.floor(secs / 60)
    const s = secs % 60
    return `${m}:${String(s).padStart(2, '0')}`
  }

  function formatDuration(secs) {
    if (secs < 60) return `${secs}s`
    const m = Math.floor(secs / 60)
    const s = secs % 60
    return s > 0 ? `${m}m ${s}s` : `${m}m`
  }

  function formatTimestamp(ms) {
    const d = new Date(ms)
    return d.toLocaleString()
  }

  function titleOf(t) {
    const s = (t?.text ?? '').trim()
    if (!s) return 'Empty recording'
    return s.length > 60 ? s.slice(0, 60) + '…' : s
  }



  onMount(() => {
    const saved = localStorage.getItem('flow-recordings-sidebar-width');
    if (saved) {
      sidebarWidth = Math.max(160, Math.min(450, parseInt(saved, 10)));
    }
    refreshList()
    loadHotkeyStatus()
    window.addEventListener('flow:settings-saved', onSettingsSaved)
    Events.on('flow:result', (payload) => {
      liveText = payload.text ?? liveText
    })
    Events.on('flow:error', (payload) => {
      errorMsg = payload.error ?? 'Unknown speech error'
      isRecording = false
      stopTimer()
    })
    Events.on('flow:hotkey:toggle', () => {
      if (isRecording) {
        stopRecording()
      } else {
        startRecording()
      }
    })
    Events.on('flow:model:download:progress', (payload) => {
      modelDownload = {
        downloaded: payload?.downloaded ?? 0,
        total: payload?.total ?? 0,
      }
    })
  })

  onDestroy(() => {
    Events.off('flow:result')
    Events.off('flow:error')
    Events.off('flow:hotkey:toggle')
    Events.off('flow:model:download:progress')
    window.removeEventListener('flow:settings-saved', onSettingsSaved)
    stopTimer()
  })
</script>

<div class="flow" style="grid-template-columns: {sidebarWidth}px 1fr;">
  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="resize-handle" class:active={isResizing} on:mousedown={startResize} role="separator" aria-label="Resize Sidebar"></div>
    <div class="drag-region"></div>
    <div class="sidebar-inner">
      <button class="nav-new" on:click={startRecording} disabled={isRecording}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 1a3 3 0 00-3 3v8a3 3 0 006 0V4a3 3 0 00-3-3z"/><path d="M19 10v2a7 7 0 01-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/></svg>
        <span>New recording</span>
      </button>

      <div class="search-wrapper">
        <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
        <input class="search-input" type="text" bind:value={searchQuery} placeholder="Search recordings" />
      </div>

      <div class="divider"></div>

      <div class="sidebar-list">
        {#each groupedTranscripts as group}
          <div class="date-group">
            <div class="date-label">{group.label}</div>
            {#each group.items as t (t.id)}
              <button
                class="transcript-item"
                class:selected={selectedId === t.id}
                on:click={() => selectTranscript(t.id)}
              >
                <div class="transcript-title">{t.title}</div>
                <div class="transcript-meta">
                  {t.wordCount} words · {formatDuration(t.duration)}
                </div>
                <button class="transcript-delete" on:click={(e) => deleteTranscript(t.id, e)} title="Delete recording">×</button>
              </button>
            {/each}
          </div>
        {/each}
        {#if filteredTranscripts.length === 0}
          <div class="sidebar-empty">{searchQuery ? 'No matching recordings' : 'Your recordings will show up here'}</div>
        {/if}
      </div>
    </div>

    <div class="sidebar-bottom">
      <button class="sidebar-icon-btn" on:click={onOpenSettings} title="Settings">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06A1.65 1.65 0 0019.32 9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z"/>
        </svg>
      </button>
    </div>
  </aside>

  <!-- Main content -->
  <main class="content">
    <!-- ─── Hotkey Banner ─── -->
    {#if hotkeyLoaded && !hotkeyBannerDismissed}
      {#if !hotkeyEnabled}
        <div class="hotkey-banner hotkey-banner-enable">
          <div class="hotkey-banner-icon">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
              <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
              <line x1="12" y1="19" x2="12" y2="23"/>
              <line x1="8" y1="23" x2="16" y2="23"/>
            </svg>
          </div>
          <div class="hotkey-banner-body">
            <span class="hotkey-banner-title">Voice-to-text anywhere on your Mac</span>
            <span class="hotkey-banner-desc">Enable the global hotkey to dictate into any app — emails, docs, Slack, and more.</span>
          </div>
          <button class="hotkey-banner-btn" on:click={onOpenSettings} type="button">Enable in Settings</button>
          <button class="hotkey-banner-dismiss" on:click={() => hotkeyBannerDismissed = true} title="Dismiss">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
              <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            </svg>
          </button>
        </div>
      {:else}
        <div class="hotkey-banner hotkey-banner-active">
          <div class="hotkey-banner-icon hotkey-banner-icon-active">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
              <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
              <line x1="12" y1="19" x2="12" y2="23"/>
              <line x1="8" y1="23" x2="16" y2="23"/>
            </svg>
          </div>
          <div class="hotkey-banner-body">
            <span class="hotkey-banner-title">Voice dictation is active</span>
            <span class="hotkey-banner-desc">
              Hold <kbd class="hotkey-kbd">{modifierDisplayName(hotkeyModifier)}</kbd> and speak — text will be typed into whatever app you're using.
            </span>
            <span class="hotkey-banner-desc">
              Double-tap <kbd class="hotkey-kbd">{modifierDisplayName(hotkeyModifier)}</kbd> to fix grammar of selected text.
            </span>
          </div>
          <button class="hotkey-banner-dismiss" on:click={() => hotkeyBannerDismissed = true} title="Dismiss">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
              <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            </svg>
          </button>
        </div>
      {/if}
    {/if}

    <div class="content-inner">
    {#if errorMsg}
      <div class="error-banner">
        <span>{errorMsg}</span>
        <button on:click={() => (errorMsg = '')} title="Dismiss error">×</button>
      </div>
    {/if}

    {#if modelDownload && modelDownload.total > 0}
      <div class="download-banner">
        <span>
          Downloading whisper model… {Math.round((modelDownload.downloaded / modelDownload.total) * 100)}%
          ({Math.round(modelDownload.downloaded / 1024 / 1024)} / {Math.round(modelDownload.total / 1024 / 1024)} MB)
        </span>
        <progress max={modelDownload.total} value={modelDownload.downloaded}></progress>
      </div>
    {:else if isTranscribing}
      <div class="download-banner">
        <span>Transcribing…</span>
      </div>
    {/if}

    <!-- Stats row -->
    <div class="stats-row">
      <div class="stat-card">
        <div class="stat-icon clock">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>
        </div>
        <div class="stat-value">{formatTime(isRecording ? elapsedSec : (viewing?.duration ?? 0))}</div>
        <div class="stat-label">Session Time</div>
      </div>
      <div class="stat-card">
        <div class="stat-icon words">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.12 2.12 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
        </div>
        <div class="stat-value">{wordCount}</div>
        <div class="stat-label">Words</div>
      </div>
    </div>

    <!-- Transcript area -->
    <div class="transcript-area">
      {#if isRecording}
        <div class="transcript-text">
          {#if liveText}
            {liveText}
          {:else}
            <span class="listening">Listening…</span>
          {/if}
        </div>
      {:else if viewing}
        <textarea
          class="transcript-textarea-view"
          bind:value={viewingText}
          use:autoResize
          on:blur={handleViewingBlur}
          placeholder="Start typing..."
        ></textarea>
        {#if viewing.refinements?.length}
          <div class="refinements">
            {#each viewing.refinements as r}
              <div class="refinement">
                <div class="refinement-head">
                  <span class="badge">{r.action}</span>
                  <span class="muted">{r.model}</span>
                  <button class="copy-sm" on:click={() => copyText(r.text)}>Copy</button>
                </div>
                <div class="refinement-text">{r.text}</div>
              </div>
            {/each}
          </div>
        {/if}
        <FlowRefineMenu transcript={viewing} onRefined={() => selectTranscript(viewing.id)} />
      {:else}
        <textarea
          class="transcript-textarea"
          bind:value={liveText}
          placeholder="Click the microphone to start speaking, or start typing here..."
        ></textarea>
      {/if}
    </div>

    <!-- Bottom actions -->
    <footer class="bottom-bar">
      <div class="bottom-actions">
        <button class="action-btn" on:click={clearText} disabled={viewing ? !viewingText : !liveText} title="Clear transcript">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
          Clear
        </button>
        <button class="action-btn" on:click={() => copyText(viewing ? viewingText : liveText)} disabled={viewing ? !viewingText.trim() : !liveText.trim()} title="Copy to clipboard">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
          Copy
        </button>
        {#if isRecording}
          <button class="action-btn" on:click={stopRecording} title="Stop recording and save">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/></svg>
            Save
          </button>
        {:else if viewing}
          <button class="action-btn" on:click={saveViewingEdits} disabled={viewingText === originalViewingText} title="Save changes">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/></svg>
            Save
          </button>
        {:else}
          <button class="action-btn" on:click={saveTypedText} disabled={!liveText.trim()} title="Save typed transcript">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/></svg>
            Save
          </button>
        {/if}
      </div>


      <!-- Big microphone button -->
      <button
        class="mic-btn"
        class:recording={isRecording}
        on:click={() => isRecording ? stopRecording() : startRecording()}
        title={isRecording ? "Stop recording" : "Start recording"}
      >
        {#if isRecording}
          <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>
        {:else}
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 1a3 3 0 00-3 3v8a3 3 0 006 0V4a3 3 0 00-3-3z"/><path d="M19 10v2a7 7 0 01-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
        {/if}
      </button>

      <div class="status-text">
        {#if isRecording}
          <span class="status-recording">
            <span class="rec-dot"></span>
            Recording · {formatTime(elapsedSec)}
          </span>
        {:else}
          Ready
        {/if}
      </div>
    </footer>
    </div>
  </main>
</div>

<style>
  /* ─── Hotkey Banner ─── */
  .hotkey-banner {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    flex-shrink: 0;
    animation: bannerSlideIn 0.3s ease;
  }
  @keyframes bannerSlideIn {
    from { opacity: 0; transform: translateY(-6px); }
    to   { opacity: 1; transform: translateY(0); }
  }
  .hotkey-banner-enable {
    background: rgba(45, 212, 191, 0.06);
    border-bottom: 1px solid rgba(45, 212, 191, 0.18);
  }
  .hotkey-banner-active {
    background: rgba(34, 197, 94, 0.06);
    border-bottom: 1px solid rgba(34, 197, 94, 0.18);
  }
  .hotkey-banner-icon {
    width: 34px; height: 34px;
    display: flex; align-items: center; justify-content: center;
    background: rgba(45, 212, 191, 0.12);
    border-radius: 8px;
    color: var(--accent);
    flex-shrink: 0;
  }
  .hotkey-banner-icon-active {
    background: rgba(34, 197, 94, 0.12);
    color: #4ade80;
  }
  .hotkey-banner-body {
    flex: 1;
    display: flex; flex-direction: column; gap: 1px;
    min-width: 0;
  }
  .hotkey-banner-title {
    font-size: 12px; font-weight: 600;
    color: var(--text-primary); line-height: 1.3;
  }
  .hotkey-banner-desc {
    font-size: 11px; color: var(--text-secondary); line-height: 1.4;
  }
  .hotkey-kbd {
    display: inline-block;
    padding: 1px 5px;
    background: rgba(255,255,255,0.08);
    border: 1px solid rgba(255,255,255,0.12);
    border-radius: 4px;
    font-family: inherit; font-size: 10px; font-weight: 600;
    color: var(--text-primary); letter-spacing: 0.2px;
  }
  .hotkey-banner-btn {
    padding: 5px 12px;
    background: var(--accent); border: none; border-radius: 6px;
    color: #000; font-size: 11px; font-weight: 600; font-family: inherit;
    cursor: pointer; white-space: nowrap; flex-shrink: 0;
    transition: all 0.15s ease;
  }
  .hotkey-banner-btn:hover { background: var(--accent-dim); transform: scale(1.02); }
  .hotkey-banner-dismiss {
    display: flex; align-items: center; justify-content: center;
    width: 22px; height: 22px;
    background: transparent; border: none; border-radius: 5px;
    color: var(--text-muted); cursor: pointer; flex-shrink: 0;
    opacity: 0.5; transition: all 0.15s ease;
  }
  .hotkey-banner-dismiss:hover { background: rgba(255,255,255,0.06); color: var(--text-primary); opacity: 1; }

  .flow {
    display: grid;
    grid-template-columns: 220px 1fr;
    height: 100%;
    gap: 0;
  }

  /* ─── Sidebar ─── */
  .sidebar {
    position: relative;
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--border);
    background: var(--bg-sidebar);
    overflow: hidden;
    user-select: none;
  }

  .resize-handle {
    position: absolute;
    top: 0; right: 0; bottom: 0;
    width: 4px;
    cursor: col-resize;
    z-index: 100;
    transition: background 0.15s ease;
  }
  .resize-handle:hover,
  .resize-handle.active {
    background: var(--accent);
  }
  .drag-region {
    --wails-draggable: drag;
    height: 12px;
    flex-shrink: 0;
  }
  .sidebar-inner {
    display: flex;
    flex-direction: column;
    flex: 1;
    padding: 0 10px 12px;
    overflow-y: auto;
    overflow-x: hidden;
    min-height: 0;
  }
  .sidebar-inner::-webkit-scrollbar { width: 4px; }
  .sidebar-inner::-webkit-scrollbar-track { background: transparent; }
  .sidebar-inner::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

  .nav-new {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    background: transparent;
    border: none;
    color: var(--text-primary);
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 13.5px;
    font-weight: 500;
    font-family: var(--font-sans);
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
    margin-bottom: 2px;
  }
  .nav-new:hover { background: var(--bg-hover); }
  .nav-new:disabled { opacity: 0.5; cursor: not-allowed; }
  .nav-new svg { color: var(--accent); flex-shrink: 0; }

  .search-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 12px;
    border-radius: 8px;
    transition: all 0.15s ease;
  }
  .search-wrapper:focus-within { background: var(--bg-hover); }
  .search-icon { color: var(--text-muted); flex-shrink: 0; }
  .search-input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-secondary);
    font-size: 13.5px;
    font-family: var(--font-sans);
    padding: 4px 0;
  }
  .search-input::placeholder { color: var(--text-muted); }

  .divider {
    height: 1px;
    background: var(--border);
    margin: 4px 12px;
    flex-shrink: 0;
  }

  .sidebar-list {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 0;
  }
  .date-group { margin-bottom: 2px; }
  .date-label {
    padding: 6px 12px 2px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .transcript-item {
    position: relative;
    display: flex;
    flex-direction: column;
    width: 100%;
    text-align: left;
    background: transparent;
    border: none;
    color: var(--text-secondary);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
    font-family: var(--font-sans);
  }
  .transcript-item:hover { background: var(--bg-hover); color: var(--text-primary); }
  .transcript-item.selected { background: rgba(255,255,255,0.08); color: var(--text-primary); }
  .transcript-title {
    font-size: 13px;
    overflow: hidden;
    white-space: nowrap;
    padding-right: 6px;
    font-weight: 400;
    min-width: 0;
    transition: padding-right 0.15s ease;
    -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 24px), transparent 100%);
    mask-image: linear-gradient(to right, #000 calc(100% - 24px), transparent 100%);
  }
  .transcript-item:hover .transcript-title {
    padding-right: 22px;
  }
  .transcript-item.selected .transcript-title,
  .transcript-item:hover .transcript-title { font-weight: 500; }
  .transcript-meta {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
  }
  .transcript-delete {
    position: absolute;
    top: 5px; right: 6px;
    background: transparent;
    color: var(--text-muted);
    border: none;
    font-size: 15px;
    line-height: 1;
    padding: 2px 5px;
    border-radius: 4px;
    opacity: 0;
    transition: all 0.15s ease;
  }
  .transcript-item:hover .transcript-delete { opacity: 1; }
  .transcript-delete:hover { color: var(--danger); background: var(--danger-bg); }

  .sidebar-empty {
    padding: 12px;
    color: var(--text-muted);
    font-size: 13px;
    line-height: 1.4;
  }

  .sidebar-bottom {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    padding: 8px 14px;
    border-top: 1px solid var(--border);
    flex-shrink: 0;
  }
  .sidebar-icon-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    width: 32px;
    height: 32px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .sidebar-icon-btn:hover { color: var(--text-secondary); background: var(--bg-hover); }

  /* ─── Main Content ─── */
  .content {
    display: flex;
    flex-direction: column;
    padding: 0;
    overflow: hidden;
  }

  .content-inner {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    padding: 20px 28px;
    overflow: hidden;
  }

  .error-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--danger-bg);
    border: 1px solid rgba(239, 68, 68, 0.2);
    border-radius: var(--radius-md);
    padding: 10px 14px;
    margin-bottom: 16px;
    color: #fca5a5;
    font-size: 13px;
  }
  .error-banner button {
    background: transparent;
    border: none;
    color: #fca5a5;
    font-size: 16px;
    padding: 0 4px;
  }

  .download-banner {
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 10px 14px;
    margin-bottom: 16px;
    color: var(--text-secondary);
    font-size: 13px;
  }
  .download-banner progress {
    width: 100%;
    height: 6px;
  }

  /* ─── Stats ─── */
  .stats-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-bottom: 16px;
    flex-shrink: 0;
  }
  .stat-card {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 14px 16px;
  }
  .stat-icon {
    width: 34px;
    height: 34px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .stat-icon.clock {
    background: var(--accent-bg);
    color: var(--accent);
  }
  .stat-icon.words {
    background: var(--accent-bg);
    color: var(--accent);
  }
  .stat-value {
    font-size: 22px;
    font-weight: 600;
    color: var(--text-primary);
    line-height: 1;
  }
  .stat-label {
    font-size: 12px;
    color: var(--text-muted);
  }

  /* ─── Transcript area ─── */
  .transcript-area {
    flex: 1;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 20px 22px;
    overflow-y: auto;
    min-height: 0;
    margin-bottom: 16px;
  }
  .transcript-text {
    font-size: 15px;
    line-height: 1.7;
    color: var(--text-primary);
    white-space: pre-wrap;
  }
  .listening {
    color: var(--accent);
    animation: pulse-text 1.5s ease-in-out infinite;
  }
  @keyframes pulse-text {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
  .transcript-textarea, .transcript-textarea-view {
    width: 100%;
    background: transparent;
    border: none;
    outline: none;
    resize: none;
    font-family: inherit;
    font-size: 15px;
    line-height: 1.7;
    color: var(--text-primary);
    padding: 0;
    margin: 0;
    overflow-y: hidden;
  }
  .transcript-textarea {
    height: 100%;
    min-height: 200px;
    overflow-y: auto;
  }
  .transcript-textarea::placeholder, .transcript-textarea-view::placeholder {
    color: var(--text-muted);
  }


  .refinements {
    margin-top: 20px;
    border-top: 1px solid var(--border-subtle);
    padding-top: 16px;
  }
  .refinement { margin-bottom: 14px; }
  .refinement-head {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 6px;
  }
  .badge {
    background: var(--accent-bg);
    color: var(--accent);
    border-radius: var(--radius-sm);
    padding: 2px 8px;
    font-size: 11px;
    font-weight: 500;
    text-transform: capitalize;
  }
  .muted { color: var(--text-muted); font-size: 11px; flex: 1; }
  .copy-sm {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    border-radius: var(--radius-sm);
    padding: 2px 8px;
    font-size: 11px;
    transition: all 0.12s;
  }
  .copy-sm:hover { color: var(--text-primary); background: var(--bg-hover); }
  .refinement-text {
    white-space: pre-wrap;
    line-height: 1.6;
    color: var(--text-primary);
    font-size: 14px;
  }

  /* ─── Bottom bar ─── */
  .bottom-bar {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-shrink: 0;
  }
  .bottom-actions {
    display: flex;
    gap: 4px;
  }
  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid var(--border-subtle);
    color: var(--text-secondary);
    padding: 7px 14px;
    border-radius: var(--radius-full);
    font-size: 12px;
    font-weight: 500;
    transition: all 0.12s;
  }
  .action-btn:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--border);
    background: var(--bg-hover);
  }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }

  /* Mic button */
  .mic-btn {
    width: 52px;
    height: 52px;
    border-radius: 50%;
    background: var(--accent);
    border: none;
    color: #1c1917;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 0 auto;
    box-shadow: 0 4px 16px rgba(45, 212, 191, 0.25);
    transition: all 0.2s ease;
  }
  .mic-btn:hover {
    transform: scale(1.06);
    box-shadow: 0 6px 24px rgba(45, 212, 191, 0.35);
  }
  .mic-btn.recording {
    background: var(--danger);
    box-shadow: 0 4px 16px rgba(239, 68, 68, 0.3);
    animation: pulse-mic 1.5s ease-in-out infinite;
  }
  @keyframes pulse-mic {
    0%, 100% { box-shadow: 0 4px 16px rgba(239, 68, 68, 0.3); }
    50% { box-shadow: 0 4px 28px rgba(239, 68, 68, 0.5); }
  }

  .status-text {
    font-size: 13px;
    color: var(--text-muted);
    min-width: 100px;
    text-align: right;
  }
  .status-recording {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--danger);
    font-weight: 500;
  }
  .rec-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--danger);
    animation: blink 1s ease-in-out infinite;
  }
  @keyframes blink {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }
</style>
