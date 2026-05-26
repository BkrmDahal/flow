<script>
  import { createEventDispatcher, onMount } from 'svelte';

  export let progressSteps = [];
  export let files = [];
  export let contextTools = [];
  export let skillsUsed = [];
  export let activeModel = '';
  const dispatch = createEventDispatcher();

  let planOpen = true;
  let skillsOpen = true;
  let filesOpen = true;
  let contextOpen = true;

  // ── Resizer state ──
  let sidebarWidth = 220;
  let isResizing = false;

  onMount(() => {
    // Load UI widths asynchronously from backend, fallback to localStorage
    (async () => {
      let loadedFromBackend = false;
      try {
        const widths = await Backend.GetUIWidths();
        if (widths && widths.agentRightSidebarWidth > 0) {
          sidebarWidth = Math.max(160, Math.min(450, widths.agentRightSidebarWidth));
          loadedFromBackend = true;
        }
      } catch (e) {
        console.warn('Failed to load UI widths from backend:', e);
      }

      if (!loadedFromBackend) {
        const saved = localStorage.getItem('flow-agent-right-sidebar-width');
        if (saved) {
          sidebarWidth = Math.max(160, Math.min(450, parseInt(saved, 10)));
          try {
            await Backend.SaveUIWidths(0, sidebarWidth);
          } catch (e) {}
        }
      }
    })();
  });

  function startResize(e) {
    e.preventDefault();
    isResizing = true;
    window.addEventListener('mousemove', handleResize);
    window.addEventListener('mouseup', stopResize);
  }

  function handleResize(e) {
    if (!isResizing) return;
    const newWidth = window.innerWidth - e.clientX;
    sidebarWidth = Math.max(160, Math.min(450, newWidth));
  }

  async function stopResize() {
    isResizing = false;
    window.removeEventListener('mousemove', handleResize);
    window.removeEventListener('mouseup', stopResize);
    localStorage.setItem('flow-agent-right-sidebar-width', sidebarWidth.toString());
    try {
      const widths = await Backend.GetUIWidths();
      const leftWidth = widths ? widths.sidebarWidth : 0;
      await Backend.SaveUIWidths(leftWidth, sidebarWidth);
    } catch (e) {
      console.warn('Failed to save UI widths to backend:', e);
    }
  }

  $: totalTodos = progressSteps.length;
  $: completedTodos = progressSteps.filter(s => s.status === 'completed').length;
  $: inProgressTodos = progressSteps.filter(s => s.status === 'in_progress').length;
  $: statusSummary = buildStatusSummary(totalTodos, completedTodos, inProgressTodos);

  function buildStatusSummary(total, completed, inProgress) {
    if (total === 0) return '';
    if (completed === total) return 'Completed In Order';
    return `${completed}/${total} completed`;
  }

  function fileIcon(name) {
    const ext = (name || '').split('.').pop()?.toLowerCase() || '';
    const codeExts = new Set(['js', 'ts', 'py', 'go', 'sh', 'json', 'html', 'css', 'svelte']);
    if (codeExts.has(ext)) return 'code';
    return 'file';
  }

  function handleOpenFile(file) {
    dispatch('openFile', { path: file.path, name: file.name });
  }

  function handleOpenFolder() {
    dispatch('openFolder');
  }
</script>

<aside class="info-panel" style="width: {sidebarWidth}px;">
  <div class="resize-handle left" class:active={isResizing} on:mousedown={startResize} role="separator" aria-label="Resize Sidebar"></div>
  <!-- Plan Section -->
  <div class="panel-section">
    <button class="section-header" on:click={() => planOpen = !planOpen}>
      <span class="section-title">Plan</span>
      <svg class="chevron" class:open={planOpen} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
    {#if planOpen}
      <div class="section-content">
        {#if progressSteps.length === 0}
          <p class="empty-text">Plan will appear when the agent starts working</p>
        {:else}
          <div class="plan-summary">
            <span class="plan-count">{totalTodos} To-do{totalTodos !== 1 ? 's' : ''} · {statusSummary}</span>
            {#if activeModel}
              <div class="active-model-tag" title="Active model running this plan">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path>
                  <polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
                  <line x1="12" y1="22.08" x2="12" y2="12"></line>
                </svg>
                <span>{activeModel}</span>
              </div>
            {/if}
          </div>
          <div class="plan-list">
            {#each progressSteps as step}
              <div class="plan-item" title={step.label} class:completed={step.status === 'completed'} class:active={step.status === 'in_progress'} class:pending={step.status === 'pending'}>
                <div class="plan-icon">
                  {#if step.status === 'completed'}
                    <div class="icon-completed">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round">
                        <path d="M20 6L9 17l-5-5" />
                      </svg>
                    </div>
                  {:else if step.status === 'in_progress'}
                    <div class="icon-progress"></div>
                  {:else}
                    <div class="icon-pending"></div>
                  {/if}
                </div>
                <span class="plan-label" class:strikethrough={step.status === 'completed'}>{step.label}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Working Folder Section -->
  <div class="panel-section">
    <button class="section-header" on:click={() => filesOpen = !filesOpen}>
      <span class="section-title">Working folder</span>
      <div class="section-header-actions">
        {#if files.length > 0}
          <button class="header-action-btn" on:click|stopPropagation={handleOpenFolder} title="Open folder">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
              <polyline points="15 3 21 3 21 9"/>
              <line x1="10" y1="14" x2="21" y2="3"/>
            </svg>
          </button>
        {/if}
        <svg class="chevron" class:open={filesOpen} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 9l6 6 6-6" />
        </svg>
      </div>
    </button>
    {#if filesOpen}
      <div class="section-content">
        {#if files.length === 0}
          <p class="empty-text">No files created yet</p>
        {:else}
          <div class="file-list">
            {#each files as file}
              <button class="file-item" on:click={() => handleOpenFile(file)} title={file.path}>
                <div class="file-item-icon">
                  {#if fileIcon(file.name) === 'code'}
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="16 18 22 12 16 6"/>
                      <polyline points="8 6 2 12 8 18"/>
                    </svg>
                  {:else}
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z"/>
                      <path d="M14 2v6h6"/>
                    </svg>
                  {/if}
                </div>
                <span class="file-item-name">{file.name}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Skills Section -->
  <div class="panel-section">
    <button class="section-header" on:click={() => skillsOpen = !skillsOpen}>
      <span class="section-title">Skills</span>
      <svg class="chevron" class:open={skillsOpen} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
    {#if skillsOpen}
      <div class="section-content">
        {#if skillsUsed.length === 0}
          <p class="empty-text">No skills used yet</p>
        {:else}
          <div class="skill-list">
            {#each skillsUsed as skill}
              <div class="skill-tag">
                <svg class="skill-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/>
                  <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
                </svg>
                <span>{skill}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Context Section -->
  <div class="panel-section">
    <button class="section-header" on:click={() => contextOpen = !contextOpen}>
      <span class="section-title">Context</span>
      <svg class="chevron" class:open={contextOpen} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
    {#if contextOpen}
      <div class="section-content">
        {#if contextTools.length === 0}
          <p class="empty-text">Tools used will appear here</p>
        {:else}
          <div class="context-list">
            {#each contextTools as tool}
              <span class="context-tag">{tool}</span>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</aside>

<style>
  .info-panel {
    position: relative;
    width: 220px; height: 100%;
    background: var(--bg-primary);
    border-left: 1px solid var(--border);
    flex-shrink: 0;
    display: flex; flex-direction: column;
    overflow-y: auto;
    padding-top: 8px;
  }

  .resize-handle {
    position: absolute;
    top: 0; bottom: 0;
    width: 4px;
    cursor: col-resize;
    z-index: 100;
    transition: background 0.15s ease;
  }
  .resize-handle.left {
    left: 0;
  }
  .resize-handle:hover,
  .resize-handle.active {
    background: var(--accent);
  }

  .info-panel::-webkit-scrollbar { width: 4px; }
  .info-panel::-webkit-scrollbar-track { background: transparent; }
  .info-panel::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

  .panel-section { border-bottom: 1px solid var(--border); }

  .section-header {
    display: flex; align-items: center; justify-content: space-between;
    width: 100%; padding: 12px 16px;
    background: none; border: none; cursor: pointer;
    transition: background 0.15s ease;
    font-family: var(--font-sans);
  }
  .section-header:hover { background: var(--bg-hover); }

  .section-title {
    font-size: 12px; font-weight: 600; color: var(--text-primary);
    text-transform: uppercase; letter-spacing: 0.5px;
  }

  .section-header-actions { display: flex; align-items: center; gap: 4px; }

  .header-action-btn {
    display: flex; align-items: center; justify-content: center;
    width: 22px; height: 22px; padding: 0;
    background: none; border: none; border-radius: 4px;
    color: var(--text-muted); cursor: pointer;
    transition: all 0.15s ease;
  }
  .header-action-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }

  .chevron {
    color: var(--text-muted);
    transition: transform 0.2s ease;
    flex-shrink: 0;
  }
  .chevron.open { transform: rotate(0deg); }
  .chevron:not(.open) { transform: rotate(-90deg); }

  .section-content {
    padding: 0 16px 12px;
    animation: sectionFadeIn 0.15s ease;
  }
  @keyframes sectionFadeIn { from { opacity: 0; } to { opacity: 1; } }

  .empty-text { font-size: 12px; color: var(--text-muted); line-height: 1.4; }

  /* Plan */
  .plan-summary {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    margin-bottom: 12px;
  }
  .plan-count { font-size: 12px; color: var(--text-muted); font-weight: 500; }
  
  .active-model-tag {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    margin-top: 2px;
    padding: 3px 8px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px;
    font-size: 11px;
    color: var(--text-secondary, #d4d4d8);
    max-width: 100%;
    box-sizing: border-box;
  }
  .active-model-tag svg {
    color: var(--accent, #10b981);
    flex-shrink: 0;
  }
  .active-model-tag span {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .plan-list {
    display: flex;
    flex-direction: column;
    gap: 0;
    max-height: 144px;
    overflow-y: auto;
    padding-right: 4px;
  }
  .plan-list::-webkit-scrollbar { width: 3px; }
  .plan-list::-webkit-scrollbar-track { background: transparent; }
  .plan-list::-webkit-scrollbar-thumb { background: var(--border); border-radius: 1.5px; }

  .plan-item {
    display: flex; align-items: flex-start; gap: 10px;
    padding: 7px 0;
    border-bottom: 1px solid rgba(255,255,255,0.04);
    transition: opacity 0.2s ease;
  }
  .plan-item:last-child { border-bottom: none; }
  .plan-icon { flex-shrink: 0; margin-top: 2px; }

  .icon-completed {
    width: 18px; height: 18px; border-radius: 50%;
    background: #22c55e;
    display: flex; align-items: center; justify-content: center;
    color: white;
  }

  .icon-progress {
    width: 18px; height: 18px;
    border: 2px solid rgba(255,255,255,0.1);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .icon-pending {
    width: 18px; height: 18px; border-radius: 50%;
    border: 1.5px solid rgba(255,255,255,0.15);
  }

  .plan-label {
    font-size: 12.5px;
    color: var(--text-secondary);
    line-height: 1.45;
    min-width: 0;
    flex: 1;
    display: block;
    max-height: 2.9em;
    overflow: hidden;
    word-wrap: break-word;
    -webkit-mask-image: linear-gradient(to bottom, #000 1.3em, transparent 2.8em);
    mask-image: linear-gradient(to bottom, #000 1.3em, transparent 2.8em);
  }
  .plan-label.strikethrough { text-decoration: line-through; color: var(--text-muted); opacity: 0.7; }
  .plan-item.active .plan-label { color: var(--text-primary); font-weight: 500; }
  .plan-item.pending .plan-label { color: var(--text-secondary); }

  /* Files */
  .file-list { display: flex; flex-direction: column; gap: 2px; }

  .file-item {
    display: flex; align-items: center; gap: 8px;
    width: 100%; padding: 6px 8px;
    background: none; border: none; border-radius: 6px;
    cursor: pointer; transition: all 0.15s ease; text-align: left;
    font-family: var(--font-sans);
  }
  .file-item:hover { background: var(--bg-hover); }

  .file-item-icon {
    width: 24px; height: 24px;
    display: flex; align-items: center; justify-content: center;
    background: rgba(255,255,255,0.04); border-radius: 5px;
    color: var(--text-muted); flex-shrink: 0;
  }

  .file-item-name {
    font-size: 12px; color: var(--text-secondary);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
  }

  /* Skills */
  .skill-list { display: flex; flex-direction: column; gap: 4px; }

  .skill-tag {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 10px;
    background: rgba(139,92,246,0.08);
    border: 1px solid rgba(139,92,246,0.2);
    border-radius: 8px;
    font-size: 12px; color: var(--text-secondary);
  }

  .skill-icon { color: rgba(139,92,246,0.7); flex-shrink: 0; }

  /* Context */
  .context-list { display: flex; flex-wrap: wrap; gap: 6px; }

  .context-tag {
    display: inline-block;
    padding: 3px 10px;
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    border-radius: 12px;
    font-size: 11px; color: var(--text-secondary); white-space: nowrap;
  }
</style>
