<script>
  import { onMount, tick } from 'svelte';
  import AgentSidebar from './AgentSidebar.svelte';
  import AgentWelcome from './AgentWelcome.svelte';
  import AgentWorkspace from './AgentWorkspace.svelte';
  import { Backend } from '../lib/wails.js';

  import {
    coworkPhase, coworkTaskHistory, activeCoworkTaskId,
    coworkTaskTitle, coworkMessages, coworkCreatedFiles,
    coworkContextTools, coworkLoading, coworkIsStreaming,
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
    if (!text || $coworkLoading) return;
    startCoworkTask(text);
    agentInput = '';
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

  // ─── Workspace events ───
  function handleFollowUp(e) { sendCoworkFollowUp(e.detail.text); }
  function handleCancel() { cancelCowork(); }

  function handleNewTask() {
    newCoworkTask();
    tick().then(() => agentTextareaEl?.focus());
  }

  function handleOpenFile(e) { Backend.OpenFileInApp?.(e.detail.path); }
  function handleRevealFile(e) { Backend.RevealInFinder?.(e.detail.path); }
  function handleOpenFolder() {
    if ($activeCoworkTaskId) Backend.OpenFileInApp?.(`~/.flow/agents/${$activeCoworkTaskId}`);
  }
  function handleOpenTaskFolder(e) {
    if (e.detail.id) Backend.OpenFileInApp?.(`~/.flow/agents/${e.detail.id}`);
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
            <div></div>
            <button class="btn-send" on:click={handleWelcomeSend}
              disabled={!agentInput.trim() || $coworkLoading}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                <path d="M7 11l5-5m0 0l5 5m-5-5v12" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
          </div>
        </div>
      </footer>

    {:else}
      <AgentWorkspace
        taskTitle={$coworkTaskTitle}
        messages={$coworkMessages}
        isStreaming={$coworkIsStreaming}
        loading={$coworkLoading}
        progressSteps={[]}
        createdFiles={$coworkCreatedFiles}
        contextTools={$coworkContextTools}
        skillsUsed={[]}
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
</style>
