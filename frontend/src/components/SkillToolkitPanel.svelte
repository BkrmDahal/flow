<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import {
    skills,
    activeSection,
    activeItemId,
    activeItemDetail,
    detailLoading,
    refreshSkills,
    switchSection,
    selectSkill,
    addSkill,
    updateSkill,
    deleteSkill,
    clearPluginSelection,
    masterPrompt,
    masterPromptLoading,
    loadMasterPrompt,
    saveMasterPrompt,
    coworkPrompt,
    coworkPromptLoading,
    loadCoworkPrompt,
    saveCoworkPrompt,
  } from '../lib/stores/pluginsStore.js';

  const dispatch = createEventDispatcher();

  let editing = false;
  let editName = '';
  let editDescription = '';
  let editBody = '';
  let editError = '';
  let saving = false;

  let adding = false;
  let addName = '';
  let addDescription = '';
  let addBody = '';
  let addError = '';
  let addSaving = false;

  let searchQuery = '';
  let hoveredItemId = null;

  // Master Prompt Edit State
  let editPromptBody = '';
  let promptSaving = false;
  let promptError = '';
  let promptSuccess = false;

  onMount(async () => {
    if ($activeSection === 'prompt') {
      await handleSwitchSection('prompt');
    } else if ($activeSection === 'cowork_prompt') {
      await handleSwitchSection('cowork_prompt');
    } else {
      await handleSwitchSection('skills');
    }
  });

  $: filteredItems = searchQuery
    ? $skills.filter(i =>
        i.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        (i.description || '').toLowerCase().includes(searchQuery.toLowerCase()))
    : $skills;
  $: detail = $activeSection === 'skills' ? $activeItemDetail : null;

  // Sync editPromptBody when prompts update in store
  $: if ($masterPrompt !== undefined && !promptSaving && $activeSection === 'prompt') {
    editPromptBody = $masterPrompt;
  }
  $: if ($coworkPrompt !== undefined && !promptSaving && $activeSection === 'cowork_prompt') {
    editPromptBody = $coworkPrompt;
  }

  async function handleSwitchSection(section) {
    adding = false;
    editing = false;
    promptError = '';
    promptSuccess = false;
    switchSection(section);
    if (section === 'skills') {
      await refreshSkills();
    } else if (section === 'prompt') {
      await loadMasterPrompt();
      editPromptBody = $masterPrompt || '';
    } else if (section === 'cowork_prompt') {
      await loadCoworkPrompt();
      editPromptBody = $coworkPrompt || '';
    }
  }

  async function handleSavePrompt() {
    promptError = '';
    promptSuccess = false;
    promptSaving = true;
    try {
      if ($activeSection === 'prompt') {
        await saveMasterPrompt(editPromptBody);
      } else if ($activeSection === 'cowork_prompt') {
        await saveCoworkPrompt(editPromptBody);
      }
      promptSuccess = true;
      setTimeout(() => {
        promptSuccess = false;
      }, 3000);
    } catch (e) {
      promptError = e?.message || 'Failed to save prompt.';
    } finally {
      promptSaving = false;
    }
  }

  function formatDate(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
  }

  function handleSelectItem(id) {
    adding = false;
    editing = false;
    selectSkill(id);
  }

  function handleStartAdd() {
    clearPluginSelection();
    addName = '';
    addDescription = '';
    addBody = '# New Skill\n\nDescribe when Cowork should use this skill and the instructions it should follow.\n';
    addError = '';
    adding = true;
    editing = false;
  }

  function handleCancelAdd() {
    adding = false;
    addError = '';
  }

  async function handleSaveAdd() {
    if (!addName.trim()) {
      addError = 'Name is required.';
      return;
    }

    addError = '';
    addSaving = true;
    try {
      const result = await addSkill(addName, addDescription, addBody);
      adding = false;
      if (result?.id) handleSelectItem(result.id);
    } catch (e) {
      addError = e?.message || 'Failed to create skill.';
    } finally {
      addSaving = false;
    }
  }

  function handleStartEdit() {
    if (!detail) return;
    editName = detail.name || '';
    editDescription = detail.description || '';
    editBody = detail.body || '';
    editError = '';
    editing = true;
  }

  function handleCancelEdit() {
    editing = false;
    editError = '';
  }

  async function handleSaveEdit() {
    if (!editName.trim()) {
      editError = 'Name is required.';
      return;
    }

    editError = '';
    saving = true;
    try {
      await updateSkill($activeItemId, editName, editDescription, editBody);
      editing = false;
    } catch (e) {
      editError = e?.message || 'Failed to save skill.';
    } finally {
      saving = false;
    }
  }

  async function handleDelete(id) {
    await deleteSkill(id);
    adding = false;
    editing = false;
  }
</script>

<div class="toolkit-panel">
  <div class="toolkit-content">
    <div class="section-nav">
      <div class="section-nav-top">
        <button class="back-btn" on:click={() => dispatch('close')} title="Back">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5M12 19l-7-7 7-7"/></svg>
        </button>
        <div class="section-nav-header">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="7" width="20" height="14" rx="2" ry="2"/><path d="M16 7V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v2"/></svg>
          <span>Toolkit</span>
        </div>
      </div>

      <div class="section-nav-label">Cowork</div>
      <button class="section-btn" class:active={$activeSection === 'skills'} on:click={() => handleSwitchSection('skills')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
        Skills
        {#if $skills.length > 0}<span class="section-count">{$skills.length}</span>{/if}
      </button>
      <button class="section-btn" class:active={$activeSection === 'cowork_prompt'} on:click={() => handleSwitchSection('cowork_prompt')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
        Cowork Prompt
      </button>
      <button class="section-btn" class:active={$activeSection === 'prompt'} on:click={() => handleSwitchSection('prompt')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
        Master Prompt
      </button>
    </div>

    {#if $activeSection === 'skills'}
      <div class="item-list">
        <div class="item-list-header">
          <span class="item-list-title">Skills</span>
          <button class="btn-add-item" on:click={handleStartAdd} title="Add skill">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
          </button>
        </div>
        <div class="item-search">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          <input type="text" placeholder="Search skills..." bind:value={searchQuery} />
        </div>
        <div class="item-entries">
          {#if filteredItems.length === 0}
            <div class="empty-items"><p>No skills yet</p><span>Click + to add one.</span></div>
          {:else}
            {#each filteredItems as item (item.id)}
              <button
                class="item-entry"
                class:active={item.id === $activeItemId && !adding}
                on:click={() => handleSelectItem(item.id)}
                on:mouseenter={() => hoveredItemId = item.id}
                on:mouseleave={() => hoveredItemId = null}
              >
                <span class="item-entry-name">{item.name}</span>
                <button
                  class="item-delete-btn"
                  class:visible={hoveredItemId === item.id}
                  on:click|stopPropagation={() => handleDelete(item.id)}
                  title="Delete skill"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/></svg>
                </button>
              </button>
            {/each}
          {/if}
        </div>
      </div>
    {/if}

    <div class="detail-panel">
      {#if $activeSection === 'prompt' || $activeSection === 'cowork_prompt'}
        <div class="detail-header">
          <div class="detail-header-top">
            <h2 class="detail-title">{$activeSection === 'prompt' ? 'Master Prompt' : 'Cowork Prompt'}</h2>
            <div class="prompt-badge">System Prompt</div>
          </div>
          <p class="detail-description">
            {#if $activeSection === 'prompt'}
              The global instructions that define Flow's core personality, capabilities, rules, and task planning strategies. Changes here directly affect all subsequent turns and agent interactions.
            {:else}
              The system instructions that guide the Cowork assistant during conversational chat and coding sessions. These define its tone, style, formatting, and behavior.
            {/if}
          </p>
        </div>

        {#if $activeSection === 'prompt' ? $masterPromptLoading : $coworkPromptLoading}
          <div class="detail-loading"><div class="spinner"></div></div>
        {:else}
          <div class="edit-form prompt-editor-form">
            <div class="edit-field edit-field-body">
              <label for="prompt-textarea">System Prompt (Markdown)</label>
              <textarea
                id="prompt-textarea"
                bind:value={editPromptBody}
                placeholder="# System Instructions..."
                disabled={promptSaving}
              ></textarea>
            </div>
            {#if promptError}<div class="edit-error">{promptError}</div>{/if}
            {#if promptSuccess}<div class="edit-success">System prompt saved successfully!</div>{/if}
            <div class="edit-actions">
              <button
                class="btn-cancel"
                on:click={() => { editPromptBody = $activeSection === 'prompt' ? $masterPrompt : $coworkPrompt; promptError = ''; promptSuccess = false; }}
                disabled={promptSaving || editPromptBody === ($activeSection === 'prompt' ? $masterPrompt : $coworkPrompt)}
              >
                Reset
              </button>
              <button
                class="btn-save"
                on:click={handleSavePrompt}
                disabled={promptSaving || editPromptBody === ($activeSection === 'prompt' ? $masterPrompt : $coworkPrompt)}
              >
                {promptSaving ? 'Saving...' : 'Save Prompt'}
              </button>
            </div>
          </div>
        {/if}
      {:else}
        {#if adding}
          <div class="detail-header"><h2 class="detail-title">New Skill</h2></div>
          <div class="edit-form">
            <div class="edit-field">
              <label for="skill-add-name">Name</label>
              <input id="skill-add-name" type="text" bind:value={addName} placeholder="e.g. code-review" disabled={addSaving} />
              <span class="field-hint">No spaces. Use hyphens or underscores.</span>
            </div>
            <div class="edit-field">
              <label for="skill-add-desc">Description</label>
              <input id="skill-add-desc" type="text" bind:value={addDescription} placeholder="When Cowork should use this skill..." disabled={addSaving} />
            </div>
            <div class="edit-field edit-field-body">
              <label for="skill-add-body">Content (Markdown)</label>
              <textarea id="skill-add-body" bind:value={addBody} placeholder="# Instructions..." disabled={addSaving}></textarea>
            </div>
            {#if addError}<div class="edit-error">{addError}</div>{/if}
            <div class="edit-actions">
              <button class="btn-cancel" on:click={handleCancelAdd}>Cancel</button>
              <button class="btn-save" on:click={handleSaveAdd} disabled={addSaving || !addName.trim()}>{addSaving ? 'Saving...' : 'Create'}</button>
            </div>
          </div>
        {:else if $detailLoading}
          <div class="detail-loading"><div class="spinner"></div></div>
        {:else if detail}
          <div class="detail-header">
            <div class="detail-header-top">
              <h2 class="detail-title">{detail.name}</h2>
              {#if !editing}<button class="btn-edit" on:click={handleStartEdit}>Edit</button>{/if}
            </div>
            {#if !editing}
              <div class="detail-meta">
                <span class="meta-item"><span class="meta-label">Added</span><span class="meta-value">{formatDate(detail.createdAt)}</span></span>
                {#if detail.updatedAt !== detail.createdAt}
                  <span class="meta-item"><span class="meta-label">Updated</span><span class="meta-value">{formatDate(detail.updatedAt)}</span></span>
                {/if}
              </div>
              {#if detail.description}<p class="detail-description">{detail.description}</p>{/if}
            {/if}
          </div>

          {#if editing}
            <div class="edit-form">
              <div class="edit-field">
                <label for="skill-edit-name">Name</label>
                <input id="skill-edit-name" type="text" bind:value={editName} disabled={saving} />
                <span class="field-hint">No spaces. Use hyphens or underscores.</span>
              </div>
              <div class="edit-field">
                <label for="skill-edit-desc">Description</label>
                <input id="skill-edit-desc" type="text" bind:value={editDescription} disabled={saving} />
              </div>
              <div class="edit-field edit-field-body">
                <label for="skill-edit-body">Content (Markdown)</label>
                <textarea id="skill-edit-body" bind:value={editBody} disabled={saving}></textarea>
              </div>
              {#if editError}<div class="edit-error">{editError}</div>{/if}
              <div class="edit-actions">
                <button class="btn-cancel" on:click={handleCancelEdit}>Cancel</button>
                <button class="btn-save" on:click={handleSaveEdit} disabled={saving || !editName.trim()}>{saving ? 'Saving...' : 'Save'}</button>
              </div>
            </div>
          {:else}
            <div class="detail-body"><pre class="detail-body-content">{detail.body || '(empty)'}</pre></div>
          {/if}
        {:else}
          <div class="detail-empty">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" opacity="0.22"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
            <p>Select a skill to view details</p>
            <span>or click + to create a new one</span>
          </div>
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  .toolkit-panel { flex: 1; display: flex; flex-direction: column; overflow: hidden; background: var(--bg-app); }
  .toolkit-content { flex: 1; display: flex; overflow: hidden; min-height: 0; }
  .section-nav { --wails-draggable: drag; width: 180px; flex-shrink: 0; display: flex; flex-direction: column; border-right: 1px solid var(--border); padding: 38px 10px 16px; gap: 2px; background: var(--bg-sidebar); }
  .section-nav-top { --wails-draggable: no-drag; display: flex; align-items: center; gap: 4px; padding: 0 4px 8px; }
  .back-btn { display: flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; background: none; border: none; border-radius: 7px; color: var(--text-muted); cursor: pointer; transition: all 0.15s ease; flex-shrink: 0; }
  .back-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .section-nav-header { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .section-nav-label { font-size: 10px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.8px; padding: 8px 10px 4px; }
  .section-btn { --wails-draggable: no-drag; display: flex; align-items: center; gap: 8px; width: 100%; padding: 7px 10px; background: none; border: none; border-radius: 7px; color: var(--text-secondary); font-size: 13px; font-family: var(--font-sans); cursor: pointer; transition: all 0.15s ease; text-align: left; }
  .section-btn.active { background: rgba(255,255,255,0.08); color: var(--text-primary); }
  .section-count { margin-left: auto; font-size: 11px; color: var(--text-muted); background: var(--bg-card); padding: 1px 6px; border-radius: 8px; }
  .item-list { width: 230px; flex-shrink: 0; display: flex; flex-direction: column; border-right: 1px solid var(--border); overflow: hidden; padding-top: 38px; background: var(--bg-app); }
  .item-list-header { display: flex; align-items: center; justify-content: space-between; padding: 6px 14px 8px; }
  .item-list-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .btn-add-item { display: flex; align-items: center; justify-content: center; width: 26px; height: 26px; background: none; border: 1px solid var(--border); border-radius: 6px; color: var(--text-secondary); cursor: pointer; transition: all 0.15s ease; }
  .btn-add-item:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--text-muted); }
  .item-search { display: flex; align-items: center; gap: 6px; padding: 4px 14px 8px; }
  .item-search svg { flex-shrink: 0; color: var(--text-muted); }
  .item-search input { flex: 1; min-width: 0; background: none; border: none; outline: none; color: var(--text-secondary); font-size: 12.5px; font-family: var(--font-sans); padding: 4px 0; }
  .item-search input::placeholder { color: var(--text-muted); }
  .item-entries { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 1px; padding: 0 6px; }
  .empty-items { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 32px 16px; text-align: center; }
  .empty-items p { font-size: 13px; font-weight: 500; color: var(--text-secondary); margin: 0 0 4px; }
  .empty-items span { font-size: 12px; color: var(--text-muted); }
  .item-entry { display: flex; align-items: center; width: 100%; padding: 7px 10px; background: none; border: none; border-radius: 6px; color: var(--text-secondary); font-size: 13px; font-family: var(--font-sans); cursor: pointer; transition: all 0.12s ease; text-align: left; position: relative; }
  .item-entry:hover { background: var(--bg-hover); color: var(--text-primary); }
  .item-entry.active { background: rgba(255,255,255,0.08); color: var(--text-primary); }
  .item-entry-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .item-delete-btn { flex-shrink: 0; display: flex; align-items: center; justify-content: center; width: 24px; height: 24px; padding: 0; background: none; border: none; border-radius: 4px; color: var(--text-muted); cursor: pointer; transition: all 0.15s ease; opacity: 0; pointer-events: none; }
  .item-delete-btn.visible { opacity: 1; pointer-events: auto; }
  .item-delete-btn:hover { background: var(--danger-bg); color: var(--danger); }
  .detail-panel { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-width: 0; position: relative; padding-top: 38px; background: var(--bg-app); }
  .detail-header { padding: 20px 24px 0; flex-shrink: 0; }
  .detail-header-top { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .detail-title { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
  .detail-meta { display: flex; gap: 16px; margin-top: 8px; }
  .meta-item { display: flex; gap: 6px; font-size: 12px; }
  .meta-label { color: var(--text-muted); }
  .meta-value { color: var(--text-secondary); }
  .detail-description { font-size: 13px; color: var(--text-secondary); line-height: 1.5; margin: 10px 0 0; padding: 10px 14px; background: var(--bg-card); border-radius: 8px; border: 1px solid var(--border); }
  .btn-edit { padding: 6px 16px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; color: var(--text-secondary); font-size: 13px; font-weight: 500; font-family: var(--font-sans); cursor: pointer; transition: all 0.15s ease; flex-shrink: 0; }
  .btn-edit:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--text-muted); }
  .detail-body { flex: 1; overflow-y: auto; padding: 16px 24px 24px; }
  .detail-body-content { font-size: 13px; color: var(--text-secondary); line-height: 1.7; font-family: var(--font-mono, 'SF Mono', 'Fira Code', monospace); white-space: pre-wrap; word-wrap: break-word; margin: 0; padding: 16px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; }
  .edit-form { flex: 1; display: flex; flex-direction: column; padding: 16px 24px 24px; gap: 12px; overflow-y: auto; }
  .edit-field { display: flex; flex-direction: column; gap: 4px; }
  .edit-field label { font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; }
  .edit-field input { padding: 8px 12px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 8px; color: var(--text-primary); font-size: 13px; font-family: var(--font-sans); outline: none; transition: border-color 0.15s ease; }
  .edit-field input:focus { border-color: var(--border-focus); box-shadow: 0 0 0 2px var(--accent-bg); }
  .edit-field input::placeholder { color: var(--text-muted); }
  .edit-field-body { flex: 1; min-height: 220px; display: flex; flex-direction: column; }
  .edit-field textarea { flex: 1; padding: 12px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 8px; color: var(--text-primary); font-size: 13px; font-family: var(--font-mono, 'SF Mono', 'Fira Code', monospace); line-height: 1.6; outline: none; resize: none; transition: border-color 0.15s ease; min-height: 220px; }
  .edit-field textarea:focus { border-color: var(--border-focus); box-shadow: 0 0 0 2px var(--accent-bg); }
  .edit-field textarea::placeholder { color: var(--text-muted); }
  .field-hint { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
  .edit-error { font-size: 12px; color: #f87171; }
  .edit-actions { display: flex; justify-content: flex-end; gap: 8px; flex-shrink: 0; }
  .btn-cancel { padding: 7px 16px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; color: var(--text-secondary); font-size: 13px; font-family: var(--font-sans); cursor: pointer; transition: all 0.15s ease; }
  .btn-cancel:hover { background: var(--bg-hover); color: var(--text-primary); }
  .btn-save { padding: 7px 20px; background: var(--accent); border: none; border-radius: 8px; color: #06201d; font-size: 13px; font-weight: 600; font-family: var(--font-sans); cursor: pointer; transition: all 0.15s ease; }
  .btn-save:hover:not(:disabled) { background: var(--accent-dim); }
  .btn-save:disabled { opacity: 0.4; cursor: not-allowed; }
  .detail-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; color: var(--text-muted); text-align: center; gap: 4px; }
  .detail-empty p { margin: 12px 0 0; font-size: 14px; font-weight: 500; color: var(--text-secondary); }
  .detail-empty span { font-size: 13px; color: var(--text-muted); }
  .detail-loading { flex: 1; display: flex; align-items: center; justify-content: center; }
  .spinner { width: 20px; height: 20px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ─── Master Prompt Editor Styling ─── */
  .prompt-badge {
    padding: 3px 8px;
    background: var(--accent-bg);
    color: var(--accent);
    border: 1px solid rgba(45, 212, 191, 0.2);
    border-radius: var(--radius-sm);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .edit-success {
    font-size: 12.5px;
    color: #34d399;
    background: rgba(52, 211, 153, 0.1);
    border: 1px solid rgba(52, 211, 153, 0.2);
    padding: 8px 12px;
    border-radius: var(--radius-md);
  }
</style>
