<script>
  import { createEventDispatcher, onMount } from 'svelte';

  export let agentTaskHistory = [];
  export let activeAgentTaskId = null;
  export let bgStreamingAgents = new Set();

  const dispatch = createEventDispatcher();

  let searchQuery = '';
  let hoveredTaskId = null;

  // ── Resizer state ──
  let sidebarWidth = 220;
  let isResizing = false;

  onMount(() => {
    const saved = localStorage.getItem('flow-sidebar-width');
    if (saved) {
      sidebarWidth = Math.max(160, Math.min(450, parseInt(saved, 10)));
    }
  });

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
    localStorage.setItem('flow-sidebar-width', sidebarWidth.toString());
  }

  $: filtered = searchQuery
    ? agentTaskHistory.filter(t => t.title.toLowerCase().includes(searchQuery.toLowerCase()))
    : agentTaskHistory;

  $: grouped = groupByTime(filtered);

  function groupByTime(items) {
    const groups = [];
    let currentLabel = '';
    for (const item of items) {
      const label = formatTimestamp(item.timestamp);
      if (label !== currentLabel) {
        currentLabel = label;
        groups.push({ label, items: [] });
      }
      groups[groups.length - 1].items.push(item);
    }
    return groups;
  }

  function formatTimestamp(ts) {
    if (!ts) return '';
    const date = new Date(ts);
    const now = new Date();
    const diffDays = Math.floor((now - date) / 86400000);
    if (diffDays === 0) return 'Today';
    if (diffDays === 1) return 'Yesterday';
    if (diffDays < 7) return `${diffDays}d ago`;
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }
</script>

<aside class="sidebar" style="width: {sidebarWidth}px;">
  <div class="resize-handle" class:active={isResizing} on:mousedown={startResize} role="separator" aria-label="Resize Sidebar"></div>
  <div class="drag-region"></div>
  <div class="sidebar-inner">
    <button class="nav-new" on:click={() => dispatch('newTask')}>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M12 5v14M5 12h14" />
      </svg>
      <span>New chat</span>
    </button>

    <div class="search-wrapper">
      <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
        <circle cx="11" cy="11" r="8" />
        <path d="M21 21l-4.35-4.35" />
      </svg>
      <input class="search-input" type="text" placeholder="Search chats" bind:value={searchQuery} />
    </div>

    <div class="divider"></div>

    <div class="task-list">
      {#if filtered.length === 0}
        <p class="empty">{searchQuery ? 'No matching chats' : 'Your chats will show up here'}</p>
      {:else}
        {#each grouped as group}
          <div class="group">
            <div class="group-label">{group.label}</div>
            {#each group.items as task (task.id)}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <button
                class="task-item"
                class:active={task.id === activeAgentTaskId}
                on:click={() => dispatch('selectTask', { id: task.id })}
                on:mouseenter={() => hoveredTaskId = task.id}
                on:mouseleave={() => hoveredTaskId = null}
                title={task.title}
              >
                {#if bgStreamingAgents.has(task.id)}
                  <span class="streaming-dot"></span>
                {/if}
                <span class="task-title">{task.title}</span>
                <button
                  class="action-btn"
                  class:visible={hoveredTaskId === task.id}
                  on:click|stopPropagation={(e) => dispatch('openFolder', { id: task.id })}
                  title="Open folder"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
                  </svg>
                </button>
                <button
                  class="action-btn delete-btn"
                  class:visible={hoveredTaskId === task.id}
                  on:click|stopPropagation={(e) => dispatch('deleteTask', { id: task.id })}
                  title="Delete task"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
                  </svg>
                </button>
              </button>
            {/each}
          </div>
        {/each}
      {/if}
    </div>
  </div>

  <div class="sidebar-footer">
    <button class="footer-nav-btn" on:click={() => dispatch('openToolkit')} title="Customize">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <line x1="4" y1="21" x2="4" y2="14" />
        <line x1="4" y1="10" x2="4" y2="3" />
        <line x1="12" y1="21" x2="12" y2="12" />
        <line x1="12" y1="8" x2="12" y2="3" />
        <line x1="20" y1="21" x2="20" y2="16" />
        <line x1="20" y1="12" x2="20" y2="3" />
        <line x1="1" y1="14" x2="7" y2="14" />
        <line x1="9" y1="8" x2="15" y2="8" />
        <line x1="17" y1="16" x2="23" y2="16" />
      </svg>
      <span>Customize</span>
    </button>
    <button class="footer-btn" on:click={() => dispatch('openSettings')} title="Settings">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
      </svg>
    </button>
  </div>
</aside>

<style>
  .sidebar {
    position: relative;
    width: 220px; height: 100%;
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    flex-shrink: 0;
    display: flex; flex-direction: column;
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

  .drag-region { --wails-draggable: drag; height: 12px; flex-shrink: 0; }

  .sidebar-inner {
    display: flex; flex-direction: column; flex: 1;
    padding: 0 10px 12px;
    overflow-y: auto; overflow-x: hidden; min-height: 0;
  }
  .sidebar-inner::-webkit-scrollbar { width: 4px; }
  .sidebar-inner::-webkit-scrollbar-track { background: transparent; }
  .sidebar-inner::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

  .nav-new {
    display: flex; align-items: center; gap: 8px;
    width: 100%; padding: 8px 12px;
    background: none; border: none; border-radius: 8px;
    color: var(--text-primary);
    font-size: 13.5px; font-weight: 500; font-family: var(--font-sans);
    cursor: pointer; transition: all 0.15s ease; text-align: left;
    margin-bottom: 2px;
  }
  .nav-new:hover { background: var(--bg-hover); }

  .search-wrapper {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 12px; border-radius: 8px;
    transition: all 0.15s ease;
  }
  .search-wrapper:focus-within { background: var(--bg-hover); }

  .search-icon { flex-shrink: 0; color: var(--text-muted); }

  .search-input {
    flex: 1; background: none; border: none; outline: none;
    color: var(--text-secondary);
    font-size: 13.5px; font-family: var(--font-sans);
    padding: 4px 0;
  }
  .search-input::placeholder { color: var(--text-muted); }

  .divider { height: 1px; background: var(--border); margin: 4px 12px; }

  .task-list {
    flex: 1; display: flex; flex-direction: column;
    gap: 1px; min-height: 0; overflow-y: auto; overflow-x: hidden;
  }

  .empty { padding: 12px; font-size: 13px; color: var(--text-muted); line-height: 1.4; }

  .group { margin-bottom: 2px; }

  .group-label {
    padding: 6px 12px 2px;
    font-size: 11px; font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase; letter-spacing: 0.5px;
  }

  .task-item {
    display: flex; align-items: center;
    width: 100%; padding: 6px 12px;
    background: none; border: none; border-radius: 6px;
    color: var(--text-secondary);
    font-size: 13px; font-family: var(--font-sans);
    cursor: pointer; transition: all 0.15s ease; text-align: left;
    position: relative;
  }
  .task-item:hover { background: var(--bg-hover); color: var(--text-primary); }
  .task-item.active { background: rgba(255,255,255,0.08); color: var(--text-primary); }

  .task-title {
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    min-width: 0;
    -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 24px), transparent 100%);
    mask-image: linear-gradient(to right, #000 calc(100% - 24px), transparent 100%);
  }

  .streaming-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0; margin-right: 6px;
    animation: pulse 1.5s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; } 50% { opacity: 0.4; }
  }

  .action-btn {
    display: flex; align-items: center; justify-content: center;
    width: 0; height: 22px; padding: 0;
    background: none; border: none; border-radius: 4px;
    color: var(--text-muted); cursor: pointer;
    transition: width 0.15s ease, opacity 0.15s ease;
    opacity: 0; flex-shrink: 0;
    overflow: hidden;
  }
  .action-btn.visible { width: 22px; opacity: 1; }
  .action-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .delete-btn:hover { color: #f87171; background: rgba(248,113,113,0.1); }

  .sidebar-footer {
    padding: 8px 10px;
    border-top: 1px solid var(--border);
    display: flex; align-items: center; justify-content: space-between; gap: 4px;
  }

  .footer-nav-btn {
    display: flex; align-items: center; gap: 8px;
    flex: 1; height: 32px; padding: 0 10px;
    background: none; border: none; border-radius: 8px;
    color: var(--text-secondary); cursor: pointer;
    transition: all 0.15s ease;
    font-size: 13px; font-weight: 500; font-family: var(--font-sans);
  }
  .footer-nav-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .footer-nav-btn svg { flex-shrink: 0; }

  .footer-btn {
    display: flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; flex-shrink: 0;
    background: none; border: none; border-radius: 8px;
    color: var(--text-muted); cursor: pointer;
    transition: all 0.15s ease;
  }
  .footer-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
</style>
