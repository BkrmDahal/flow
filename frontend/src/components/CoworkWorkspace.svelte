<script>
  import { onMount, tick } from 'svelte';
  import AgentSidebar from './AgentSidebar.svelte';
  import AgentWelcome from './AgentWelcome.svelte';
  import AgentWorkspace from './AgentWorkspace.svelte';
  import { Backend } from '../lib/wails.js';

  import {
    coworkPhase, coworkTaskHistory, activeCoworkTaskId,
    coworkTaskTitle, coworkMessages, coworkCreatedFiles,
    coworkContextTools, coworkProgressSteps, coworkSkillsUsed,
    coworkLoading, coworkIsStreaming, coworkParseDocuments,
    refreshCoworkHistory, startCoworkTask, sendCoworkFollowUp,
    cancelCowork, newCoworkTask, selectCoworkTask, deleteCoworkTask,
  } from '../lib/stores/coworkStore.js';

  export let onOpenSettings = () => {};

  let agentInput = '';
  let agentTextareaEl;
  let agentFileInputEl;
  let agentFiles = [];

  const ACCEPTED_TYPES = ['image/png','image/jpeg','image/gif','image/webp','application/pdf','text/plain','text/csv','text/markdown','text/html','application/json'].join(',');
  const IMAGE_TYPES = new Set(['image/png','image/jpeg','image/gif','image/webp']);
  const MAX_FILE_SIZE = 20 * 1024 * 1024;

  onMount(() => { refreshCoworkHistory(); });

  // ─── Welcome input ───
  function handleWelcomeSend() {
    const text = agentInput.trim();
    if ((!text && agentFiles.length === 0) || $coworkLoading) return;
    startCoworkTask(text, [...agentFiles]);
    agentInput = '';
    agentFiles = [];
    if (agentTextareaEl) agentTextareaEl.style.height = 'auto';
  }

  function handleWelcomeKeydown(e) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleWelcomeSend(); }
  }

  function handleWelcomeInput(e) {
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 200) + 'px';
  }

  function openWelcomeFilePicker() {
    agentFileInputEl?.click();
  }

  async function handleWelcomeFileSelect(e) {
    const selected = Array.from(e.target.files || []);
    for (const file of selected) {
      if (file.size > MAX_FILE_SIZE) {
        alert(`File "${file.name}" exceeds the 20 MB limit.`);
        continue;
      }
      try {
        const result = await readFileAsBase64(file);
        agentFiles = [...agentFiles, {
          name: file.name,
          type: file.type || inferMimeType(file.name),
          size: file.size,
          dataUrl: result.dataUrl,
          data: result.base64,
        }];
      } catch (err) {
        alert(`Could not attach "${file.name}": ${err?.message || err}`);
      }
    }
    e.target.value = '';
    agentTextareaEl?.focus();
  }

  function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(reader.error || new Error('Failed to read file'));
      reader.onload = () => {
        const dataUrl = reader.result;
        const base64 = dataUrl.split(',')[1] || '';
        resolve({ dataUrl, base64 });
      };
      reader.readAsDataURL(file);
    });
  }

  function inferMimeType(name) {
    const ext = (name || '').split('.').pop()?.toLowerCase();
    if (ext === 'pdf') return 'application/pdf';
    if (ext === 'csv') return 'text/csv';
    if (ext === 'md' || ext === 'markdown') return 'text/markdown';
    if (ext === 'html' || ext === 'htm') return 'text/html';
    if (ext === 'json') return 'application/json';
    return 'text/plain';
  }

  function removeWelcomeFile(index) {
    agentFiles = agentFiles.filter((_, i) => i !== index);
    agentTextareaEl?.focus();
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  function isImage(mime) {
    return IMAGE_TYPES.has(mime);
  }

  // ─── Workspace events ───
  function handleFollowUp(e) { sendCoworkFollowUp(e.detail.text, e.detail.files); }
  function handleCancel() { cancelCowork(); }

  function handleNewTask() {
    newCoworkTask();
    tick().then(() => agentTextareaEl?.focus());
  }

  function handleOpenFile(e) { Backend.OpenFileInApp?.(e.detail.path); }
  function handleRevealFile(e) { Backend.RevealInFinder?.(e.detail.path); }
  function handleOpenFolder() {
    if ($activeCoworkTaskId) Backend.OpenFileInApp?.(`~/.flow/cowork/${$activeCoworkTaskId}`);
  }
  function handleOpenTaskFolder(e) {
    if (e.detail.id) Backend.OpenFileInApp?.(`~/.flow/cowork/${e.detail.id}`);
  }
</script>

<div class="cowork-layout">
  <AgentSidebar
    agentTaskHistory={$coworkTaskHistory}
    activeAgentTaskId={$activeCoworkTaskId}
    bgStreamingAgents={new Set()}
    on:newTask={handleNewTask}
    on:selectTask={(e) => selectCoworkTask(e.detail.id)}
    on:deleteTask={(e) => deleteCoworkTask(e.detail.id)}
    on:openFolder={handleOpenTaskFolder}
    on:openSettings={() => onOpenSettings()}
  />

  <div class="cowork-main">
    {#if $coworkPhase === 'welcome'}
      <div class="agent-scroll">
        <div class="agent-welcome-wrap">
          <AgentWelcome />
        </div>
      </div>

      <footer class="agent-input-area">
        <div class="agent-input-container">
          {#if agentFiles.length > 0}
            <div class="file-preview-row">
              {#each agentFiles as file, i}
                <div class="file-chip" title={file.name}>
                  {#if isImage(file.type)}
                    <img class="file-thumb" src={file.dataUrl} alt={file.name} />
                  {:else}
                    <div class="file-icon">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none">
                        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                        <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                      </svg>
                    </div>
                  {/if}
                  <span class="file-name">{file.name}</span>
                  <span class="file-size">{formatSize(file.size)}</span>
                  <button class="file-remove" on:click={() => removeWelcomeFile(i)} title="Remove file">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none">
                      <path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                    </svg>
                  </button>
                </div>
              {/each}
            </div>
          {/if}

          <textarea
            bind:this={agentTextareaEl}
            bind:value={agentInput}
            on:keydown={handleWelcomeKeydown}
            on:input={handleWelcomeInput}
            placeholder="Describe a task for the agent…"
            rows="1"
            disabled={$coworkLoading}
          ></textarea>
          <div class="input-bottom">
            <div class="input-bottom-left">
              <button class="btn-attach" on:click|stopPropagation={openWelcomeFilePicker} disabled={$coworkLoading} title="Attach file">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M12 5v14M5 12h14" />
                </svg>
              </button>
              <button
                class="btn-toggle-parse"
                class:active={$coworkParseDocuments}
                on:click={() => $coworkParseDocuments = !$coworkParseDocuments}
                title={$coworkParseDocuments ? "Document parsing enabled" : "Document parsing disabled"}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z"/>
                  <path d="M14 2v6h6"/>
                  {#if !$coworkParseDocuments}
                    <line x1="4" y1="4" x2="20" y2="20" stroke="currentColor" stroke-width="2" />
                  {/if}
                </svg>
              </button>
            </div>
            <button class="btn-send" on:click={handleWelcomeSend}
              disabled={(!agentInput.trim() && agentFiles.length === 0) || $coworkLoading}
              title="Send message">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                <path d="M7 11l5-5m0 0l5 5m-5-5v12" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
          </div>
        </div>

        <input
          bind:this={agentFileInputEl}
          type="file"
          multiple
          accept={ACCEPTED_TYPES}
          on:change={handleWelcomeFileSelect}
          class="hidden-file-input"
        />
      </footer>

    {:else}
      <AgentWorkspace
        taskTitle={$coworkTaskTitle}
        messages={$coworkMessages}
        isStreaming={$coworkIsStreaming}
        loading={$coworkLoading}
        progressSteps={$coworkProgressSteps}
        createdFiles={$coworkCreatedFiles}
        contextTools={$coworkContextTools}
        skillsUsed={$coworkSkillsUsed}
        bind:parseDocuments={$coworkParseDocuments}
        on:openFile={handleOpenFile}
        on:openFolder={handleOpenFolder}
        on:revealFile={handleRevealFile}
        on:sendFollowUp={handleFollowUp}
        on:cancel={handleCancel}
      />
    {/if}
  </div>
</div>

<style>
  .cowork-layout {
    display: flex;
    height: 100%;
    width: 100%;
    overflow: hidden;
  }

  .cowork-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow: hidden;
  }

  .agent-scroll {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    background: var(--bg-primary);
  }

  .agent-welcome-wrap {
    max-width: 720px;
    margin: 0 auto;
    padding: 24px;
  }

  .agent-input-area {
    padding: 0 24px 20px;
    flex-shrink: 0;
  }

  .agent-input-container {
    max-width: 680px;
    margin: 0 auto;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 16px;
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
  }

  .agent-input-container:focus-within {
    border-color: var(--border-light);
    box-shadow: 0 0 0 3px rgba(255,255,255,0.03);
  }

  .agent-input-container textarea {
    --wails-draggable: no-drag;
    display: block;
    width: 100%;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-primary);
    font-family: var(--font-sans);
    font-size: 15px;
    line-height: 1.5;
    resize: none;
    min-height: 24px;
    max-height: 200px;
    padding: 14px 16px 6px;
  }

  .agent-input-container textarea::placeholder { color: var(--text-muted); }
  .agent-input-container textarea:disabled { opacity: 0.5; cursor: not-allowed; }

  .input-bottom {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 8px 8px;
  }

  .btn-send {
    width: 32px; height: 32px;
    display: flex; align-items: center; justify-content: center;
    background: var(--accent);
    border: none; border-radius: 50%;
    color: #000; cursor: pointer;
    transition: all 0.15s ease;
  }
  .btn-send:hover:not(:disabled) { background: var(--accent-hover); transform: scale(1.05); }
  .btn-send:disabled { opacity: 0.3; cursor: not-allowed; }

  /* ─── File Attachment Styles ─── */
  .input-bottom-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn-toggle-parse {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .btn-toggle-parse:hover {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }
  .btn-toggle-parse.active {
    color: var(--accent);
  }

  .btn-attach {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .btn-attach:hover:not(:disabled) {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }

  .btn-attach:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .file-preview-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 12px 14px 0;
  }

  .file-chip {
    display: flex;
    align-items: center;
    gap: 6px;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 4px 8px;
    max-width: 220px;
    animation: chipFadeIn 0.15s ease;
  }

  @keyframes chipFadeIn {
    from { opacity: 0; transform: scale(0.95); }
    to   { opacity: 1; transform: scale(1); }
  }

  .file-thumb {
    width: 28px;
    height: 28px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }

  .file-icon {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.04);
    border-radius: 4px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .file-name {
    font-size: 12px;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100px;
  }

  .file-size {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .file-remove {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    background: transparent;
    border: none;
    border-radius: 50%;
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.1s ease;
  }

  .file-remove:hover {
    color: #f87171;
    background: rgba(248, 113, 113, 0.1);
  }

  .hidden-file-input {
    position: absolute;
    width: 0;
    height: 0;
    opacity: 0;
    pointer-events: none;
  }
</style>
