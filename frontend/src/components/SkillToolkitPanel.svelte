<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import {
    skills,
    snippets,
    activeSection,
    activeItemId,
    activeItemDetail,
    detailLoading,
    refreshSkills,
    refreshSnippets,
    switchSection,
    selectSkill,
    selectSnippet,
    addSkill,
    addSnippet,
    updateSkill,
    updateSnippet,
    deleteSkill,
    deleteSnippet,
    clearPluginSelection,
    masterPrompt,
    masterPromptLoading,
    loadMasterPrompt,
    saveMasterPrompt,
    coworkPrompt,
    coworkPromptLoading,
    loadCoworkPrompt,
    saveCoworkPrompt,
    memoryFiles,
    activeMemoryName,
    activeMemoryDetail,
    memoryDetailLoading,
    refreshMemoryFiles,
    selectMemoryFile,
    addMemoryFile,
    saveMemoryFile,
    deleteMemoryFile,
    clearMemorySelection,
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

  // Snippets Edit/Add State
  let addSnippetTrigger = '';
  let addSnippetExpansion = '';
  let editSnippetTrigger = '';
  let editSnippetExpansion = '';

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
    } else if ($activeSection === 'snippets') {
      await handleSwitchSection('snippets');
    } else if ($activeSection === 'memory') {
      await handleSwitchSection('memory');
    } else {
      await handleSwitchSection('skills');
    }
  });

  $: filteredItems = $activeSection === 'skills'
    ? (searchQuery
        ? $skills.filter(i =>
            i.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
            (i.description || '').toLowerCase().includes(searchQuery.toLowerCase()))
        : $skills)
    : ($activeSection === 'snippets'
        ? (searchQuery
            ? $snippets.filter(i =>
                i.trigger.toLowerCase().includes(searchQuery.toLowerCase()) ||
                i.expansion.toLowerCase().includes(searchQuery.toLowerCase()))
            : $snippets)
        : ($activeSection === 'memory'
            ? (searchQuery
                ? $memoryFiles.filter(i =>
                    i.name.toLowerCase().includes(searchQuery.toLowerCase()))
                : $memoryFiles)
            : []));

  $: detail = ($activeSection === 'skills' || $activeSection === 'snippets')
    ? $activeItemDetail
    : ($activeSection === 'memory' ? $activeMemoryDetail : null);

  $: isLoading = $activeSection === 'memory' ? $memoryDetailLoading : $detailLoading;

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
    } else if (section === 'snippets') {
      await refreshSnippets();
    } else if (section === 'memory') {
      await refreshMemoryFiles();
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
    if ($activeSection === 'skills') {
      selectSkill(id);
    } else if ($activeSection === 'snippets') {
      selectSnippet(id);
    } else if ($activeSection === 'memory') {
      selectMemoryFile(id);
    }
  }

  function handleStartAdd() {
    clearPluginSelection();
    clearMemorySelection();
    addError = '';
    adding = true;
    editing = false;

    if ($activeSection === 'skills') {
      addName = '';
      addDescription = '';
      addBody = '# New Skill\n\nDescribe when Cowork should use this skill and the instructions it should follow.\n';
    } else if ($activeSection === 'snippets') {
      addSnippetTrigger = '';
      addSnippetExpansion = '';
    } else if ($activeSection === 'memory') {
      addName = '';
      addBody = '# Memory\n\nDescribe the facts or info to remember.\n';
    }
  }

  function handleCancelAdd() {
    adding = false;
    addError = '';
  }

  async function handleSaveAdd() {
    addError = '';
    addSaving = true;

    if ($activeSection === 'skills') {
      if (!addName.trim()) {
        addError = 'Name is required.';
        addSaving = false;
        return;
      }
      try {
        const result = await addSkill(addName, addDescription, addBody);
        adding = false;
        if (result?.id) handleSelectItem(result.id);
      } catch (e) {
        addError = e?.message || 'Failed to create skill.';
      } finally {
        addSaving = false;
      }
    } else if ($activeSection === 'snippets') {
      if (!addSnippetTrigger.trim()) {
        addError = 'Trigger is required.';
        addSaving = false;
        return;
      }
      if (!addSnippetExpansion.trim()) {
        addError = 'Expansion is required.';
        addSaving = false;
        return;
      }
      try {
        const result = await addSnippet(addSnippetTrigger, addSnippetExpansion);
        adding = false;
        if (result?.id) handleSelectItem(result.id);
      } catch (e) {
        addError = e?.message || 'Failed to create snippet.';
      } finally {
        addSaving = false;
      }
    } else if ($activeSection === 'memory') {
      if (!addName.trim()) {
        addError = 'Name is required.';
        addSaving = false;
        return;
      }
      try {
        const result = await addMemoryFile(addName, addBody);
        adding = false;
        if (result?.name) handleSelectItem(result.name);
      } catch (e) {
        addError = e?.message || 'Failed to create memory.';
      } finally {
        addSaving = false;
      }
    }
  }

  function handleStartEdit() {
    if (!detail) return;
    addError = '';
    editError = '';
    editing = true;

    if ($activeSection === 'skills') {
      editName = detail.name || '';
      editDescription = detail.description || '';
      editBody = detail.body || '';
    } else if ($activeSection === 'snippets') {
      editSnippetTrigger = detail.trigger || '';
      editSnippetExpansion = detail.expansion || '';
    } else if ($activeSection === 'memory') {
      editName = detail.name || '';
      editBody = detail.body || '';
    }
  }

  function handleCancelEdit() {
    editing = false;
    editError = '';
  }

  async function handleSaveEdit() {
    editError = '';
    saving = true;

    if ($activeSection === 'skills') {
      if (!editName.trim()) {
        editError = 'Name is required.';
        saving = false;
        return;
      }
      try {
        await updateSkill($activeItemId, editName, editDescription, editBody);
        editing = false;
      } catch (e) {
        editError = e?.message || 'Failed to save skill.';
      } finally {
        saving = false;
      }
    } else if ($activeSection === 'snippets') {
      if (!editSnippetTrigger.trim()) {
        editError = 'Trigger is required.';
        saving = false;
        return;
      }
      if (!editSnippetExpansion.trim()) {
        editError = 'Expansion is required.';
        saving = false;
        return;
      }
      try {
        await updateSnippet($activeItemId, editSnippetTrigger, editSnippetExpansion);
        editing = false;
      } catch (e) {
        editError = e?.message || 'Failed to save snippet.';
      } finally {
        saving = false;
      }
    } else if ($activeSection === 'memory') {
      try {
        await saveMemoryFile($activeMemoryName, editBody);
        editing = false;
      } catch (e) {
        editError = e?.message || 'Failed to save memory.';
      } finally {
        saving = false;
      }
    }
  }

  async function handleDelete(id) {
    adding = false;
    editing = false;
    if ($activeSection === 'skills') {
      await deleteSkill(id);
    } else if ($activeSection === 'snippets') {
      await deleteSnippet(id);
    } else if ($activeSection === 'memory') {
      await deleteMemoryFile(id);
    }
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
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="4" y1="21" x2="4" y2="14" /><line x1="4" y1="10" x2="4" y2="3" />
            <line x1="12" y1="21" x2="12" y2="12" /><line x1="12" y1="8" x2="12" y2="3" />
            <line x1="20" y1="21" x2="20" y2="16" /><line x1="20" y1="12" x2="20" y2="3" />
            <line x1="1" y1="14" x2="7" y2="14" /><line x1="9" y1="8" x2="15" y2="8" /><line x1="17" y1="16" x2="23" y2="16" />
          </svg>
          <span>Customize</span>
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

      <div class="section-nav-label">Flow</div>
      <button class="section-btn" class:active={$activeSection === 'snippets'} on:click={() => handleSwitchSection('snippets')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9.59 4.59A2 2 0 1 1 11 8H2m10.59 11.41A2 2 0 1 0 14 16H2m18-6H8m12 4H8m-2-8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 12a2 2 0 1 0 0-4 2 2 0 0 0 0 4z"/></svg>
        Snippets
        {#if $snippets.length > 0}<span class="section-count">{$snippets.length}</span>{/if}
      </button>

      <div class="section-nav-label">System</div>
      <button class="section-btn" class:active={$activeSection === 'prompt'} on:click={() => handleSwitchSection('prompt')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
        Master Prompt
      </button>
      <button class="section-btn" class:active={$activeSection === 'memory'} on:click={() => handleSwitchSection('memory')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <ellipse cx="12" cy="5" rx="9" ry="3"></ellipse>
          <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>
          <path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3"></path>
        </svg>
        Memory
        {#if $memoryFiles.length > 0}<span class="section-count">{$memoryFiles.length}</span>{/if}
      </button>
    </div>

    {#if $activeSection === 'skills' || $activeSection === 'snippets' || $activeSection === 'memory'}
      <div class="item-list">
        <div class="item-list-header">
          <span class="item-list-title">
            {#if $activeSection === 'skills'}
              Skills
            {:else if $activeSection === 'snippets'}
              Snippets
            {:else}
              Memory
            {/if}
          </span>
          <button class="btn-add-item" on:click={handleStartAdd} title={$activeSection === 'skills' ? 'Add skill' : ($activeSection === 'snippets' ? 'Add snippet' : 'Add memory')}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
          </button>
        </div>
        <div class="item-search">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          <input type="text" placeholder={$activeSection === 'skills' ? 'Search skills...' : ($activeSection === 'snippets' ? 'Search triggers...' : 'Search memory...')} bind:value={searchQuery} />
        </div>
        <div class="item-entries">
          {#if filteredItems.length === 0}
            <div class="empty-items">
              <p>No {$activeSection === 'skills' ? 'skills' : ($activeSection === 'snippets' ? 'snippets' : 'memories')} yet</p>
              <span>Click + to add one.</span>
            </div>
          {:else}
            {#each filteredItems as item (item.id || item.name)}
              <button
                class="item-entry"
                class:active={(item.id === $activeItemId || item.name === $activeMemoryName) && !adding}
                on:click={() => handleSelectItem(item.id || item.name)}
                on:mouseenter={() => hoveredItemId = item.id || item.name}
                on:mouseleave={() => hoveredItemId = null}
              >
                <span class="item-entry-name">
                  {#if $activeSection === 'skills'}
                    {item.name}
                  {:else if $activeSection === 'snippets'}
                    <code style="background: rgba(255,255,255,0.06); padding: 2px 6px; border-radius: 4px; font-family: var(--font-mono); margin-right: 4px;">{item.trigger}</code>
                    <span style="opacity: 0.5; font-size: 11px;">&rarr; {item.expansion}</span>
                  {:else}
                    {item.name}
                  {/if}
                </span>
                <button
                  class="item-delete-btn"
                  class:visible={hoveredItemId === (item.id || item.name)}
                  on:click|stopPropagation={() => handleDelete(item.id || item.name)}
                  title={$activeSection === 'skills' ? 'Delete skill' : ($activeSection === 'snippets' ? 'Delete snippet' : 'Delete memory')}
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
          {#if $activeSection === 'skills'}
            <div class="detail-header">
              <div class="detail-header-top">
                <h2 class="detail-title">New Skill</h2>
              </div>
            </div>
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
          {:else if $activeSection === 'memory'}
            <div class="detail-header">
              <div class="detail-header-top">
                <h2 class="detail-title">New Memory</h2>
              </div>
            </div>
            <div class="edit-form">
              <div class="edit-field">
                <label for="memory-add-name">Name</label>
                <input id="memory-add-name" type="text" bind:value={addName} placeholder="e.g. user_preferences" disabled={addSaving} />
                <span class="field-hint">No spaces. Use hyphens or underscores. Stored as markdown.</span>
              </div>
              <div class="edit-field edit-field-body">
                <label for="memory-add-body">Content (Markdown)</label>
                <textarea id="memory-add-body" bind:value={addBody} placeholder="# Memory Content..." disabled={addSaving}></textarea>
              </div>
              {#if addError}<div class="edit-error">{addError}</div>{/if}
              <div class="edit-actions">
                <button class="btn-cancel" on:click={handleCancelAdd}>Cancel</button>
                <button class="btn-save" on:click={handleSaveAdd} disabled={addSaving || !addName.trim()}>{addSaving ? 'Saving...' : 'Create'}</button>
              </div>
            </div>
          {:else}
            <div class="detail-header">
              <div class="detail-header-top">
                <h2 class="detail-title">New Snippet</h2>
              </div>
            </div>
            <div class="edit-form">
              <div class="edit-field">
                <label for="snippet-add-trigger">Trigger abbreviation</label>
                <input id="snippet-add-trigger" type="text" bind:value={addSnippetTrigger} placeholder="e.g. btw" disabled={addSaving} />
                <span class="field-hint">The text to look for in your transcript (case-insensitive).</span>
              </div>
              <div class="edit-field edit-field-body">
                <label for="snippet-add-expansion">Expanded text replacement</label>
                <textarea id="snippet-add-expansion" bind:value={addSnippetExpansion} placeholder="e.g. by the way" disabled={addSaving} style="min-height: 120px; flex: initial; height: 120px;"></textarea>
              </div>
              {#if addError}<div class="edit-error">{addError}</div>{/if}
              <div class="edit-actions">
                <button class="btn-cancel" on:click={handleCancelAdd}>Cancel</button>
                <button class="btn-save" on:click={handleSaveAdd} disabled={addSaving || !addSnippetTrigger.trim()}>{addSaving ? 'Saving...' : 'Create'}</button>
              </div>
            </div>
          {/if}
        {:else if isLoading}
          <div class="detail-loading"><div class="spinner"></div></div>
        {:else if detail}
          <div class="detail-header">
            <div class="detail-header-top">
              <h2 class="detail-title">
                {#if $activeSection === 'skills'}
                  {detail.name}
                {:else if $activeSection === 'memory'}
                  {detail.name}
                {:else}
                  Snippet: {detail.trigger}
                {/if}
              </h2>
              {#if !editing}<button class="btn-edit" on:click={handleStartEdit}>Edit</button>{/if}
            </div>
            {#if !editing}
              <div class="detail-meta">
                <span class="meta-item"><span class="meta-label">Created</span><span class="meta-value">{formatDate(detail.createdAt || detail.updatedAt)}</span></span>
              </div>
              {#if $activeSection === 'skills' && detail.description}
                <p class="detail-description">{detail.description}</p>
              {/if}
            {/if}
          </div>

          {#if editing}
            {#if $activeSection === 'skills'}
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
            {:else if $activeSection === 'memory'}
              <div class="edit-form">
                <div class="edit-field">
                  <label for="memory-edit-name">Name</label>
                  <input id="memory-edit-name" type="text" bind:value={editName} disabled />
                  <span class="field-hint">Memory name cannot be changed once created.</span>
                </div>
                <div class="edit-field edit-field-body">
                  <label for="memory-edit-body">Content (Markdown)</label>
                  <textarea id="memory-edit-body" bind:value={editBody} disabled={saving}></textarea>
                </div>
                {#if editError}<div class="edit-error">{editError}</div>{/if}
                <div class="edit-actions">
                  <button class="btn-cancel" on:click={handleCancelEdit}>Cancel</button>
                  <button class="btn-save" on:click={handleSaveEdit} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
                </div>
              </div>
            {:else}
              <div class="edit-form">
                <div class="edit-field">
                  <label for="snippet-edit-trigger">Trigger abbreviation</label>
                  <input id="snippet-edit-trigger" type="text" bind:value={editSnippetTrigger} disabled={saving} />
                  <span class="field-hint">Trigger shortcut text.</span>
                </div>
                <div class="edit-field edit-field-body">
                  <label for="snippet-edit-expansion">Expanded text replacement</label>
                  <textarea id="snippet-edit-expansion" bind:value={editSnippetExpansion} disabled={saving} style="min-height: 120px; flex: initial; height: 120px;"></textarea>
                </div>
                {#if editError}<div class="edit-error">{editError}</div>{/if}
                <div class="edit-actions">
                  <button class="btn-cancel" on:click={handleCancelEdit}>Cancel</button>
                  <button class="btn-save" on:click={handleSaveEdit} disabled={saving || !editSnippetTrigger.trim()}>{saving ? 'Saving...' : 'Save'}</button>
                </div>
              </div>
            {/if}
          {:else}
            {#if $activeSection === 'skills' || $activeSection === 'memory'}
              <div class="detail-body"><pre class="detail-body-content">{detail.body || '(empty)'}</pre></div>
            {:else}
              <div class="detail-body" style="display: flex; flex-direction: column; gap: 14px;">
                <div style="background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 14px 18px;">
                  <span style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; display: block; margin-bottom: 4px;">Trigger Shortcut</span>
                  <code style="font-size: 16px; font-family: var(--font-mono); color: var(--accent); font-weight: bold; background: rgba(0,0,0,0.2); padding: 2px 8px; border-radius: 4px;">{detail.trigger}</code>
                </div>
                <div style="background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 14px 18px; flex: 1; display: flex; flex-direction: column;">
                  <span style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; display: block; margin-bottom: 6px;">Expanded Text</span>
                  <div style="font-size: 13.5px; color: var(--text-secondary); line-height: 1.65; white-space: pre-wrap; font-family: var(--font-sans); flex: 1;">{detail.expansion}</div>
                </div>
              </div>
            {/if}
          {/if}
        {:else}
          <div class="detail-empty">
            {#if $activeSection === 'skills'}
              <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" opacity="0.22"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
              <p>Select a skill to view details</p>
              <span>or click + to create a new one</span>
            {:else if $activeSection === 'snippets'}
              <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" opacity="0.22"><path d="M9.59 4.59A2 2 0 1 1 11 8H2m10.59 11.41A2 2 0 1 0 14 16H2m18-6H8m12 4H8m-2-8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 12a2 2 0 1 0 0-4 2 2 0 0 0 0 4z"/></svg>
              <p>Select a snippet to view details</p>
              <span>or click + to create a new one</span>
            {:else if $activeSection === 'memory'}
              <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" opacity="0.22">
                <ellipse cx="12" cy="5" rx="9" ry="3"></ellipse>
                <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>
                <path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3"></path>
              </svg>
              <p>Select a memory to view details</p>
              <span>or click + to create a new one</span>
            {/if}
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
  .section-nav-top { --wails-draggable: no-drag; display: flex; align-items: center; gap: 4px; padding: 0 4px 8px; height: 32px; box-sizing: border-box; }
  .back-btn { display: flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; background: none; border: none; border-radius: 7px; color: var(--text-muted); cursor: pointer; transition: all 0.15s ease; flex-shrink: 0; }
  .back-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .section-nav-header { display: flex; align-items: center; gap: 8px; font-size: 15px; font-weight: 600; color: var(--text-primary); }
  .section-nav-label { font-size: 10px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.8px; padding: 8px 10px 4px; }
  .section-btn { --wails-draggable: no-drag; display: flex; align-items: center; gap: 8px; width: 100%; padding: 7px 10px; background: none; border: none; border-radius: 7px; color: var(--text-secondary); font-size: 13px; font-family: var(--font-sans); cursor: pointer; transition: all 0.15s ease; text-align: left; }
  .section-btn.active { background: rgba(255,255,255,0.08); color: var(--text-primary); }
  .section-count { margin-left: auto; font-size: 11px; color: var(--text-muted); background: var(--bg-card); padding: 1px 6px; border-radius: 8px; }
  .item-list { width: 230px; flex-shrink: 0; display: flex; flex-direction: column; border-right: 1px solid var(--border); overflow: hidden; padding-top: 38px; background: var(--bg-app); }
  .item-list-header { display: flex; align-items: center; justify-content: space-between; padding: 0 14px 8px; height: 32px; box-sizing: border-box; }
  .item-list-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
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
  .detail-header { padding: 0 24px 0; flex-shrink: 0; }
  .detail-header-top { display: flex; align-items: center; justify-content: space-between; gap: 12px; height: 32px; box-sizing: border-box; margin-bottom: 8px; }
  .detail-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin: 0; }
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
