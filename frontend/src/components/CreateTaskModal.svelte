<script>
  import { createEventDispatcher } from 'svelte';
  import { createSchedule } from '../lib/stores/schedulesStore.js';

  export let open = false;
  export let editTask = null; // When set, modal is in edit mode

  const dispatch = createEventDispatcher();

  let name = '';
  let instructions = '';
  let cronExpr = '0 9 * * *';
  let saving = false;
  let error = '';
  let _editApplied = null; // tracks which editTask ID was already applied

  // Pre-fill ONCE when modal opens with an editTask
  $: if (open && editTask && _editApplied !== editTask.id) {
    name = editTask.name || '';
    instructions = editTask.instructions || '';
    cronExpr = editTask.cronExpr || '0 9 * * *';
    _editApplied = editTask.id;
  }
  // Reset tracking when modal closes
  $: if (!open) {
    _editApplied = null;
  }

  $: isEdit = !!editTask;

  // Common cron presets for quick selection
  const presets = [
    { label: 'Every minute',          value: '* * * * *' },
    { label: 'Every hour',            value: '0 * * * *' },
    { label: 'Every 30 minutes',      value: '*/30 * * * *' },
    { label: 'Every day at 9 AM',     value: '0 9 * * *' },
    { label: 'Every day at 7 PM',     value: '0 19 * * *' },
    { label: 'Every Monday at 9 AM',  value: '0 9 * * 1' },
    { label: 'Every weekday at 8 AM', value: '0 8 * * 1-5' },
  ];

  let showPresets = false;

  function selectPreset(value) {
    cronExpr = value;
  }

  function resetForm() {
    name = '';
    instructions = '';
    cronExpr = '0 9 * * *';
    saving = false;
    error = '';
    showPresets = false;
    _editApplied = null;
  }

  function handleClose() {
    resetForm();
    dispatch('close');
  }

  async function handleConfirm() {
    if (!name.trim()) {
      error = 'Name is required';
      return;
    }
    if (!instructions.trim()) {
      error = 'Instructions are required';
      return;
    }
    if (!cronExpr.trim()) {
      error = 'Cron expression is required';
      return;
    }
    // Basic validation: must be 5 fields
    const fields = cronExpr.trim().split(/\s+/);
    if (fields.length !== 5) {
      error = 'Cron expression must have 5 fields (min hour dom month dow)';
      return;
    }
    error = '';
    saving = true;

    try {
      await createSchedule({
        id: editTask?.id || '',
        name: name.trim(),
        instructions: instructions.trim(),
        cronExpr: cronExpr.trim(),
        enabled: editTask?.enabled ?? true,
        lastRun: '',
        createdAt: '',
      });
      resetForm();
      dispatch('created');
      dispatch('close');
    } catch (e) {
      error = e?.message || 'Failed to create task';
    } finally {
      saving = false;
    }
  }

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget) handleClose();
  }

  function handleKeydown(e) {
    if (e.key === 'Escape') handleClose();
  }
</script>

{#if open}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="modal-backdrop" on:click={handleBackdropClick} on:keydown={handleKeydown}>
    <div class="modal" role="dialog" aria-modal="true" aria-label="Create task">
      <div class="modal-header">
        <h2>{isEdit ? 'Edit task' : 'Create task'}</h2>
        <button class="close-btn" on:click={handleClose} title="Close">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
      </div>

      <div class="modal-body">
        <!-- Name (full width) -->
        <div class="form-group">
          <label class="form-label">Name <span class="required">*</span></label>
          <div class="input-wrap">
            <input
              type="text"
              class="form-input"
              placeholder="Name your task"
              bind:value={name}
              maxlength="50"
            />
            <span class="char-count">{name.length}/50</span>
          </div>
        </div>

        <!-- Instructions -->
        <div class="form-group">
          <label class="form-label">Instructions <span class="required">*</span></label>
          <div class="textarea-wrap">
            <textarea
              class="form-textarea"
              placeholder="Describe what you want the Agent to do..."
              bind:value={instructions}
              maxlength="8000"
              rows="6"
            ></textarea>
            <span class="char-count textarea-count">{instructions.length}/8000</span>
          </div>
        </div>

        <!-- Cron expression -->
        <div class="form-group">
          <label class="form-label">Schedule (cron) <span class="required">*</span></label>
          <div class="input-wrap">
            <input
              type="text"
              class="form-input cron-input"
              placeholder="* * * * *"
              bind:value={cronExpr}
              spellcheck="false"
            />
          </div>
          <div class="preset-chips">
            {#each presets as preset}
              <button
                class="preset-chip"
                class:active={cronExpr.trim() === preset.value}
                on:click={() => selectPreset(preset.value)}
                type="button"
              >
                {preset.label}
              </button>
            {/each}
          </div>
          <span class="cron-hint">Format: minute hour day-of-month month day-of-week &mdash; e.g. <code>* 7,11,15 * * *</code> = every minute at 7, 11, 15</span>
        </div>

        {#if error}
          <div class="error-msg">{error}</div>
        {/if}
      </div>

      <div class="modal-footer">
        <button class="btn-cancel" on:click={handleClose}>Cancel</button>
        <button class="btn-confirm" on:click={handleConfirm} disabled={saving}>
          {saving ? 'Saving...' : isEdit ? 'Save' : 'Confirm'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(6px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 9999;
    animation: fadeIn 0.15s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes slideUp {
    from { opacity: 0; transform: translateY(16px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  .modal {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 20px;
    width: 520px;
    max-width: 90vw;
    max-height: 85vh;
    overflow-y: auto;
    box-shadow: 0 24px 80px rgba(0, 0, 0, 0.5), 0 0 0 1px rgba(255, 255, 255, 0.04);
    animation: slideUp 0.2s ease;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 24px 28px 0;
  }

  .modal-header h2 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: none;
    border: none;
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .close-btn:hover {
    background: var(--bg-hover);
    color: var(--text-secondary);
  }

  .modal-body {
    padding: 20px 28px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
  }

  .required {
    color: #ef4444;
    margin-left: 1px;
  }

  .input-wrap {
    position: relative;
  }

  .form-input {
    width: 100%;
    padding: 10px 50px 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text-primary);
    font-size: 14px;
    font-family: inherit;
    outline: none;
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
  }
  .form-input:focus {
    border-color: var(--border-focus);
    box-shadow: 0 0 0 3px rgba(45, 212, 191, 0.08);
  }
  .form-input::placeholder {
    color: var(--text-muted);
  }

  .char-count {
    position: absolute;
    right: 12px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 11px;
    color: var(--text-muted);
    pointer-events: none;
  }

  .textarea-wrap {
    position: relative;
  }

  .form-textarea {
    width: 100%;
    padding: 12px 14px 28px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text-primary);
    font-size: 14px;
    font-family: inherit;
    outline: none;
    resize: vertical;
    min-height: 120px;
    max-height: 280px;
    line-height: 1.5;
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
  }
  .form-textarea:focus {
    border-color: var(--border-focus);
    box-shadow: 0 0 0 3px rgba(45, 212, 191, 0.08);
  }
  .form-textarea::placeholder {
    color: var(--text-muted);
  }

  .textarea-count {
    position: absolute;
    right: 12px;
    bottom: 8px;
    top: auto;
    transform: none;
  }

  /* ── Cron input ── */
  .cron-input {
    font-family: 'SF Mono', 'Menlo', 'Consolas', monospace;
    font-size: 14px;
    letter-spacing: 0.5px;
    padding: 10px 14px !important;
    position: relative;
    z-index: 1;
  }

  .preset-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .preset-chip {
    padding: 5px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 20px;
    color: var(--text-muted);
    font-size: 12px;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.15s ease;
    white-space: nowrap;
  }
  .preset-chip:hover {
    background: var(--bg-hover);
    color: var(--text-secondary);
    border-color: var(--text-muted);
  }
  .preset-chip.active {
    background: var(--accent-bg);
    border-color: rgba(45, 212, 191, 0.25);
    color: var(--accent);
  }

  .cron-hint {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: -2px;
  }

  .presets-container {
    position: relative;
    flex-shrink: 0;
  }

  .btn-presets {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text-secondary);
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.15s ease;
    white-space: nowrap;
  }
  .btn-presets:hover, .btn-presets.active {
    background: var(--bg-hover);
    color: var(--text-primary);
    border-color: var(--border-focus);
  }

  .presets-dropdown {
    position: absolute;
    bottom: calc(100% + 6px);
    right: 0;
    min-width: 280px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.4);
    z-index: 10;
    padding: 4px;
    animation: fadeIn 0.12s ease;
  }

  .preset-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    padding: 9px 12px;
    background: none;
    border: none;
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 0.1s ease;
  }
  .preset-item:hover {
    background: var(--bg-hover);
  }

  .preset-label {
    flex: 1;
  }

  .preset-value {
    font-family: 'SF Mono', 'Menlo', 'Consolas', monospace;
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-card);
    padding: 2px 6px;
    border-radius: 4px;
  }

  .error-msg {
    font-size: 13px;
    color: #ef4444;
    padding: 8px 12px;
    background: rgba(239, 68, 68, 0.08);
    border-radius: 8px;
  }

  .modal-footer {
    padding: 0 28px 24px;
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .btn-cancel,
  .btn-confirm {
    padding: 9px 22px;
    border-radius: 10px;
    font-size: 14px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.15s ease;
    border: none;
  }

  .btn-cancel {
    background: var(--bg-card);
    color: var(--text-secondary);
    border: 1px solid var(--border);
  }
  .btn-cancel:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .btn-confirm {
    background: var(--accent-bg);
    border: 1px solid rgba(45, 212, 191, 0.15);
    color: var(--accent);
    font-weight: 600;
  }
  .btn-confirm:hover:not(:disabled) {
    background: rgba(45, 212, 191, 0.15);
    border-color: rgba(45, 212, 191, 0.3);
    transform: translateY(-1px);
  }
  .btn-confirm:active:not(:disabled) {
    transform: translateY(0);
  }
  .btn-confirm:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
