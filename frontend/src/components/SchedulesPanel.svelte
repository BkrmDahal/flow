<script>
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { schedules, schedulesLoading, refreshSchedules, deleteSchedule, toggleSchedule } from '../lib/stores/schedulesStore.js';
  import { runScheduleNow, catchUpEnabled, loadCatchUpSetting, setCatchUpSetting } from '../lib/stores/schedulesStore.js';
  import { startScheduledCoworkSession } from '../lib/stores/coworkStore.js';
  import CreateTaskModal from './CreateTaskModal.svelte';

  let showCreateModal = false;
  let filterMode = 'all'; // 'all' | 'active' | 'disabled'
  let runningIds = new Set();
  let openMenuId = null; // ID of the task whose overflow menu is open

  // Edit state
  let editingTask = null;
  let showEditModal = false;

  const dispatch = createEventDispatcher();

  onMount(() => {
    refreshSchedules();
    loadCatchUpSetting();
    window.addEventListener('click', closeMenuOnOutsideClick);
  });

  onDestroy(() => {
    window.removeEventListener('click', closeMenuOnOutsideClick);
  });

  function closeMenuOnOutsideClick() {
    if (openMenuId) openMenuId = null;
  }

  $: filtered = filterTasks($schedules, filterMode);

  function filterTasks(tasks, mode) {
    if (mode === 'active') return tasks.filter(t => t.enabled);
    if (mode === 'disabled') return tasks.filter(t => !t.enabled);
    return tasks;
  }

  function formatLastRun(lastRun) {
    if (!lastRun) return 'Never';
    const date = new Date(lastRun);
    const now = new Date();
    const diffMs = now - date;
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return 'Just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const diffDay = Math.floor(diffHr / 24);
    if (diffDay < 7) return `${diffDay}d ago`;
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }

  async function handleDelete(id) {
    openMenuId = null;
    try {
      await deleteSchedule(id);
    } catch (e) {
      console.error(e);
    }
  }

  async function handleToggle(id, currentEnabled) {
    openMenuId = null;
    try {
      await toggleSchedule(id, !currentEnabled);
    } catch (e) {
      console.error(e);
    }
  }

  async function handleRunNow(task) {
    openMenuId = null;
    runningIds.add(task.id);
    runningIds = runningIds;
    try {
      const sessionId = await runScheduleNow(task.id);
      await refreshSchedules();
      if (sessionId) {
        // Set up the frontend streaming state so we receive LLM events live.
        startScheduledCoworkSession(sessionId, task.name, task.instructions);
        dispatch('viewSession', { sessionId });
      }
    } catch (e) {
      console.error('Run now failed:', e);
    } finally {
      runningIds.delete(task.id);
      runningIds = runningIds;
    }
  }

  function handleViewLastRun(lastSessionID) {
    openMenuId = null;
    if (lastSessionID) {
      dispatch('viewSession', { sessionId: lastSessionID });
    }
  }

  function handleEdit(task) {
    openMenuId = null;
    editingTask = task;
    showCreateModal = true;
  }

  function handleModalClose() {
    showCreateModal = false;
    editingTask = null;
  }

  function toggleMenu(id, e) {
    e.stopPropagation();
    openMenuId = openMenuId === id ? null : id;
  }

  function handleCreated() {
    refreshSchedules();
  }
</script>

<div class="schedules-panel">
  <div class="drag-region"></div>

  <div class="schedules-content">
    <!-- Header -->
    <div class="schedules-header">
      <h1>Schedules</h1>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <p class="subtitle">Run tasks on a schedule or whenever you need them.</p>
      <div class="toolbar-actions">
        <div class="filter-wrap">
          <select class="filter-select" bind:value={filterMode}>
            <option value="all">All</option>
            <option value="active">Active</option>
            <option value="disabled">Disabled</option>
          </select>
          <svg class="filter-chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </div>
        <button class="btn-new-task" on:click={() => showCreateModal = true}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <path d="M12 5v14M5 12h14" />
          </svg>
          New task
        </button>
      </div>
    </div>

    <!-- Catch-up toggle -->
    <label class="catchup-toggle">
      <span class="catchup-label">Run missed tasks on startup</span>
      <button
        class="toggle-switch"
        class:active={$catchUpEnabled}
        on:click={() => setCatchUpSetting(!$catchUpEnabled)}
        role="switch"
        aria-checked={$catchUpEnabled}
      >
        <span class="toggle-knob"></span>
      </button>
    </label>

    <!-- Task list -->
    <div class="task-list">
      {#if $schedulesLoading}
        <div class="empty-state">
          <div class="loading-spinner"></div>
          <span>Loading schedules...</span>
        </div>
      {:else if filtered.length === 0}
        <div class="empty-state">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" opacity="0.3">
            <circle cx="12" cy="12" r="10" />
            <polyline points="12 6 12 12 16 14" />
          </svg>
          <span class="empty-text">No scheduled tasks yet.</span>
        </div>
      {:else}
        {#each filtered as task (task.id)}
          <div class="task-card" class:disabled={!task.enabled}>
            <div class="card-top">
              <div class="card-info">
                <span class="card-name">{task.name}</span>
                <p class="card-instructions">{task.instructions}</p>
              </div>
              <div class="card-top-actions">
                <!-- Play / Run button -->
                <button
                  class="btn-play"
                  on:click|stopPropagation={() => handleRunNow(task)}
                  disabled={runningIds.has(task.id)}
                  title="Run now"
                >
                  {#if runningIds.has(task.id)}
                    <div class="run-spinner"></div>
                  {:else}
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <polygon points="5 3 19 12 5 21 5 3" />
                    </svg>
                  {/if}
                </button>

                <!-- Overflow menu -->
                <div class="overflow-container">
                  <button
                    class="btn-overflow"
                    class:active={openMenuId === task.id}
                    on:click={(e) => toggleMenu(task.id, e)}
                    title="More options"
                  >
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                      <circle cx="12" cy="5" r="2" />
                      <circle cx="12" cy="12" r="2" />
                      <circle cx="12" cy="19" r="2" />
                    </svg>
                  </button>

                  {#if openMenuId === task.id}
                    <!-- svelte-ignore a11y-click-events-have-key-events -->
                    <!-- svelte-ignore a11y-no-static-element-interactions -->
                    <div class="overflow-menu" on:click|stopPropagation>
                      <button class="menu-item" on:click={() => handleRunNow(task)}>
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                          <polygon points="5 3 19 12 5 21 5 3" />
                        </svg>
                        Run now
                      </button>
                      {#if task.lastSessionID}
                        <button class="menu-item" on:click={() => handleViewLastRun(task.lastSessionID)}>
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                            <circle cx="12" cy="12" r="3" />
                          </svg>
                          View last run
                        </button>
                      {/if}
                      <button class="menu-item" on:click={() => handleToggle(task.id, task.enabled)}>
                        {#if task.enabled}
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <circle cx="12" cy="12" r="10" />
                            <line x1="10" y1="15" x2="10" y2="9" />
                            <line x1="14" y1="15" x2="14" y2="9" />
                          </svg>
                          Pause
                        {:else}
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polygon points="5 3 19 12 5 21 5 3" />
                          </svg>
                          Resume
                        {/if}
                      </button>
                      <div class="menu-divider"></div>
                      <button class="menu-item" on:click={() => handleEdit(task)}>
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                          <path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7" />
                          <path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z" />
                        </svg>
                        Edit
                      </button>
                      <div class="menu-divider"></div>
                      <button class="menu-item danger" on:click={() => handleDelete(task.id)}>
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
                        </svg>
                        Delete
                      </button>
                    </div>
                  {/if}
                </div>
              </div>
            </div>
            <div class="card-bottom">
              <div class="card-meta">
                <span class="schedule-pill">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <circle cx="12" cy="12" r="10" />
                    <polyline points="12 6 12 12 16 14" />
                  </svg>
                  <code>{task.cronExpr}</code>
                </span>
                <span class="last-run">
                  Last run: {formatLastRun(task.lastRun)}
                </span>
                {#if !task.enabled}
                  <span class="status-badge paused">Paused</span>
                {/if}
              </div>
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<CreateTaskModal
  open={showCreateModal}
  editTask={editingTask}
  on:close={handleModalClose}
  on:created={handleCreated}
/>

<style>
  .schedules-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-primary, var(--bg-app));
    overflow: hidden;
  }

  .drag-region {
    --wails-draggable: drag;
    height: 12px;
    flex-shrink: 0;
  }

  .schedules-content {
    flex: 1;
    overflow-y: auto;
    padding: 0 48px 40px;
    max-width: 780px;
    margin: 0 auto;
    width: 100%;
  }

  .schedules-header {
    padding: 8px 0 4px;
  }

  .schedules-header h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 0 20px;
    gap: 16px;
  }

  .subtitle {
    margin: 0;
    font-size: 14px;
    color: var(--text-muted);
    flex: 1;
  }

  .toolbar-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }

  .filter-wrap {
    position: relative;
  }

  .filter-select {
    padding: 7px 28px 7px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-secondary);
    font-size: 13px;
    font-family: inherit;
    appearance: none;
    cursor: pointer;
    outline: none;
    transition: border-color 0.15s ease;
  }
  .filter-select:focus {
    border-color: var(--border-focus);
  }

  .filter-chevron {
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    pointer-events: none;
    color: var(--text-muted);
  }

  .btn-new-task {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.15s ease;
    white-space: nowrap;
  }
  .btn-new-task:hover {
    background: var(--bg-hover);
    border-color: var(--text-muted);
  }

  .catchup-toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    margin-bottom: 16px;
    cursor: pointer;
  }

  .catchup-label {
    font-size: 13px;
    color: var(--text-secondary);
  }

  .toggle-switch {
    position: relative;
    width: 36px;
    height: 20px;
    background: var(--bg-hover);
    border: 1px solid var(--border);
    border-radius: 20px;
    cursor: pointer;
    transition: all 0.2s ease;
    padding: 0;
    flex-shrink: 0;
  }
  .toggle-switch.active {
    background: rgba(45, 212, 191, 0.25);
    border-color: rgba(45, 212, 191, 0.4);
  }

  .toggle-knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    background: var(--text-muted);
    border-radius: 50%;
    transition: all 0.2s ease;
  }
  .toggle-switch.active .toggle-knob {
    left: 18px;
    background: var(--accent);
  }

  .task-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 60px 20px;
    color: var(--text-muted);
    font-size: 14px;
  }

  .empty-text {
    opacity: 0.6;
  }

  .loading-spinner {
    width: 24px;
    height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* ── Task Card ── */
  .task-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 14px;
    padding: 18px 20px;
    transition: all 0.15s ease;
  }
  .task-card:hover {
    border-color: rgba(255, 255, 255, 0.1);
  }
  .task-card.disabled {
    opacity: 0.55;
  }

  .card-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }

  .card-info {
    flex: 1;
    min-width: 0;
  }

  .card-name {
    font-size: 15px;
    font-weight: 600;
    color: var(--text-primary);
    display: block;
    margin-bottom: 6px;
  }

  .card-instructions {
    margin: 0;
    font-size: 13px;
    color: var(--text-muted);
    line-height: 1.5;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .card-top-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
  }

  /* Play button */
  .btn-play {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    background: none;
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .btn-play:hover:not(:disabled) {
    background: var(--accent-bg);
    border-color: rgba(45, 212, 191, 0.2);
    color: var(--accent);
  }
  .btn-play:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .run-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid rgba(45, 212, 191, 0.3);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  /* Overflow / "..." menu */
  .overflow-container {
    position: relative;
  }

  .btn-overflow {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    background: none;
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .btn-overflow:hover, .btn-overflow.active {
    background: var(--bg-hover);
    color: var(--text-secondary);
  }

  .overflow-menu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    min-width: 170px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.45);
    z-index: 100;
    padding: 6px;
    animation: menuFade 0.12s ease;
  }

  @keyframes menuFade {
    from { opacity: 0; transform: translateY(-4px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .menu-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 10px 12px;
    background: none;
    border: none;
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 14px;
    font-family: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 0.1s ease;
  }
  .menu-item:hover {
    background: var(--bg-hover);
  }
  .menu-item svg {
    flex-shrink: 0;
    color: var(--text-secondary);
  }

  .menu-item.danger {
    color: #ef4444;
  }
  .menu-item.danger svg {
    color: #ef4444;
  }
  .menu-item.danger:hover {
    background: rgba(239, 68, 68, 0.08);
  }

  .menu-divider {
    height: 1px;
    background: var(--border);
    margin: 4px 8px;
  }

  .card-bottom {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 16px;
    flex: 1;
    min-width: 0;
  }

  .schedule-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 10px;
    background: var(--accent-bg);
    color: var(--accent);
    border-radius: 20px;
    font-size: 12px;
    font-weight: 500;
    white-space: nowrap;
  }
  .schedule-pill svg {
    flex-shrink: 0;
  }
  .schedule-pill code {
    font-family: 'SF Mono', 'Menlo', 'Consolas', monospace;
    font-size: 11px;
    letter-spacing: 0.3px;
  }

  .last-run {
    font-size: 12px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .status-badge {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 20px;
    white-space: nowrap;
  }
  .status-badge.paused {
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.1);
  }
</style>
