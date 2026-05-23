<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { Backend } from '../lib/wails.js';

  export let onOpenSettings = () => {};

  let settings = null;
  let isOpen = false;
  let activeModel = '';
  let isLoadingLocalModel = false;
  let loadingModelName = '';
  let dropdownEl;
  let triggerBtnEl;

  // Dynamic cloud model list state
  let fetchedModels = [];
  let loadingModels = false;
  let fetchError = '';
  let searchQuery = '';

  onMount(() => {
    loadSettings();
    window.addEventListener('click', handleOutsideClick);
    window.addEventListener('flow:settings-saved', loadSettings);

    const handleLocalLlmLoading = (e) => {
      isLoadingLocalModel = e.detail.loading;
      loadingModelName = e.detail.modelName || '';
    };
    window.addEventListener('flow:local-llm-loading', handleLocalLlmLoading);

    return () => {
      window.removeEventListener('click', handleOutsideClick);
      window.removeEventListener('flow:settings-saved', loadSettings);
      window.removeEventListener('flow:local-llm-loading', handleLocalLlmLoading);
    };
  });

  async function loadSettings() {
    try {
      settings = await Backend.GetSettings();
      if (settings) {
        activeModel = getActiveModelName(settings);
        if (isOpen) {
          fetchCloudModels();
        }
      }
    } catch (err) {
      console.error('Failed to load settings in ModelSelector:', err);
    }
  }

  function getActiveModelName(s) {
    if (s.providerMode === 'local') {
      if (s.llamaModelPath) {
        return s.llamaModelPath.split('/').pop() || s.model || 'Local GGUF';
      }
      return s.model || 'Local LLM';
    } else {
      return s.cloudModel ? s.cloudModel.split('/').pop() : 'Cloud Model';
    }
  }

  let showSubMenu = false;

  function handleOutsideClick(e) {
    if (isOpen && dropdownEl && !dropdownEl.contains(e.target) && triggerBtnEl && !triggerBtnEl.contains(e.target)) {
      isOpen = false;
      showSubMenu = false;
    }
  }

  function toggleDropdown() {
    if (isLoadingLocalModel) return;
    isOpen = !isOpen;
    showSubMenu = false;
    if (isOpen) {
      searchQuery = '';
      fetchCloudModels();
    }
  }

  // Dynamically fetch models from the active cloud endpoint
  async function fetchCloudModels() {
    if (!settings) {
      fetchedModels = [];
      return;
    }

    loadingModels = true;
    fetchError = '';
    
    let url = '';
    let key = '';
    const provider = settings.cloudProvider;

    if (provider === 'openai') {
      url = 'https://api.openai.com/v1';
      key = settings.openaiKey;
    } else if (provider === 'openrouter') {
      url = 'https://openrouter.ai/api/v1';
      key = settings.openRouterKey;
    } else if (provider === 'custom') {
      url = settings.customCloudURL;
      key = settings.customCloudKey;
    } else if (provider === 'anthropic') {
      // Anthropic does not support standard /v1/models endpoint
      fetchedModels = [
        { id: 'claude-3-5-sonnet-latest' },
        { id: 'claude-3-5-haiku-latest' },
        { id: 'claude-3-opus-latest' }
      ];
      loadingModels = false;
      return;
    }

    if (!url) {
      loadingModels = false;
      fetchedModels = [];
      return;
    }

    try {
      // Remove trailing /chat/completions if present
      const cleanUrl = url.replace(/\/chat\/completions$/, '');
      const list = await Backend.ListLocalModels(cleanUrl, key);
      fetchedModels = list || [];
    } catch (err) {
      console.warn('Failed to fetch cloud models:', err);
      fetchError = 'Could not load other models';
      fetchedModels = [];
    } finally {
      loadingModels = false;
    }
  }

  async function selectModel(provider, modelId) {
    if (!settings) return;
    try {
      const current = await Backend.GetSettings();
      current.providerMode = provider === 'local' ? 'local' : 'cloud';
      
      if (provider !== 'local') {
        current.cloudModel = modelId;
      } else {
        if (modelId) current.model = modelId;
      }
      
      await Backend.SaveSettings(current);
      settings = current;
      activeModel = getActiveModelName(current);
      isOpen = false;

      // Dispatch settings-saved immediately so other components are notified of the switch
      window.dispatchEvent(new CustomEvent('flow:settings-saved'));

      // Handle managed llama.cpp server lifecycle
      if (provider === 'local') {
        if (current.llamaManagedEnabled && current.llamaModelPath) {
          try {
            const status = await Backend.GetLlamaServerStatus();
            if (!status || !status.running) {
              const modelName = current.llamaModelPath.split('/').pop() || 'Local Model';
              window.dispatchEvent(new CustomEvent('flow:local-llm-loading', {
                detail: { loading: true, modelName }
              }));
              await Backend.StartLlamaServer(
                current.llamaModelPath,
                Number(current.llamaPort) || 8080,
                Number(current.llamaContextSize) || 4096
              );
            }
          } catch (serverErr) {
            console.error('Failed to start llama-server on selection:', serverErr);
          } finally {
            window.dispatchEvent(new CustomEvent('flow:local-llm-loading', {
              detail: { loading: false }
            }));
            // Dispatch it again post-loading to trigger clean final states
            window.dispatchEvent(new CustomEvent('flow:settings-saved'));
          }
        }
      } else {
        if (current.llamaManagedEnabled) {
          try {
            const status = await Backend.GetLlamaServerStatus();
            if (status && status.running) {
              await Backend.StopLlamaServer();
            }
          } catch (serverErr) {
            console.error('Failed to stop llama-server on cloud selection:', serverErr);
          }
        }
      }
    } catch (err) {
      console.error('Failed to switch model:', err);
    }
  }

  // Reactive options filtering
  $: filteredFetchedModels = searchQuery.trim()
    ? fetchedModels.filter(m => m.id.toLowerCase().includes(searchQuery.toLowerCase()))
    : fetchedModels;
</script>

<div class="model-selector-container">
  <button
    bind:this={triggerBtnEl}
    class="selector-trigger"
    class:active={isOpen}
    class:loading={isLoadingLocalModel}
    on:click={toggleDropdown}
    type="button"
    title="Change LLM model"
    disabled={isLoadingLocalModel}
  >
    {#if isLoadingLocalModel}
      <span class="spinner mini-spinner"></span>
    {:else}
      <span class="pill-dot"></span>
    {/if}
    <span class="active-label">
      {#if isLoadingLocalModel}
        Loading {loadingModelName}...
      {:else}
        {activeModel}
      {/if}
    </span>
    <svg class="chevron" class:open={isOpen} width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
      <polyline points="6 9 12 15 18 9"></polyline>
    </svg>
  </button>

  {#if isOpen}
    <div bind:this={dropdownEl} class="selector-dropdown" role="menu">
      
      <!-- CURRENTLY SELECTED & INSTALLED SECTION -->
      <div class="section-header">Active Configurations</div>
      
      <div class="options-list main-options">
        {#if settings}
          <!-- LOCAL GGUF MODEL -->
          {@const localName = settings.llamaModelPath ? settings.llamaModelPath.split('/').pop() : settings.model || 'Local Model'}
          <button
            class="option-item"
            class:selected={settings.providerMode === 'local'}
            on:click={() => selectModel('local', settings.model)}
            type="button"
            role="menuitem"
            disabled={isLoadingLocalModel}
          >
            <div class="option-meta">
              <div class="option-title-row">
                <span class="option-label">Local GGUF</span>
                <span class="option-sublabel" title={localName}>{localName}</span>
              </div>
              <span class="option-desc">On-device managed llama.cpp</span>
            </div>
            {#if settings.providerMode === 'local'}
              {#if isLoadingLocalModel}
                <span class="spinner dropdown-spinner"></span>
              {:else}
                <svg class="check-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="20 6 9 17 5 12"></polyline>
                </svg>
              {/if}
            {/if}
          </button>

          <!-- CLOUD MODEL -->
          {@const cloudName = settings.cloudModel || 'Default Cloud'}
          <button
            class="option-item"
            class:selected={settings.providerMode === 'cloud'}
            on:click={() => selectModel('cloud', settings.cloudModel)}
            type="button"
            role="menuitem"
          >
            <div class="option-meta">
              <div class="option-title-row">
                <span class="option-label">Cloud Endpoint</span>
                <span class="option-sublabel" title={cloudName}>{cloudName.split('/').pop()}</span>
              </div>
              <span class="option-desc">{settings.cloudProvider || 'cloud'} ({cloudName})</span>
            </div>
            {#if settings.providerMode === 'cloud'}
              <svg class="check-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 5 12"></polyline>
              </svg>
            {/if}
          </button>
        {/if}
      </div>

      <div class="dropdown-divider"></div>

      <!-- Footer More Models Link -->
      <button class="more-models-btn" class:active-more={showSubMenu} on:click|stopPropagation={() => showSubMenu = !showSubMenu} type="button">
        <span>More models</span>
        <svg class="arrow-icon" class:rotated={showSubMenu} width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="9 18 15 12 9 6"></polyline>
        </svg>
      </button>

      <!-- PREMIUM SIDE PANEL (SUB-MENU) DRAWING TO THE LEFT -->
      {#if showSubMenu && settings && settings.cloudProvider}
        <div class="sub-menu-container" on:click|stopPropagation>
          <div class="section-header">Available Cloud Models</div>
          
          <!-- Search bar inside side-panel for premium filtering -->
          {#if fetchedModels.length > 5}
            <div class="search-box">
              <svg class="search-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <circle cx="11" cy="11" r="8"></circle>
                <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
              </svg>
              <input
                type="text"
                bind:value={searchQuery}
                placeholder="Search cloud models..."
              />
            </div>
          {/if}

          <div class="options-list scroll-options">
            {#if loadingModels}
              <div class="loading-state">
                <span class="spinner"></span>
                <span>Fetching models...</span>
              </div>
            {:else if fetchError}
              <div class="error-state">{fetchError}</div>
            {:else if filteredFetchedModels.length === 0}
              <div class="empty-state">No models found</div>
            {:else}
              {#each filteredFetchedModels as m}
                <button
                  class="option-item compact-option"
                  class:selected={settings.cloudModel === m.id}
                  on:click={() => selectModel('cloud', m.id)}
                  type="button"
                  role="menuitem"
                >
                  <div class="option-meta">
                    <span class="option-label compact-label" title={m.id}>{m.id}</span>
                  </div>
                  {#if settings.cloudModel === m.id}
                    <svg class="check-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="20 6 9 17 5 12"></polyline>
                    </svg>
                  {/if}
                </button>
              {/each}
            {/if}
          </div>

          <div class="dropdown-divider"></div>
          
          <!-- Quick link to settings from sub-menu -->
          <button class="more-models-btn settings-link" on:click={() => { isOpen = false; showSubMenu = false; onOpenSettings(); }} type="button">
            <span>Configure settings</span>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="9 18 15 12 9 6"></polyline>
            </svg>
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .model-selector-container {
    position: relative;
    display: inline-block;
    font-family: var(--font-sans, system-ui, -apple-system, sans-serif);
  }

  .selector-trigger {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 999px;
    color: var(--text-secondary, #a1a1aa);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
    user-select: none;
  }

  .selector-trigger:hover,
  .selector-trigger.active {
    background: rgba(255, 255, 255, 0.08);
    border-color: rgba(255, 255, 255, 0.12);
    color: var(--text-primary, #ffffff);
  }

  .pill-dot {
    width: 6px;
    height: 6px;
    background: var(--accent, #10b981);
    border-radius: 50%;
  }

  .active-label {
    max-width: 150px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .chevron {
    color: var(--text-muted, #71717a);
    transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }
  .chevron.open {
    transform: rotate(180deg);
  }

  /* Dropdown Popover */
  .selector-dropdown {
    position: absolute;
    bottom: calc(100% + 8px);
    right: 0;
    width: 290px;
    background: rgba(24, 24, 27, 0.9);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 14px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.45);
    padding: 8px 6px;
    z-index: 1000;
    display: flex;
    flex-direction: column;
    animation: menuFadeIn 0.2s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  /* Sub-menu Side Panel Drawer aligned to the left */
  .sub-menu-container {
    position: absolute;
    right: calc(100% + 8px);
    bottom: 0;
    width: 290px;
    background: rgba(24, 24, 27, 0.92);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 14px;
    box-shadow: -10px 20px 40px rgba(0, 0, 0, 0.45);
    padding: 8px 6px;
    z-index: 1001;
    display: flex;
    flex-direction: column;
    animation: subMenuSlideIn 0.2s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  @keyframes subMenuSlideIn {
    from {
      opacity: 0;
      transform: translateX(8px);
    }
    to {
      opacity: 1;
      transform: translateX(0);
    }
  }

  @keyframes menuFadeIn {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.97);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .section-header {
    font-size: 10px;
    font-weight: 700;
    color: var(--text-muted, #71717a);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding: 6px 10px 4px;
  }

  .options-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .scroll-options {
    max-height: 180px;
    overflow-y: auto;
    padding-right: 2px;
    margin-top: 4px;
  }

  .scroll-options::-webkit-scrollbar {
    width: 4px;
  }
  .scroll-options::-webkit-scrollbar-track {
    background: transparent;
  }
  .scroll-options::-webkit-scrollbar-thumb {
    background: rgba(255, 255, 255, 0.1);
    border-radius: 2px;
  }

  .option-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px 10px;
    background: none;
    border: none;
    border-radius: 8px;
    text-align: left;
    cursor: pointer;
    transition: background 0.15s ease, color 0.15s ease;
    color: var(--text-secondary, #d4d4d8);
  }

  .option-item:hover {
    background: rgba(255, 255, 255, 0.04);
    color: var(--text-primary, #ffffff);
  }

  .option-item.selected {
    color: var(--text-primary, #ffffff);
  }

  .compact-option {
    padding: 6px 10px;
  }

  .option-meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
    margin-right: 8px;
  }

  .option-title-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .option-label {
    font-size: 13px;
    font-weight: 600;
  }

  .compact-label {
    font-size: 12px;
    font-weight: 500;
    font-family: var(--font-mono, monospace);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .option-sublabel {
    font-size: 10px;
    color: var(--text-muted, #71717a);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.06);
    padding: 1px 5px;
    border-radius: 3px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 110px;
  }

  .option-desc {
    font-size: 11px;
    color: var(--text-muted, #71717a);
    line-height: 1.3;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .check-icon {
    color: var(--accent, #10b981);
    flex-shrink: 0;
  }

  .dropdown-divider {
    height: 1px;
    background: rgba(255, 255, 255, 0.06);
    margin: 6px 4px;
  }

  /* Search Box */
  .search-box {
    display: flex;
    align-items: center;
    gap: 6px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 6px;
    padding: 4px 8px;
    margin: 4px 6px;
  }
  .search-icon {
    color: var(--text-muted, #71717a);
  }
  .search-box input {
    background: none;
    border: none;
    outline: none;
    color: var(--text-primary, #ffffff);
    font-size: 11px;
    padding: 0;
    width: 100%;
  }
  .search-box input::placeholder {
    color: var(--text-muted, #71717a);
  }

  /* Async Loading / Empty / Error State */
  .loading-state, .error-state, .empty-state {
    padding: 20px;
    text-align: center;
    color: var(--text-muted, #71717a);
    font-size: 12px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }

  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid rgba(255, 255, 255, 0.1);
    border-top-color: var(--accent, #10b981);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    box-sizing: border-box;
  }

  .mini-spinner {
    width: 10px;
    height: 10px;
    border-width: 1.5px;
  }

  .dropdown-spinner {
    width: 14px;
    height: 14px;
    border-width: 2px;
  }

  .selector-trigger.loading {
    opacity: 0.8;
    cursor: default;
    background: rgba(255, 255, 255, 0.02);
    border-color: rgba(255, 255, 255, 0.04);
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* More Models Link */
  .more-models-btn {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px 10px;
    background: none;
    border: none;
    border-radius: 8px;
    cursor: pointer;
    color: var(--text-secondary, #d4d4d8);
    font-size: 12.5px;
    font-weight: 500;
    transition: background 0.15s ease, color 0.15s ease;
  }

  .more-models-btn:hover,
  .more-models-btn.active-more {
    background: rgba(255, 255, 255, 0.04);
    color: var(--text-primary, #ffffff);
  }

  .more-models-btn svg.arrow-icon {
    color: var(--text-muted, #71717a);
    transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  .more-models-btn:hover svg.arrow-icon,
  .more-models-btn.active-more svg.arrow-icon {
    color: var(--text-primary, #ffffff);
  }

  .more-models-btn svg.arrow-icon.rotated {
    transform: rotate(-180deg);
  }

  .settings-link {
    font-size: 11.5px;
    color: var(--text-muted, #71717a);
    padding: 6px 10px;
  }
  .settings-link:hover {
    color: var(--text-secondary, #d4d4d8);
  }
</style>
