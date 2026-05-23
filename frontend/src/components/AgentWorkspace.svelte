<script>
  import { afterUpdate, createEventDispatcher, tick, onMount, onDestroy } from 'svelte';
  import { renderMarkdown } from '../lib/markdown.js';
  import { formatToolLabel, formatToolName } from '../lib/utils/formatters.js';
  import { Backend, Events } from '../lib/wails.js';
  import AgentFileCard from './AgentFileCard.svelte';
  import AgentInfoPanel from './AgentInfoPanel.svelte';
  import TypingIndicator from './TypingIndicator.svelte';
  import LoadingSpinner from './LoadingSpinner.svelte';
  import { skills, refreshSkills } from '../lib/stores/pluginsStore.js';

  export let taskTitle = '';
  export let messages = [];            // Array of { role: 'user'|'assistant', content, steps?, isStreaming? }
  export let isStreaming = false;
  export let loading = false;
  export let disabled = false;
  export let parseDocuments = true;

  // Data for the info panel
  export let progressSteps = [];     // Array of { label, status }
  export let createdFiles = [];      // Array of { name, path, type?, size? }
  export let contextTools = [];      // Array of tool name strings
  export let skillsUsed = [];        // Array of skill name strings loaded via use_skill

  // Derive the original prompt from the first user message
  $: prompt = (messages.find(m => m.role === 'user')?.content || '').trim();

  const dispatch = createEventDispatcher();

  let contentContainer;
  let input = '';
  let textareaEl;
  let fileInputEl;
  let files = [];
  let showSkillMenu = false;
  let skillQuery = '';
  let skillHighlightIdx = 0;
  let skillMenuEl;
  let selectedSkill = null;
  let skipNextScroll = false;
  let userScrolledUp = false;
  let dragOver = false;

  // Integrations state
  let showIntegrationsMenu = false;
  let integrationFilter = '';
  let integrationsMenuEl;
  let integrationsBtnEl;

  let integrations = [
    {
      id: 'parse-document',
      name: 'parse-document',
      iconClass: 'doc-icon',
      iconSvg: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z"/><path d="M14 2v6h6"/></svg>`,
      description: 'Extract and send document text',
    },
    {
      id: 'web-search',
      name: 'web-search',
      iconClass: 'web-icon',
      iconSvg: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>`,
      description: 'Search & Retrieve web resources',
    },
    {
      id: 'screencapture',
      name: 'screencapture',
      iconClass: 'screen-icon',
      iconSvg: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>`,
      description: 'Take screenshots for visual context',
    }
  ];

  $: filteredIntegrations = integrationFilter
    ? integrations.filter(i =>
        i.name.toLowerCase().includes(integrationFilter.toLowerCase()) ||
        (i.description || '').toLowerCase().includes(integrationFilter.toLowerCase())
      )
    : integrations;

  function toggleIntegrationsMenu() {
    showIntegrationsMenu = !showIntegrationsMenu;
    if (showIntegrationsMenu) {
      integrationFilter = '';
    }
  }

  function handleGlobalClick(e) {
    if (showIntegrationsMenu &&
        integrationsMenuEl &&
        !integrationsMenuEl.contains(e.target) &&
        integrationsBtnEl &&
        !integrationsBtnEl.contains(e.target)) {
      showIntegrationsMenu = false;
    }
  }

  // File attachment constants (same as ChatInput)
  const ACCEPTED_TYPES = [
    'image/png', 'image/jpeg', 'image/gif', 'image/webp',
    'application/pdf',
    'text/plain', 'text/csv', 'text/markdown', 'text/html',
    'application/json', 'application/xml',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  ].join(',');

  const IMAGE_TYPES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
  const MAX_FILE_SIZE = 20 * 1024 * 1024;

  $: filteredSkills = skillQuery
    ? $skills.filter(skill =>
        skill.name.toLowerCase().includes(skillQuery.toLowerCase()) ||
        (skill.description || '').toLowerCase().includes(skillQuery.toLowerCase()))
    : $skills;

  $: if (filteredSkills.length > 0 && skillHighlightIdx >= filteredSkills.length) {
    skillHighlightIdx = filteredSkills.length - 1;
  }

  afterUpdate(() => {
    if (skipNextScroll) {
      skipNextScroll = false;
      return;
    }
    if (loading && !userScrolledUp) {
      scrollToBottom();
    }
  });

  function handleScroll() {
    if (!contentContainer) return;
    const { scrollTop, scrollHeight, clientHeight } = contentContainer;
    userScrolledUp = scrollHeight - scrollTop - clientHeight > 150;
  }

  function scrollToBottom() {
    if (contentContainer) {
      contentContainer.scrollTo({
        top: contentContainer.scrollHeight,
        behavior: isStreaming ? 'auto' : 'smooth',
      });
    }
  }

  // Build summary from all steps across all messages
  $: allSteps = messages.flatMap(m => m.steps || []);
  $: commandCount = allSteps.filter(s => s.type === 'tool_call').length;
  $: toolNames = [...new Set(allSteps.filter(s => s.type === 'tool_call').map(s => s.tool_name))];
  $: summaryText = buildSummary(commandCount, toolNames.length);

  function buildSummary(cmds, tools) {
    const parts = [];
    if (cmds > 0) parts.push(`Ran ${cmds} command${cmds !== 1 ? 's' : ''}`);
    if (tools > 0) parts.push(`used ${tools} tool${tools !== 1 ? 's' : ''}`);
    return parts.length > 0 ? parts.join(', ') : '';
  }

  // Expandable steps (keyed by message index + step index).
  // NOTE: We access expandedSteps directly in the template (not via a helper
  // function) so that Svelte's compiler can track the reactive dependency.
  let expandedSteps = {};
  function toggleStep(msgIdx, stepIdx) {
    const key = `${msgIdx}-${stepIdx}`;
    expandedSteps[key] = !expandedSteps[key];
    expandedSteps = expandedSteps;
    skipNextScroll = true;
  }

  let pendingApprovals = {};

  $: if (messages.length === 0) {
    pendingApprovals = {};
  }

  onMount(() => {
    Events.on('sandbox:request_approval', handleSandboxApproval);
    Events.on('command:request_approval', handleCommandApproval);
    window.addEventListener('click', handleGlobalClick);
  });

  onDestroy(() => {
    Events.off('sandbox:request_approval');
    Events.off('command:request_approval');
    window.removeEventListener('click', handleGlobalClick);
  });

  function handleSandboxApproval(req) {
    if (!req || !req.id || !req.path) return;
    const active = findActiveToolStep();
    if (active) {
      const key = `${active.msgIdx}-${active.stepIdx}`;
      pendingApprovals[key] = {
        type: 'sandbox',
        id: req.id,
        path: req.path
      };
      pendingApprovals = pendingApprovals;
      
      // Auto expand this step so they see the inline prompt immediately
      expandedSteps[key] = true;
      expandedSteps = expandedSteps;
    }
  }

  function handleCommandApproval(req) {
    if (!req || !req.id || !req.command) return;
    const active = findActiveToolStep();
    if (active) {
      const key = `${active.msgIdx}-${active.stepIdx}`;
      pendingApprovals[key] = {
        type: 'command',
        id: req.id,
        command: req.command,
        exe: req.exe
      };
      pendingApprovals = pendingApprovals;

      // Auto expand this step so they see the inline prompt immediately
      expandedSteps[key] = true;
      expandedSteps = expandedSteps;
    }
  }

  function findActiveToolStep() {
    // Traverse backwards to find the last assistant tool call step
    for (let m = messages.length - 1; m >= 0; m--) {
      const msg = messages[m];
      if (msg.role === 'assistant' && msg.steps) {
        for (let s = msg.steps.length - 1; s >= 0; s--) {
          const step = msg.steps[s];
          if (step.type === 'tool_call') {
            return { msgIdx: m, stepIdx: s };
          }
        }
      }
    }
    return null;
  }

  async function submitCommandApproval(key, id, choice) {
    try {
      await Backend.SubmitCommandApproval(id, choice);
    } catch (e) {
      console.error('SubmitCommandApproval failed:', e);
    }
    delete pendingApprovals[key];
    pendingApprovals = pendingApprovals;
  }

  async function submitSandboxApproval(key, id, approved) {
    try {
      await Backend.SubmitSandboxApproval(id, approved);
    } catch (e) {
      console.error('SubmitSandboxApproval failed:', e);
    }
    delete pendingApprovals[key];
    pendingApprovals = pendingApprovals;
  }

  function handleOpenFile(e) {
    dispatch('openFile', e.detail);
  }

  function handleRevealFile(e) {
    dispatch('revealFile', e.detail);
  }

  function handleOpenFolder() {
    dispatch('openFolder');
  }

  function handleInfoOpenFile(e) {
    dispatch('openFile', e.detail);
  }

  // ─── Chat Input ───
  export function focus() {
    textareaEl?.focus();
  }

  function handleKeydown(e) {
    if (handleSkillKeydown(e)) return;
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function handleInput(e) {
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 200) + 'px';
    checkSkillSlash();
  }

  function openSkillSlash() {
    if (!input.trim()) input = '/';
    skillQuery = '';
    skillHighlightIdx = 0;
    showSkillMenu = true;
    refreshSkills();
    tick().then(() => {
      textareaEl?.focus();
      if (textareaEl) {
        textareaEl.selectionStart = textareaEl.selectionEnd = input.length;
      }
    });
  }

  function checkSkillSlash() {
    if (input.startsWith('/')) {
      const afterSlash = input.slice(1).split(/\s/)[0];
      const hasSpace = /\s/.test(input.slice(1));
      if (!hasSpace) {
        skillQuery = afterSlash;
        if (!showSkillMenu) {
          skillHighlightIdx = 0;
          refreshSkills();
        }
        showSkillMenu = true;
        return;
      }
    }
    showSkillMenu = false;
    skillQuery = '';
  }

  function selectSkillForPrompt(skill) {
    const rest = input.startsWith('/') ? input.slice(1).replace(/^\S*/, '') : input;
    selectedSkill = skill;
    input = rest.trimStart();
    showSkillMenu = false;
    skillQuery = '';
    tick().then(() => {
      if (textareaEl) {
        textareaEl.focus();
        textareaEl.selectionStart = textareaEl.selectionEnd = input.length;
        textareaEl.style.height = 'auto';
        textareaEl.style.height = Math.min(textareaEl.scrollHeight, 200) + 'px';
      }
    });
  }

  function handleSkillKeydown(e) {
    if (!showSkillMenu || filteredSkills.length === 0) return false;

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      skillHighlightIdx = (skillHighlightIdx + 1) % filteredSkills.length;
      scrollSkillIntoView();
      return true;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      skillHighlightIdx = (skillHighlightIdx - 1 + filteredSkills.length) % filteredSkills.length;
      scrollSkillIntoView();
      return true;
    }
    if (e.key === 'Tab' || e.key === 'Enter') {
      e.preventDefault();
      selectSkillForPrompt(filteredSkills[skillHighlightIdx]);
      return true;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      showSkillMenu = false;
      return true;
    }
    return false;
  }

  function scrollSkillIntoView() {
    tick().then(() => {
      const active = skillMenuEl?.querySelector('.skill-option.highlighted');
      active?.scrollIntoView({ block: 'nearest' });
    });
  }

  function clearSelectedSkill() {
    selectedSkill = null;
    textareaEl?.focus();
  }

  function handleSend() {
    const text = input.trim();
    if ((!text && files.length === 0 && !selectedSkill) || loading) return;
    userScrolledUp = false;
    dispatch('sendFollowUp', { text, files: [...files], selectedSkillName: selectedSkill?.name || '' });
    input = '';
    files = [];
    selectedSkill = null;
    if (textareaEl) {
      textareaEl.style.height = 'auto';
    }
  }

  function handleCancel() {
    dispatch('cancel');
  }

  // ─── File attachment helpers ───
  function openFilePicker() {
    fileInputEl?.click();
  }

  async function handleFileSelect(e) {
    const selected = Array.from(e.target.files || []);
    for (const file of selected) {
      if (file.size > MAX_FILE_SIZE) {
        alert(`File "${file.name}" exceeds the 20 MB limit.`);
        continue;
      }
      try {
        const result = await readFileAsBase64(file);
        files = [...files, {
          name: file.name,
          type: file.type || inferMimeType(file.name),
          size: file.size,
          dataUrl: result.dataUrl,
          data: result.base64,
        }];
      } catch (err) {
        alert(`Could not attach "${file.name}": ${err?.message || err}`);
      }
    }
    e.target.value = '';
    textareaEl?.focus();
  }

  function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(reader.error || new Error('Failed to read file'));
      reader.onload = () => {
        const dataUrl = reader.result;
        const base64 = dataUrl.split(',')[1] || '';
        resolve({ dataUrl, base64 });
      };
      reader.readAsDataURL(file);
    });
  }

  function inferMimeType(name) {
    const ext = (name || '').split('.').pop()?.toLowerCase();
    if (ext === 'pdf') return 'application/pdf';
    if (ext === 'csv') return 'text/csv';
    if (ext === 'md' || ext === 'markdown') return 'text/markdown';
    if (ext === 'html' || ext === 'htm') return 'text/html';
    if (ext === 'json') return 'application/json';
    if (ext === 'xml') return 'application/xml';
    return 'text/plain';
  }

  function removeFile(index) {
    files = files.filter((_, i) => i !== index);
    textareaEl?.focus();
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  function isImage(mime) {
    return IMAGE_TYPES.has(mime);
  }

  function handleDragOver(e) {
    e.preventDefault();
    dragOver = true;
  }

  function handleDragLeave() {
    dragOver = false;
  }

  async function handleDrop(e) {
    e.preventDefault();
    dragOver = false;
    const droppedFiles = Array.from(e.dataTransfer?.files || []);
    for (const file of droppedFiles) {
      if (file.size > MAX_FILE_SIZE) {
        alert(`File "${file.name}" exceeds the 20 MB limit.`);
        continue;
      }
      try {
        const result = await readFileAsBase64(file);
        files = [...files, {
          name: file.name,
          type: file.type || inferMimeType(file.name),
          size: file.size,
          dataUrl: result.dataUrl,
          data: result.base64,
        }];
      } catch (err) {
        alert(`Could not attach "${file.name}": ${err?.message || err}`);
      }
    }
  }

  // ─── Copy helpers ───
  let copiedMsgIdx = -1;

  function handleContentClick(e) {
    const btn = e.target.closest('.code-copy-btn');
    if (!btn) return;
    e.preventDefault();
    const code = btn.getAttribute('data-code')
      ?.replace(/&amp;/g, '&')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"');
    if (code) {
      navigator.clipboard.writeText(code);
      const label = btn.querySelector('.code-copy-label');
      if (label) {
        label.textContent = 'Copied!';
        setTimeout(() => { label.textContent = 'Copy'; }, 1500);
      }
    }
  }

  function copyMessage(msgIdx, content) {
    if (!content) return;
    navigator.clipboard.writeText(content);
    copiedMsgIdx = msgIdx;
    setTimeout(() => { copiedMsgIdx = -1; }, 1500);
  }
</script>

<div class="workspace-layout">
  <!-- Center Content -->
  <div class="workspace-center">
    <div class="workspace-scroll" bind:this={contentContainer} on:scroll={handleScroll}>
      <div class="workspace-content">
        <!-- Task Title -->
        {#if taskTitle}
          <div class="task-title-bar">
            <h2 class="task-title">{taskTitle}</h2>
          </div>
        {/if}

        <!-- Messages -->
        {#each messages as message, msgIdx}
          {#if message.role === 'user'}
            {#if msgIdx > 0}
              <div class="chat-divider">
                <span class="divider-line"></span>
                <span class="divider-dot"></span>
                <span class="divider-line"></span>
              </div>
            {/if}
            <div class="user-pill">
              {#if message.selectedSkill}
                <div class="user-pill-skill-chip">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/>
                    <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
                  </svg>
                  <span>{message.selectedSkill}</span>
                </div>
              {/if}
              {#if message.files && message.files.length > 0}
                <div class="user-pill-files">
                  {#each message.files as file}
                    <div class="user-pill-file-chip">
                      {#if IMAGE_TYPES.has(file.type)}
                        <img class="user-pill-file-thumb" src={file.dataUrl} alt={file.name} />
                      {:else}
                        <svg class="user-pill-file-icon" width="12" height="12" viewBox="0 0 24 24" fill="none">
                          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                          <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                        </svg>
                      {/if}
                      <span class="user-pill-file-name">{file.name}</span>
                    </div>
                  {/each}
                </div>
              {/if}
              {#if message.content}
                <span class="user-pill-text">{message.content}</span>
              {/if}
            </div>
          {:else if message.role === 'assistant'}
            <!-- Summary line (show after first assistant response only, when not streaming) -->
            {#if msgIdx === 1 && summaryText && !message.isStreaming}
              <div class="summary-line">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
                  <path d="M22 4L12 14.01l-3-3"/>
                </svg>
                <span>{summaryText}</span>
              </div>
            {/if}

            <!-- Steps (collapsed tool calls, thinking) -->
            {#if message.steps && message.steps.length > 0}
              <div class="steps-container">
                {#each message.steps as step, i}
                  {#if step.type === 'thinking'}
                    <div class="step-thinking-inline">
                      {step.content}
                    </div>
                  {:else if step.type === 'tool_call'}
                    <button class="step-toggle step-tool" class:step-skill={step.tool_name === 'use_skill'} on:click={() => toggleStep(msgIdx, i)}>
                      <span class="step-icon">{expandedSteps[`${msgIdx}-${i}`] ? '▼' : '▶'}</span>
                      <span class="step-tool-icon">
                        {#if step.tool_name === 'use_skill'}
                          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/>
                            <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
                          </svg>
                        {:else}
                          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
                          </svg>
                        {/if}
                      </span>
                      <span class="step-label">{formatToolLabel(step.tool_name, step.tool_input)}</span>
                      {#if pendingApprovals[`${msgIdx}-${i}`]}
                        <span class="pending-approval-badge">Requires Action</span>
                      {/if}
                    </button>
                    {#if expandedSteps[`${msgIdx}-${i}`]}
                      <div class="step-content step-tool-content">
                        <pre>{step.tool_input}</pre>
                      </div>
                      {#if pendingApprovals[`${msgIdx}-${i}`]}
                        {@const req = pendingApprovals[`${msgIdx}-${i}`]}
                        <div class="inline-approval-card">
                          {#if req.type === 'command'}
                            <div class="approval-header">
                              <svg class="warning-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                                <line x1="12" y1="9" x2="12" y2="13"/>
                                <line x1="12" y1="17" x2="12.01" y2="17"/>
                              </svg>
                              <span class="approval-text">Command requires permission:</span>
                            </div>
                            <div class="approval-actions">
                              <button class="btn-compact btn-always" on:click={() => submitCommandApproval(`${msgIdx}-${i}`, req.id, 'always')}>
                                Allow Always
                              </button>
                              <button class="btn-compact btn-session" on:click={() => submitCommandApproval(`${msgIdx}-${i}`, req.id, 'session')}>
                                Allow Session
                              </button>
                              <button class="btn-compact btn-deny" on:click={() => submitCommandApproval(`${msgIdx}-${i}`, req.id, 'deny')}>
                                Block
                              </button>
                            </div>
                          {:else if req.type === 'sandbox'}
                            <div class="approval-header">
                              <svg class="warning-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
                                <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
                              </svg>
                              <span class="approval-text">Wants access to folder: <code>{req.path}</code></span>
                            </div>
                            <div class="approval-actions">
                              <button class="btn-compact btn-deny" on:click={() => submitSandboxApproval(`${msgIdx}-${i}`, req.id, false)}>
                                Deny
                              </button>
                              <button class="btn-compact btn-allow" on:click={() => submitSandboxApproval(`${msgIdx}-${i}`, req.id, true)}>
                                Allow
                              </button>
                            </div>
                          {/if}
                        </div>
                      {/if}
                      {#if message.steps[i + 1]?.type === 'tool_result'}
                        <div class="step-content step-result-content">
                          <div class="step-result-header">Result:</div>
                          <pre>{message.steps[i + 1].content}</pre>
                        </div>
                      {/if}
                    {/if}
                  {:else if step.type === 'tool_result'}
                    <!-- Rendered inline with its tool_call above -->
                  {/if}
                {/each}
              </div>
            {/if}

            <!-- Agent Response Text -->
            {#if message.content}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="agent-response" on:click={handleContentClick}>
                {@html renderMarkdown(message.content)}
                {#if message.isStreaming}
                  <LoadingSpinner />
                {/if}
              </div>

              <!-- Copy full message button -->
              {#if !message.isStreaming}
                <div class="message-actions">
                  <button class="msg-copy-btn" on:click={() => copyMessage(msgIdx, message.content)} title="Copy message">
                    {#if copiedMsgIdx === msgIdx}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M20 6L9 17l-5-5"/>
                      </svg>
                      <span>Copied!</span>
                    {:else}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>
                        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
                      </svg>
                      <span>Copy</span>
                    {/if}
                  </button>
                </div>
              {/if}
            {:else if message.isStreaming}
              <div class="agent-response">
                <TypingIndicator />
              </div>
            {/if}
            <!-- Created File Cards inline under the turn that generated them -->
            {#if message.filesCreated && message.filesCreated.length > 0}
              <div class="file-cards" style="margin-top: 10px; margin-bottom: 10px;">
                {#each message.filesCreated as file}
                  <AgentFileCard {file} on:openFile={handleOpenFile} on:openFolder={handleRevealFile} />
                {/each}
              </div>
            {/if}
          {/if}
        {/each}

        <!-- Created File Cards (shown at bottom only as fallback if not present inline in messages) -->
        {#if createdFiles.length > 0 && !messages.some(m => m.filesCreated && m.filesCreated.length > 0)}
          <div class="file-cards">
            {#each createdFiles as file}
              <AgentFileCard {file} on:openFile={handleOpenFile} on:openFolder={handleRevealFile} />
            {/each}
          </div>
        {/if}
      </div>
    </div>

    <!-- Chat Input -->
    <footer class="workspace-input-area">
      <div
        class="workspace-input-container"
        class:drag-over={dragOver}
        on:dragover={handleDragOver}
        on:dragleave={handleDragLeave}
        on:drop={handleDrop}
        role="group"
        aria-label="Follow-up input"
      >
        {#if showSkillMenu}
          <div class="skill-menu" bind:this={skillMenuEl}>
            <div class="skill-menu-header">Skills</div>
            {#if filteredSkills.length === 0}
              <div class="skill-menu-empty">No matching skills</div>
            {:else}
              {#each filteredSkills as skill, i (skill.id)}
                <button
                  type="button"
                  class="skill-option"
                  class:highlighted={i === skillHighlightIdx}
                  on:mousedown|preventDefault={() => selectSkillForPrompt(skill)}
                >
                  <span class="skill-option-name">{skill.name}</span>
                  {#if skill.description}<span class="skill-option-desc">{skill.description}</span>{/if}
                </button>
              {/each}
            {/if}
          </div>
        {/if}

        {#if showIntegrationsMenu}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div class="integrations-menu" bind:this={integrationsMenuEl} on:click|stopPropagation>
            <div class="integrations-header">
              <div class="integrations-title-wrap">
                <svg class="integrations-header-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
                </svg>
                <span>Tools</span>
              </div>
            </div>

            <div class="integrations-list">
              {#if filteredIntegrations.length === 0}
                <div class="integrations-empty">No matching integrations</div>
              {:else}
                {#each filteredIntegrations as item (item.id)}
                  <div class="integration-item">
                    <div class="integration-info">
                      <span class="integration-icon {item.iconClass}">
                        {@html item.iconSvg}
                      </span>
                      <span class="integration-name">{item.name}</span>
                    </div>
                    <div class="integration-actions">
                      <label class="toggle-switch">
                        <input
                          type="checkbox"
                          checked={item.id === 'parse-document' ? parseDocuments : false}
                          on:change={(e) => {
                            if (item.id === 'parse-document') {
                              parseDocuments = e.target.checked;
                            }
                          }}
                        />
                        <span class="toggle-slider"></span>
                      </label>
                    </div>
                  </div>
                {/each}
              {/if}
            </div>
          </div>
        {/if}

        {#if files.length > 0}
          <div class="file-preview-row">
            {#each files as file, i}
              <div class="file-chip" title={file.name}>
                {#if isImage(file.type)}
                  <img class="file-thumb" src={file.dataUrl} alt={file.name} />
                {:else}
                  <div class="file-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none">
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                      <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>
                  </div>
                {/if}
                <span class="file-name">{file.name}</span>
                <span class="file-size">{formatSize(file.size)}</span>
                <button class="file-remove" on:click={() => removeFile(i)} title="Remove file">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none">
                    <path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                  </svg>
                </button>
              </div>
            {/each}
          </div>
        {/if}

        <div class="composer-text-row">
          {#if selectedSkill}
            <div class="skill-chip" title={selectedSkill.description || selectedSkill.name}>
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/>
                <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
              </svg>
              <span>{selectedSkill.name}</span>
              <button class="skill-chip-remove" on:click={clearSelectedSkill} title="Remove skill">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none">
                  <path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/>
                </svg>
              </button>
            </div>
          {/if}
          <textarea
            bind:this={textareaEl}
            bind:value={input}
            on:keydown={handleKeydown}
            on:input={handleInput}
            placeholder="Send a follow-up..."
            rows="1"
            disabled={disabled || loading}
          ></textarea>
        </div>
        <div class="workspace-input-bottom">
          <div class="workspace-input-bottom-left">
            <button
              class="btn-attach"
              on:click|stopPropagation={openFilePicker}
              disabled={disabled || loading}
              title="Attach file"
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </button>
            <button
              class="btn-slash"
              on:click|stopPropagation={openSkillSlash}
              disabled={disabled || loading}
              title="Show skills"
            >
              /
            </button>
            <span class="input-divider"></span>
            <button
              bind:this={integrationsBtnEl}
              class="btn-integrations-toggle"
              class:active={showIntegrationsMenu}
              on:click|stopPropagation={toggleIntegrationsMenu}
              disabled={disabled || loading}
              title="Tools"
              type="button"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
              </svg>
            </button>
          </div>
          <div class="workspace-input-bottom-right">
            {#if loading}
              <button
                class="btn-send btn-cancel"
                on:click={handleCancel}
                title="Cancel"
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                  <rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor"/>
                </svg>
              </button>
            {:else}
              <button
                class="btn-send"
                on:click={handleSend}
                disabled={(!input.trim() && files.length === 0 && !selectedSkill) || disabled}
                title="Send"
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                  <path d="M7 11l5-5m0 0l5 5m-5-5v12" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
              </button>
            {/if}
          </div>
        </div>
      </div>

      <input
        bind:this={fileInputEl}
        type="file"
        multiple
        accept={ACCEPTED_TYPES}
        on:change={handleFileSelect}
        class="hidden-file-input"
      />
    </footer>
  </div>

  <!-- Right Info Panel -->
  <AgentInfoPanel
    {progressSteps}
    files={createdFiles}
    {contextTools}
    {skillsUsed}
    on:openFile={handleInfoOpenFile}
    on:openFolder={handleOpenFolder}
  />
</div>

<style>
  .workspace-layout {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
  }

  .workspace-center {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow: hidden;
  }

  .workspace-scroll {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    background: var(--bg-primary);
  }

  .workspace-scroll::-webkit-scrollbar {
    width: 6px;
  }

  .workspace-scroll::-webkit-scrollbar-track {
    background: transparent;
  }

  .workspace-scroll::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: 3px;
  }

  .workspace-content {
    max-width: 720px;
    margin: 0 auto;
    padding: 24px 24px 24px;
  }

  /* Task Title */
  .task-title-bar {
    margin-bottom: 24px;
  }

  .task-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    letter-spacing: -0.3px;
  }

  /* User Message Pill */
  .user-pill {
    display: inline-block;
    max-width: 100%;
    padding: 10px 18px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 20px;
    margin-bottom: 16px;
  }

  .user-pill-text {
    font-size: 14px;
    color: var(--text-primary);
    line-height: 1.5;
    word-wrap: break-word;
  }

  .user-pill-skill-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: var(--accent-bg);
    border: 1px solid rgba(45, 212, 191, 0.25);
    border-radius: 6px;
    padding: 3px 8px;
    margin: 0 0 6px;
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
  }

  .user-pill-skill-chip span {
    color: var(--text-primary);
  }

  .user-pill-files {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 6px;
  }

  .user-pill-file-chip {
    display: flex;
    align-items: center;
    gap: 4px;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 11px;
    color: var(--text-secondary);
  }

  .user-pill-file-thumb {
    width: 18px;
    height: 18px;
    border-radius: 3px;
    object-fit: cover;
    flex-shrink: 0;
  }

  .user-pill-file-icon {
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .user-pill-file-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 120px;
  }

  /* Summary Line */
  .summary-line {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--text-muted);
    margin-bottom: 16px;
    padding: 6px 0;
  }

  .summary-line svg {
    color: #22c55e;
    flex-shrink: 0;
  }

  /* Steps */
  .steps-container {
    margin-bottom: 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    background: rgba(255, 255, 255, 0.02);
  }

  .step-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    border: none;
    background: none;
    color: var(--text-secondary);
    cursor: pointer;
    font-size: 12px;
    font-family: inherit;
    text-align: left;
    transition: background 0.15s ease;
  }

  .step-toggle:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .step-toggle + .step-toggle {
    border-top: 1px solid rgba(255, 255, 255, 0.04);
  }

  .step-icon {
    font-size: 9px;
    color: var(--text-muted);
    flex-shrink: 0;
    width: 12px;
  }

  .step-tool-icon {
    display: flex;
    align-items: center;
    color: var(--accent);
    flex-shrink: 0;
  }

  .step-label {
    font-weight: 500;
  }

  .pending-approval-badge {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #eab308;
    background: rgba(234, 179, 8, 0.12);
    border: 1px solid rgba(234, 179, 8, 0.2);
    padding: 1px 6px;
    border-radius: 4px;
    margin-left: auto;
    animation: pulse-glow 2s infinite ease-in-out;
  }

  .inline-approval-card {
    margin: 8px 12px;
    padding: 10px 12px;
    background: rgba(30, 30, 30, 0.4);
    border: 1px solid rgba(234, 179, 8, 0.25);
    border-radius: 8px;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    animation: fadeInStep 0.2s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }
  
  .approval-header {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 200px;
  }

  .warning-icon {
    color: #eab308;
    flex-shrink: 0;
  }

  .approval-text {
    font-size: 11.5px;
    color: var(--text-secondary, #e5e7eb);
    line-height: 1.4;
  }
  
  .approval-text code {
    font-family: var(--font-mono, monospace);
    font-size: 10.5px;
    background: rgba(0, 0, 0, 0.25);
    padding: 1.5px 5px;
    border-radius: 4px;
    color: var(--accent, #3b82f6);
  }

  .approval-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .btn-compact {
    padding: 4px 10px;
    font-size: 11.5px;
    font-weight: 600;
    border-radius: 6px;
    cursor: pointer;
    border: none;
    transition: all 0.12s ease;
  }

  .btn-always {
    background: #007aff;
    color: #ffffff;
    box-shadow: 0 2px 4px rgba(0, 122, 255, 0.15);
  }
  .btn-always:hover {
    background: #1a88ff;
  }
  .btn-always:active {
    transform: scale(0.97);
  }

  .btn-session {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary, #ffffff);
    border: 1px solid rgba(255, 255, 255, 0.04);
  }
  .btn-session:hover {
    background: rgba(255, 255, 255, 0.12);
  }
  .btn-session:active {
    transform: scale(0.97);
  }

  .btn-deny {
    background: rgba(239, 68, 68, 0.15);
    color: #f87171;
    border: 1px solid rgba(239, 68, 68, 0.1);
  }
  .btn-deny:hover {
    background: rgba(239, 68, 68, 0.22);
  }
  .btn-deny:active {
    transform: scale(0.97);
  }

  .btn-allow {
    background: #007aff;
    color: #ffffff;
    box-shadow: 0 2px 4px rgba(0, 122, 255, 0.15);
  }
  .btn-allow:hover {
    background: #1a88ff;
  }
  .btn-allow:active {
    transform: scale(0.97);
  }

  @keyframes pulse-glow {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.6; }
  }

  @keyframes fadeInStep {
    from { opacity: 0; transform: translateY(2px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .step-thinking-inline {
    padding: 8px 12px;
    font-size: 14px;
    line-height: 1.65;
    color: var(--text-secondary);
    white-space: pre-wrap;
    word-wrap: break-word;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  }

  .step-tool .step-label {
    color: var(--accent);
  }

  /* Skill steps use purple instead of the default accent color */
  .step-skill .step-tool-icon {
    color: rgba(139, 92, 246, 0.85);
  }

  .step-skill .step-label {
    color: rgba(139, 92, 246, 0.85);
  }

  .step-content {
    padding: 8px 12px 10px 32px;
    font-size: 12px;
    line-height: 1.5;
    border-top: 1px solid rgba(255, 255, 255, 0.04);
  }

  .step-tool-content pre,
  .step-result-content pre {
    margin: 0;
    padding: 8px;
    background: rgba(0, 0, 0, 0.3);
    border-radius: 6px;
    font-size: 11px;
    font-family: var(--font-mono);
    overflow-x: auto;
    color: var(--text-secondary);
    white-space: pre-wrap;
    word-wrap: break-word;
    max-height: 200px;
    overflow-y: auto;
  }

  .step-result-header {
    font-weight: 500;
    color: var(--text-muted);
    margin-bottom: 4px;
  }

  .step-result-content {
    border-top: none;
    padding-top: 0;
  }

  /* Agent Response */
  .agent-response {
    font-size: 14px;
    line-height: 1.65;
    color: var(--text-primary);
    word-wrap: break-word;
    overflow-wrap: break-word;
    margin-bottom: 4px;
  }

  .agent-response :global(p) {
    margin: 0 0 8px 0;
  }

  .agent-response :global(p:last-child) {
    margin-bottom: 0;
  }

  /* ─── Code Block Wrapper ─── */
  .agent-response :global(.code-block-wrapper) {
    margin: 10px 0;
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    background: rgba(0, 0, 0, 0.3);
  }

  .agent-response :global(.code-block-header) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    background: rgba(255, 255, 255, 0.04);
    border-bottom: 1px solid var(--border);
  }

  .agent-response :global(.code-block-lang) {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    text-transform: lowercase;
    font-family: var(--font-mono);
  }

  .agent-response :global(.code-copy-btn) {
    display: flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 11px;
    font-family: inherit;
    padding: 2px 6px;
    border-radius: 4px;
    transition: all 0.15s ease;
  }

  .agent-response :global(.code-copy-btn:hover) {
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.08);
  }

  .agent-response :global(.code-block-wrapper pre) {
    margin: 0;
    padding: 14px 16px;
    overflow-x: auto;
    font-size: 13px;
    background: none;
    border: none;
    border-radius: 0;
  }

  .agent-response :global(pre) {
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
    overflow-x: auto;
    margin: 10px 0;
    font-size: 13px;
  }

  .agent-response :global(code) {
    font-family: var(--font-mono);
    font-size: 0.88em;
  }

  .agent-response :global(p code),
  .agent-response :global(li code) {
    background: rgba(255, 255, 255, 0.06);
    padding: 2px 6px;
    border-radius: 4px;
    border: 1px solid rgba(255, 255, 255, 0.06);
  }

  .agent-response :global(pre code) {
    background: none;
    padding: 0;
    border: none;
  }

  /* ─── Message Actions ─── */
  .message-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 16px;
  }

  .msg-copy-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    font-family: inherit;
    padding: 4px 8px;
    border-radius: 6px;
    transition: all 0.15s ease;
  }

  .msg-copy-btn:hover {
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.06);
  }

  .agent-response :global(ul),
  .agent-response :global(ol) {
    margin: 8px 0;
    padding-left: 24px;
  }

  .agent-response :global(a) {
    color: var(--accent);
    text-decoration: none;
  }

  .agent-response :global(a:hover) {
    text-decoration: underline;
  }

  /* File Cards */
  .file-cards {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 16px;
  }

  /* ─── Chat Input ─── */
  .workspace-input-area {
    padding: 0 24px 20px;
    flex-shrink: 0;
  }

  .workspace-input-container {
    max-width: 680px;
    margin: 0 auto;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 16px;
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
    position: relative;
  }

  .workspace-input-container:focus-within {
    border-color: var(--border-light);
    box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.03);
  }

  .composer-text-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 14px 16px 6px;
  }

  .workspace-input-container textarea {
    --wails-draggable: no-drag;
    display: block;
    width: 100%;
    flex: 1;
    min-width: 0;
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
    padding: 0;
  }

  .workspace-input-container textarea::placeholder {
    color: var(--text-muted);
  }

  .workspace-input-container textarea:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .workspace-input-container.drag-over {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-glow);
    background: rgba(14, 240, 216, 0.03);
  }

  .workspace-input-bottom {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 8px 8px;
  }

  .workspace-input-bottom-left {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .input-divider {
    width: 1px;
    height: 20px;
    background: var(--border);
    margin: 0 2px;
    flex-shrink: 0;
  }

  .workspace-input-bottom-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn-attach {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.15s ease;
  }

  .btn-attach:hover:not(:disabled) {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }

  .btn-attach:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .btn-slash {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
    font-size: 18px;
    line-height: 1;
    font-weight: 600;
  }

  .btn-slash:hover:not(:disabled) {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }

  .btn-slash:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .skill-menu {
    position: absolute;
    left: 10px;
    right: 10px;
    bottom: calc(100% + 8px);
    max-height: 260px;
    overflow-y: auto;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 16px 36px rgba(0,0,0,0.32);
    padding: 6px;
    z-index: 10;
  }

  .skill-menu-header {
    padding: 5px 8px 7px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .skill-menu-empty {
    padding: 12px 8px;
    color: var(--text-muted);
    font-size: 13px;
  }

  .skill-option {
    display: flex;
    flex-direction: column;
    width: 100%;
    gap: 2px;
    padding: 8px 10px;
    background: none;
    border: none;
    border-radius: 8px;
    color: var(--text-secondary);
    text-align: left;
  }

  .skill-option:hover,
  .skill-option.highlighted {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .skill-option-name {
    font-size: 13px;
    font-weight: 600;
  }

  .skill-option-desc {
    color: var(--text-muted);
    font-size: 12px;
    line-height: 1.35;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  /* ─── File Preview ─── */
  .file-preview-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 12px 14px 0;
  }

  .file-chip {
    display: flex;
    align-items: center;
    gap: 6px;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 4px 8px;
    max-width: 220px;
    animation: chipFadeIn 0.15s ease;
  }

  .skill-chip {
    display: flex;
    align-items: center;
    gap: 6px;
    background: var(--accent-bg);
    border: 1px solid rgba(45, 212, 191, 0.35);
    border-radius: 8px;
    padding: 5px 8px;
    max-width: 260px;
    color: var(--accent);
    animation: chipFadeIn 0.15s ease;
    flex-shrink: 0;
  }

  .skill-chip span {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .skill-chip-remove {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    background: transparent;
    border: none;
    border-radius: 50%;
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
  }

  .skill-chip-remove:hover {
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.08);
  }

  @keyframes chipFadeIn {
    from { opacity: 0; transform: scale(0.95); }
    to   { opacity: 1; transform: scale(1); }
  }

  .file-thumb {
    width: 28px;
    height: 28px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }

  .file-icon {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.04);
    border-radius: 4px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .file-name {
    font-size: 12px;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100px;
  }

  .file-size {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .file-remove {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    background: transparent;
    border: none;
    border-radius: 50%;
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.1s ease;
  }

  .file-remove:hover {
    color: #f87171;
    background: rgba(248, 113, 113, 0.1);
  }

  .hidden-file-input {
    position: absolute;
    width: 0;
    height: 0;
    opacity: 0;
    pointer-events: none;
  }

  .btn-send {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--accent);
    border: none;
    border-radius: 50%;
    color: #000;
    cursor: pointer;
    flex-shrink: 0;
    transition: all 0.15s ease;
  }

  .btn-send:hover:not(:disabled) {
    background: var(--accent-hover);
    transform: scale(1.05);
  }

  .btn-send:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .btn-cancel {
    background: var(--danger);
  }

  .btn-cancel:hover {
    background: #dc2626;
  }

  .btn-integrations-toggle {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.07);
    border-radius: 50%;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.22s cubic-bezier(0.16, 1, 0.3, 1);
  }
  .btn-integrations-toggle:hover {
    background: var(--accent, #007aff);
    color: #ffffff;
    border-color: var(--accent, #007aff);
    transform: translateY(-1px) scale(1.08);
    box-shadow: 0 4px 12px rgba(0, 122, 255, 0.3);
  }
  .btn-integrations-toggle.active {
    background: var(--accent, #007aff);
    color: #ffffff;
    border-color: var(--accent, #007aff);
    transform: scale(0.95);
    box-shadow: 0 2px 6px rgba(0, 122, 255, 0.2);
  }

  /* Integrations Menu Popover */
  .integrations-menu {
    position: absolute;
    left: 10px;
    bottom: calc(100% + 8px);
    width: 320px;
    background: rgba(24, 24, 27, 0.88);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 14px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.45);
    padding: 14px;
    z-index: 100;
    display: flex;
    flex-direction: column;
    gap: 12px;
    animation: integrationsMenuFadeIn 0.22s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  @keyframes integrationsMenuFadeIn {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.97);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .integrations-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 2px;
  }

  .integrations-title-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-primary, #ffffff);
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.2px;
  }

  .integrations-header-icon {
    color: #10b981;
  }

  .btn-add-integration {
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: 6px;
    transition: all 0.12s ease;
  }

  .btn-add-integration:hover {
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.06);
  }

  /* Integrations Search */
  .integrations-search-wrap {
    position: relative;
  }

  .integrations-search-input {
    width: 100%;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 8px;
    padding: 7px 10px;
    color: var(--text-primary, #ffffff);
    font-family: inherit;
    font-size: 13px;
    transition: all 0.15s ease;
  }

  .integrations-search-input:focus {
    outline: none;
    border-color: rgba(59, 130, 246, 0.4);
    background: rgba(0, 0, 0, 0.2);
    box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.15);
  }

  .integrations-search-input::placeholder {
    color: var(--text-muted);
    opacity: 0.8;
  }

  /* Integrations List */
  .integrations-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 220px;
    overflow-y: auto;
  }

  .integrations-empty {
    padding: 16px;
    text-align: center;
    color: var(--text-muted);
    font-size: 12.5px;
  }

  .integration-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 8px;
    transition: background 0.12s ease;
  }

  .integration-item:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .integration-info {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .integration-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 6px;
    background: rgba(59, 130, 246, 0.12);
    color: var(--accent);
    flex-shrink: 0;
  }

  .integration-icon.doc-icon {
    background: rgba(16, 185, 129, 0.12);
    color: #10b981;
  }

  .integration-icon.web-icon {
    background: rgba(59, 130, 246, 0.12);
    color: #3b82f6;
  }

  .integration-icon.screen-icon {
    background: rgba(139, 92, 246, 0.12);
    color: #8b5cf6;
  }

  .integration-name {
    font-family: var(--font-mono, monospace);
    font-size: 13px;
    color: var(--text-secondary, #e5e7eb);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .integration-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn-integration-more {
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: 6px;
    transition: all 0.12s ease;
  }

  .btn-integration-more:hover {
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.06);
  }

  /* iOS Style Premium Toggle Switch */
  .toggle-switch {
    position: relative;
    display: inline-block;
    width: 36px;
    height: 20px;
    flex-shrink: 0;
  }

  .toggle-switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  .toggle-slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(255, 255, 255, 0.15);
    transition: 0.25s cubic-bezier(0.16, 1, 0.3, 1);
    border-radius: 34px;
  }

  .toggle-slider:before {
    position: absolute;
    content: "";
    height: 16px;
    width: 16px;
    left: 2px;
    bottom: 2px;
    background-color: white;
    transition: 0.25s cubic-bezier(0.16, 1, 0.3, 1);
    border-radius: 50%;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
  }

  .toggle-switch input:checked + .toggle-slider {
    background-color: var(--accent, #3b82f6);
  }

  .toggle-switch input:checked + .toggle-slider:before {
    transform: translateX(16px);
  }

  /* Chat Separation / Divider */
  .chat-divider {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    margin: 28px 0 20px;
    width: 100%;
  }

  .divider-line {
    flex: 1;
    height: 1px;
    background: linear-gradient(90deg, transparent, var(--border) 20%, var(--border) 80%, transparent);
    opacity: 0.45;
  }

  .divider-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--border);
    opacity: 0.35;
  }
</style>
