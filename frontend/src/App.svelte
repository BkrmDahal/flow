<script>
  import FlowPanel from './components/FlowPanel.svelte';
  import CoworkWorkspace from './components/CoworkWorkspace.svelte';
  import SettingsModal from './components/SettingsModal.svelte';

  import { OpenFileInApp } from '../wailsjs/go/backend/App';

  let activeTab = 'cowork';  // 'cowork' | 'flow'
  let settingsOpen = false;

  async function openLogs() {
    try { await OpenFileInApp('~/.flow/logs/') } catch (e) { console.warn(e) }
  }
</script>

<main>
  <!-- Titlebar -->
  <header class="titlebar">
    <div class="titlebar-left"></div>
    <nav class="tabs">
      <button class="tab" class:active={activeTab === 'cowork'} on:click={() => activeTab = 'cowork'}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87"/><path d="M16 3.13a4 4 0 010 7.75"/></svg>
        Cowork
      </button>
      <button class="tab" class:active={activeTab === 'flow'} on:click={() => activeTab = 'flow'}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 1a3 3 0 00-3 3v8a3 3 0 006 0V4a3 3 0 00-3-3z"/><path d="M19 10v2a7 7 0 01-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
        Flow
      </button>
    </nav>
    <div class="titlebar-right">
      <button class="icon-btn" on:click={openLogs} title="Open log file">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
          <polyline points="14 2 14 8 20 8"/>
          <line x1="16" y1="13" x2="8" y2="13"/>
          <line x1="16" y1="17" x2="8" y2="17"/>
          <polyline points="10 9 9 9 8 9"/>
        </svg>
      </button>
      <button class="icon-btn" on:click={() => settingsOpen = true} title="Settings">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="3"/>
          <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>
        </svg>
      </button>
    </div>
  </header>

  <section class="body">
    {#if activeTab === 'flow'}
      <FlowPanel onOpenSettings={() => settingsOpen = true} />
    {:else}
      <CoworkWorkspace onOpenSettings={() => settingsOpen = true} />
    {/if}
  </section>

  <SettingsModal open={settingsOpen} onClose={() => settingsOpen = false} />
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    height: 100%;
    width: 100%;
    background: var(--bg-app);
  }

  .titlebar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 16px;
    -webkit-app-region: drag;
    border-bottom: 1px solid var(--border-subtle);
    height: 40px;
    flex-shrink: 0;
  }

  .titlebar-left { width: 100px; flex-shrink: 0; }

  .tabs {
    -webkit-app-region: no-drag;
    display: inline-flex;
    align-items: center;
    gap: 2px;
    background: var(--bg-surface);
    border-radius: 10px;
    padding: 3px;
  }

  .tab {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    background: transparent;
    color: var(--text-muted);
    border: none;
    padding: 5px 14px;
    border-radius: 8px;
    font-size: 12.5px;
    font-weight: 500;
    transition: all 0.15s ease;
    white-space: nowrap;
  }
  .tab svg { opacity: 0.7; }
  .tab:hover { color: var(--text-secondary); }
  .tab.active { color: var(--text-primary); background: var(--bg-card); box-shadow: 0 1px 3px rgba(0,0,0,0.2); }
  .tab.active svg { opacity: 1; color: var(--accent); }

  .titlebar-right {
    width: 100px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: flex-end;
    gap: 4px;
    -webkit-app-region: no-drag;
  }

  .icon-btn {
    background: transparent; color: var(--text-muted); border: none;
    width: 28px; height: 28px; border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    transition: all 0.15s ease;
  }
  .icon-btn:hover { color: var(--text-secondary); background: var(--bg-hover); }

  .body {
    flex: 1; overflow: hidden;
    display: flex; min-height: 0;
  }
  .body > :global(*) { flex: 1; min-height: 0; }
</style>
