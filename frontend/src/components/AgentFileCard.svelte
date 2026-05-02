<script>
  import { createEventDispatcher } from 'svelte';

  export let file = null; // { name, path, type?, size? }

  const dispatch = createEventDispatcher();

  const ICON_MAP = {
    'docx': 'doc', 'doc': 'doc', 'pdf': 'pdf',
    'txt': 'text', 'md': 'text', 'csv': 'text',
    'json': 'code', 'js': 'code', 'ts': 'code', 'py': 'code',
    'go': 'code', 'sh': 'code', 'html': 'code', 'css': 'code', 'svelte': 'code',
    'png': 'image', 'jpg': 'image', 'jpeg': 'image', 'gif': 'image', 'webp': 'image', 'svg': 'image',
  };

  function fileExtension(name) {
    const parts = (name || '').split('.');
    return parts.length > 1 ? parts.pop().toLowerCase() : '';
  }

  function fileTypeBadge(name) {
    const ext = fileExtension(name);
    return ext ? ext.toUpperCase() : 'FILE';
  }

  function iconType(name) {
    const ext = fileExtension(name);
    return ICON_MAP[ext] || 'file';
  }

  function formatSize(bytes) {
    if (!bytes) return '';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  function handleOpen() {
    dispatch('openFile', { path: file.path, name: file.name });
  }

  function handleRevealFolder() {
    dispatch('openFolder', { path: file.path, name: file.name });
  }

  $: ext = fileExtension(file?.name || '');
  $: badge = fileTypeBadge(file?.name || '');
  $: icon = iconType(file?.name || '');
</script>

{#if file}
  <div class="file-card">
    <div class="file-icon-area">
      {#if icon === 'doc'}
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z"/>
          <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8"/>
        </svg>
      {:else if icon === 'code'}
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="16 18 22 12 16 6"/>
          <polyline points="8 6 2 12 8 18"/>
        </svg>
      {:else if icon === 'image'}
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
          <circle cx="8.5" cy="8.5" r="1.5"/>
          <polyline points="21 15 16 10 5 21"/>
        </svg>
      {:else}
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z"/>
          <path d="M14 2v6h6"/>
        </svg>
      {/if}
    </div>

    <div class="file-info">
      <span class="file-name">{file.name}</span>
      <span class="file-meta">
        Document · {badge}
        {#if file.size}
          · {formatSize(file.size)}
        {/if}
      </span>
    </div>

    <div class="file-actions">
      <button class="file-action-btn btn-folder" on:click={handleRevealFolder} title="Show in Finder">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
        </svg>
      </button>
      <button class="file-action-btn btn-open" on:click={handleOpen} title="Open file">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
          <polyline points="15 3 21 3 21 9"/>
          <line x1="10" y1="14" x2="21" y2="3"/>
        </svg>
        <span>Open</span>
      </button>
    </div>
  </div>
{/if}

<style>
  .file-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    border-radius: 12px;
    transition: border-color 0.15s ease;
    animation: cardFadeIn 0.2s ease;
  }

  .file-card:hover { border-color: var(--border-light); }

  @keyframes cardFadeIn {
    from { opacity: 0; transform: translateY(4px); }
    to   { opacity: 1; transform: translateY(0); }
  }

  .file-icon-area {
    width: 40px; height: 40px;
    display: flex; align-items: center; justify-content: center;
    background: rgba(255,255,255,0.04);
    border-radius: 8px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .file-info {
    flex: 1; min-width: 0;
    display: flex; flex-direction: column; gap: 2px;
  }

  .file-name {
    font-size: 13px; font-weight: 500; color: var(--text-primary);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }

  .file-meta { font-size: 11px; color: var(--text-muted); }

  .file-actions { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }

  .file-action-btn {
    display: flex; align-items: center; justify-content: center; gap: 5px;
    padding: 6px 8px;
    background: transparent; border: none; border-radius: 6px;
    color: var(--text-muted); cursor: pointer;
    transition: all 0.15s ease;
    font-family: var(--font-sans); font-size: 12px;
  }

  .file-action-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }

  .btn-folder {
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    border-radius: 8px; padding: 6px 8px;
  }
  .btn-folder:hover { background: rgba(255,255,255,0.1); color: var(--text-primary); }

  .btn-open {
    background: rgba(255,255,255,0.06);
    border: 1px solid var(--border);
    border-radius: 8px; padding: 5px 10px;
    font-weight: 500; color: var(--text-secondary);
  }
  .btn-open:hover { background: rgba(255,255,255,0.1); color: var(--text-primary); }
</style>
