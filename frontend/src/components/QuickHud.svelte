<script>
  import { onMount, tick } from 'svelte';
  import { renderMarkdown } from '../lib/markdown.js';

  // ── HUD state ──
  let phase = 'idle';        // idle | running | awaiting_approval | done | error
  let expanded = true;       // collapsed = compact pill while working; expanded = full chat
  let sessionId = '';
  let userRequest = '';
  let answer = '';           // accumulated assistant text deltas
  let status = '';           // raw activity (kept for back-compat)
  let stepTitle = '';        // collapsed pill: current step heading
  let stepSubtitle = '';     // collapsed pill: current step detail
  let errorMsg = '';
  let approval = null;       // { id, command, exe, kind }
  let suggestions = [];
  let suggestionsLoading = false;
  let inputText = '';
  let scrollEl;
  let rootEl;

  const cloudSvg = '<svg viewBox="0 0 24 16" fill="currentColor"><path d="M18.5 15a4.5 4.5 0 0 0 .9-8.9A6 6 0 0 0 7.7 5 4.5 4.5 0 0 0 5 15h13.5z"/></svg>';

  const toolLabels = {
    run_bash: 'Running a command',
    web_search: 'Searching the web',
    fetch_url: 'Reading a page',
    write_file: 'Writing a file',
    read_file: 'Reading a file',
    capture_screen: 'Looking at the screen',
    save_memory: 'Saving to memory',
    memory_search: 'Recalling memory',
    todo_write: 'Planning the steps',
    use_skill: 'Loading a skill',
  };
  const friendlyTool = (n) => toolLabels[n] || `Running ${n || 'a tool'}`;

  function toolDetail(toolInput) {
    if (!toolInput) return '';
    let o;
    try { o = JSON.parse(toolInput); } catch { return String(toolInput).replace(/\s+/g, ' ').slice(0, 90); }
    const v = o.command || o.query || o.url || o.path || o.name || o.prompt || '';
    return String(v).replace(/\s+/g, ' ').slice(0, 90);
  }
  const lastLine = (t) => (t || '').trim().split('\n').filter(Boolean).pop()?.slice(0, 110) || '';

  // ── Layout / resize: tell the native panel how tall to be. ──
  let resizeRAF = 0;
  function reportHeight() {
    if (!rootEl) return;
    cancelAnimationFrame(resizeRAF);
    resizeRAF = requestAnimationFrame(() => {
      const h = Math.ceil(rootEl.getBoundingClientRect().height);
      post('/api/hud/resize', { height: h });
    });
  }

  async function afterRender() {
    await tick();
    if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
    reportHeight();
  }

  // ── SSE ──
  function connect() {
    const es = new EventSource('/api/hud/events');
    es.onmessage = (e) => {
      let ev; try { ev = JSON.parse(e.data); } catch { return; }
      handleEvent(ev);
    };
    es.onerror = () => {};
  }

  function resetForNew() {
    userRequest = ''; answer = ''; errorMsg = ''; approval = null;
    suggestions = []; suggestionsLoading = false;
    stepTitle = ''; stepSubtitle = ''; status = '';
  }

  const voiceActive = () => phase === 'listening' || phase === 'transcribing' || phase === 'running';

  function handleEvent(ev) {
    switch (ev.type) {
      case 'listening':
        resetForNew();
        phase = 'listening';
        expanded = false;
        break;
      case 'transcribing':
        phase = 'transcribing';
        expanded = false;
        break;
      case 'cancelled':
        resetForNew();
        phase = 'idle';
        break;
      case 'session':
        sessionId = ev.session_id || sessionId;
        // Don't reset the UI mid-voice-flow (would flash help/empty). Only a
        // tap / plain-open (no active voice flow) resets to idle.
        if (!voiceActive()) {
          resetForNew();
          // Suggestions loader only when suggestions are actually coming (tap).
          suggestionsLoading = !ev.session_id && ev.suggest !== false;
          phase = 'idle';
          expanded = true;
        }
        break;
      case 'user':
        userRequest = ev.content || '';
        resetForNew();
        userRequest = ev.content || '';
        phase = 'running';
        expanded = false;                       // collapse to the step pill
        stepTitle = 'Thinking';
        break;
      case 'suggestions_loading':
        suggestionsLoading = true;
        break;
      case 'suggestions':
        suggestions = ev.items || [];
        suggestionsLoading = false;
        break;
      case 'thinking_start':
      case 'thinking':
        if (phase === 'running' && !answer && !stepSubtitle) stepTitle = 'Thinking';
        break;
      case 'text':
        answer += ev.content || '';
        status = '';
        if (phase === 'running') {
          if (stepTitle === 'Thinking') stepTitle = 'Working on it';
          const ll = lastLine(answer);
          if (ll) stepSubtitle = ll;
        }
        break;
      case 'tool_call':
        stepTitle = friendlyTool(ev.tool_name);
        stepSubtitle = toolDetail(ev.tool_input);
        status = stepTitle;
        break;
      case 'tool_result':
        status = '';
        break;
      case 'skill_used':
        stepTitle = `Loaded skill ${ev.tool_name || ''}`;
        break;
      case 'file_created':
        stepTitle = 'Saving a file';
        stepSubtitle = ev.name || ev.content || '';
        break;
      case 'command:request_approval':
        approval = { id: ev.id, command: ev.command, exe: ev.exe, kind: 'command' };
        phase = 'awaiting_approval';
        expanded = false;                       // approvals show as a compact card
        break;
      case 'sandbox:request_approval':
        approval = { id: ev.id, command: ev.path || ev.command, exe: '', kind: 'sandbox' };
        phase = 'awaiting_approval';
        expanded = false;
        break;
      case 'done':
        if (ev.final_text) answer = ev.final_text;
        phase = 'done';
        expanded = true;                        // show the whole chat
        stepTitle = ''; stepSubtitle = '';
        break;
      case 'error':
        errorMsg = ev.content || ev.error || 'Something went wrong.';
        phase = 'error';
        expanded = true;
        break;
    }
    afterRender();
  }

  // ── JS → Go ──
  async function post(path, body) {
    try {
      await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
    } catch (e) { console.warn('hud post failed', path, e); }
  }
  function submitInput() {
    const text = inputText.trim();
    if (!text) return;
    inputText = '';
    post('/api/hud/ask', { text });
  }
  const runSuggestion = (text) => post('/api/hud/ask', { text });
  function approve(choice) {
    if (!approval) return;
    post('/api/hud/approve', { id: approval.id, choice });
    approval = null;
    phase = 'running';
    stepTitle = 'Continuing…';
    stepSubtitle = '';
    afterRender();
  }
  const openInApp = () => post('/api/hud/open', { session_id: sessionId });
  const dismiss = () => post('/api/hud/dismiss', {});
  const voiceCancel = () => post('/api/hud/voice-cancel', {});
  const voiceConfirm = () => post('/api/hud/voice-confirm', {});
  const newSession = () => post('/api/hud/new', {});
  const cancelStream = () => post('/api/hud/cancel', {});

  let copied = false;
  let copiedTimer;
  async function copyAnswer() {
    try {
      await navigator.clipboard.writeText(answer);
      copied = true;
      clearTimeout(copiedTimer);
      copiedTimer = setTimeout(() => (copied = false), 1500);
    } catch (e) { console.warn('copy failed', e); }
  }

  function onKeydown(e) {
    if (e.key === 'Escape') { dismiss(); return; }
    if (e.key === 'Enter' && !e.shiftKey && document.activeElement?.tagName === 'INPUT') {
      e.preventDefault();
      submitInput();
    }
  }

  const helpGroups = [
    { title: 'Ask about your screen', examples: ['Summarize this page', 'Explain this code', 'What does this error mean?'] },
    { title: 'Write & rewrite', examples: ['Reply to this email', 'Make this more concise', 'Fix grammar and tone'] },
    { title: 'Do things across apps', examples: ['Save this as a note', 'Draft a reply in Mail', 'Open this link in my browser'] },
    { title: 'Quick answers', examples: ["What's 15% of 240?", 'Convert 30°C to °F', 'Define “idempotent”'] },
  ];

  $: showHelp = expanded && phase === 'idle'
    && !answer && !errorMsg && !approval && !suggestionsLoading && suggestions.length === 0;

  onMount(() => {
    connect();
    window.addEventListener('keydown', onKeydown);
    const ro = new ResizeObserver(() => reportHeight());
    if (rootEl) ro.observe(rootEl);
    reportHeight();
    return () => { window.removeEventListener('keydown', onKeydown); ro.disconnect(); };
  });
</script>

<div class="hud" class:compact={!expanded} bind:this={rootEl}>
  {#if !expanded && phase === 'listening'}
    <!-- Listening: cloud + live waveform + cancel / send -->
    <div class="pill listening-pill">
      <span class="pill-icon busy">{@html cloudSvg}</span>
      <div class="wave">
        {#each Array(9) as _, i}<span style="--i:{i}"></span>{/each}
      </div>
      <div class="pill-actions">
        <button class="round-btn cancel" title="Cancel (Esc)" on:click={voiceCancel}>✕</button>
        <button class="round-btn confirm" title="Send" on:click={voiceConfirm}>✓</button>
      </div>
    </div>

  {:else if !expanded && phase === 'transcribing'}
    <!-- Transcribing -->
    <div class="pill">
      <span class="pill-icon busy">{@html cloudSvg}</span>
      <div class="pill-text"><div class="pill-title">Transcribing…</div></div>
      <span class="spinner"></span>
      <button class="round-btn cancel" title="Cancel" on:click={voiceCancel}>✕</button>
    </div>

  {:else if !expanded && approval}
    <!-- Compact approval card -->
    <div class="pill approval-pill">
      <span class="pill-icon">{@html cloudSvg}</span>
      <div class="pill-text">
        <div class="pill-title">Needs approval</div>
        <div class="pill-sub mono">{approval.command}</div>
      </div>
      <div class="pill-actions">
        <button class="approve-btn" on:click={() => approve(approval.kind === 'sandbox' ? 'approve' : 'once')}>Approve</button>
        <button class="deny-btn" title="Deny (Esc)" on:click={() => approve('deny')}>✕</button>
      </div>
    </div>

  {:else if !expanded}
    <!-- Compact pill: current step while running, or a collapsed result when done -->
    <div class="pill step-pill-row">
      <button class="pill step-pill" on:click={() => { expanded = true; afterRender(); }} title="Show details">
        <span class="pill-icon" class:busy={phase === 'running'}>{@html cloudSvg}</span>
        <div class="pill-text">
          <div class="pill-title">{stepTitle || (phase === 'done' ? (userRequest || 'Response ready') : 'Working on it')}</div>
          {#if stepSubtitle}
            <div class="pill-sub">{stepSubtitle}</div>
          {:else if phase === 'done'}
            <div class="pill-sub">Tap to show response</div>
          {/if}
        </div>
        {#if phase === 'running'}<span class="active-dot"></span>{/if}
      </button>
      {#if phase === 'running'}
        <button class="round-btn cancel step-cancel" title="Cancel" on:click={cancelStream}>✕</button>
      {/if}
    </div>

  {:else}
    <!-- Expanded full view -->
    <header class="hud-head">
      <span class="head-icon" class:busy={phase === 'running'}>{@html cloudSvg}</span>
      <span class="title">{userRequest || 'Quick Ask'}</span>
      <div class="head-actions">
        {#if phase === 'running'}
          <button class="head-btn icon-btn cancel-btn" on:click={cancelStream} title="Cancel" aria-label="Cancel">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>
          </button>
        {/if}
        <button class="head-btn icon-btn" on:click={newSession} title="New chat" aria-label="New chat">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        </button>
        <button class="head-btn icon-btn" on:click={openInApp} title="Open in main app" aria-label="Open in main app" disabled={!sessionId}>
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
        <button class="head-btn icon-btn" on:click={dismiss} title="Dismiss (Esc)" aria-label="Dismiss">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
    </header>

    <div class="hud-body" bind:this={scrollEl}>
      {#if errorMsg}
        <div class="error">{errorMsg}</div>
      {/if}

      {#if suggestions.length > 0 && !answer}
        <div class="chips">
          {#each suggestions as s}
            <button class="chip" on:click={() => runSuggestion(s.prompt || s.label || s)}>
              <span class="chip-label">{s.label || s}</span>
              {#if s.detail}<span class="chip-detail">{s.detail}</span>{/if}
            </button>
          {/each}
        </div>
      {/if}

      {#if suggestionsLoading && !answer}
        <div class="status"><span class="spinner"></span>Looking at your screen…</div>
      {/if}

      {#if showHelp}
        <div class="help">
          <p class="help-lead">Hold your Quick&nbsp;Ask key to talk, or type below. I can see what's on your screen and act across your apps.</p>
          <div class="help-groups">
            {#each helpGroups as g}
              <div class="help-group">
                <div class="help-group-title">{g.title}</div>
                <ul class="help-examples">
                  {#each g.examples as ex}
                    <li><button class="help-example" on:click={() => runSuggestion(ex)}>{ex}</button></li>
                  {/each}
                </ul>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if answer}
        <div class="answer">{@html renderMarkdown(answer)}</div>
        {#if phase !== 'running'}
          <button class="copy-btn" on:click={copyAnswer} title="Copy response">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
            {copied ? 'Copied' : 'Copy'}
          </button>
        {/if}
      {/if}

      {#if phase === 'running' && !answer && !approval}
        <div class="status"><span class="spinner"></span>{stepTitle || 'Working on it'}</div>
        {#if stepSubtitle}<div class="status-sub">{stepSubtitle}</div>{/if}
      {:else if status && phase === 'running'}
        <div class="status"><span class="spinner"></span>{status}</div>
      {/if}

      {#if approval}
        <div class="approval">
          <div class="approval-label">{approval.kind === 'sandbox' ? 'Allow folder access?' : 'Run this command?'}</div>
          <code class="approval-cmd">{approval.command}</code>
          <div class="approval-actions">
            {#if approval.kind === 'sandbox'}
              <button class="btn primary" on:click={() => approve('approve')}>Approve</button>
              <button class="btn" on:click={() => approve('deny')}>Deny</button>
            {:else}
              <button class="btn primary" on:click={() => approve('once')}>Approve</button>
              <button class="btn" on:click={() => approve('session')}>Allow for session</button>
              <button class="btn" on:click={() => approve('deny')}>Deny</button>
            {/if}
          </div>
        </div>
      {/if}
    </div>

    <footer class="hud-foot">
      <div class="input-wrap">
        <input class="hud-input" placeholder="Type a message…" bind:value={inputText} autocomplete="off" />
        <button class="send" on:click={submitInput} disabled={!inputText.trim()} aria-label="Send">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <line x1="12" y1="19" x2="12" y2="5" />
            <polyline points="5 12 12 5 19 12" />
          </svg>
        </button>
      </div>
    </footer>
  {/if}
</div>

<style>
  :global(body) { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }

  .hud {
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    background: rgba(18, 18, 20, 0.94);
    color: #f4f4f5;
    border: 0.5px solid rgba(255, 255, 255, 0.08);
    border-radius: 18px;
    backdrop-filter: blur(24px);
    -webkit-backdrop-filter: blur(24px);
    overflow: hidden;
  }
  .hud.compact { border-radius: 16px; }

  /* ── Compact pill (step / approval) ── */
  .pill {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    box-sizing: border-box;
    padding: 14px 16px;
    background: none;
    border: none;
    text-align: left;
    color: inherit;
    cursor: default;
  }
  .step-pill { cursor: pointer; }
  .step-pill-row { display: flex; align-items: center; gap: 4px; padding-right: 10px; }
  .step-pill-row .step-pill { flex: 1; min-width: 0; }
  .step-cancel { width: 26px; height: 26px; font-size: 12px; }
  .pill-icon {
    flex: none;
    width: 30px; height: 30px;
    display: flex; align-items: center; justify-content: center;
    color: #d4d4d8;
  }
  .pill-icon :global(svg) { width: 26px; height: 18px; }
  .pill-icon.busy { color: #fff; filter: drop-shadow(0 0 6px rgba(120,170,255,0.5)); animation: glow 1.6s ease-in-out infinite; }
  @keyframes glow { 0%,100% { opacity: .75; } 50% { opacity: 1; } }
  .pill-text { flex: 1; min-width: 0; }
  .pill-title { font-size: 13.5px; font-weight: 600; color: #f4f4f5; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pill-sub { font-size: 12px; color: #9a9aa2; margin-top: 1px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pill-sub.mono { font-family: ui-monospace, SFMono-Regular, monospace; color: #c9b27a; }
  .active-dot { flex: none; width: 9px; height: 9px; border-radius: 50%; background: #4ade80; animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .35; } }

  /* ── Listening waveform ── */
  .listening-pill { gap: 14px; }
  .wave { flex: 1; display: flex; align-items: center; justify-content: center; gap: 4px; height: 28px; }
  .wave span {
    width: 3px; border-radius: 2px; background: #f4f4f5;
    height: 30%;
    animation: wave 0.9s ease-in-out infinite;
    animation-delay: calc(var(--i) * 0.08s);
  }
  @keyframes wave { 0%,100% { height: 18%; opacity: .6; } 50% { height: 92%; opacity: 1; } }

  .round-btn {
    flex: none; width: 30px; height: 30px; border-radius: 15px; border: none;
    font-size: 13px; cursor: pointer; display: flex; align-items: center; justify-content: center;
  }
  .round-btn.cancel { background: rgba(255,255,255,0.14); color: #f4f4f5; }
  .round-btn.cancel:hover { background: rgba(255,255,255,0.24); }
  .round-btn.confirm { background: #fff; color: #18181b; font-weight: 700; }
  .round-btn.confirm:hover { background: #e8e8ea; }

  /* Cross-fade between states */
  .pill, .hud-head, .hud-body, .hud-foot { animation: fadein 0.18s ease; }
  @keyframes fadein { from { opacity: 0; } to { opacity: 1; } }

  .pill-actions { flex: none; display: flex; align-items: center; gap: 6px; }
  .approve-btn {
    background: #fff; color: #18181b; border: none;
    padding: 7px 16px; border-radius: 16px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .approve-btn:hover { background: #e8e8ea; }
  .deny-btn {
    background: rgba(255,255,255,0.08); color: #a1a1aa; border: none;
    width: 28px; height: 28px; border-radius: 14px; font-size: 12px; cursor: pointer;
  }
  .deny-btn:hover { background: rgba(255,255,255,0.16); color: #fff; }

  /* ── Expanded view ── */
  .hud-head {
    display: flex; align-items: center; gap: 8px;
    padding: 12px 14px;
    border-bottom: 0.5px solid rgba(255, 255, 255, 0.06);
  }
  .head-icon { width: 22px; height: 16px; color: #d4d4d8; display: flex; align-items: center; }
  .head-icon :global(svg) { width: 22px; height: 16px; }
  .head-icon.busy { color: #fff; animation: glow 1.6s ease-in-out infinite; }
  .title { flex: 1; font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; color: #e4e4e7; }
  .head-btn { background: transparent; border: none; color: #a1a1aa; font-size: 12px; cursor: pointer; padding: 2px 6px; border-radius: 6px; }
  .head-btn:hover:not(:disabled) { background: rgba(255,255,255,0.08); color: #fff; }
  .head-btn:disabled { opacity: 0.4; cursor: default; }
  .head-btn.cancel-btn:hover { background: rgba(248,113,113,0.18); color: #fca5a5; }
  .head-btn.icon-btn {
    width: 28px; height: 28px; padding: 0;
    display: inline-flex; align-items: center; justify-content: center;
  }
  .head-btn.icon-btn svg { display: block; }
  .head-actions { flex: none; display: flex; align-items: center; gap: 2px; }

  .hud-body { flex: 1; overflow-y: auto; max-height: 380px; padding: 14px; font-size: 13.5px; line-height: 1.5; }

  .chips { display: flex; flex-direction: column; gap: 8px; }
  .chip { text-align: left; background: rgba(255,255,255,0.05); border: 0.5px solid rgba(255,255,255,0.08); border-radius: 10px; padding: 10px 12px; cursor: pointer; color: #f4f4f5; display: flex; flex-direction: column; gap: 2px; }
  .chip:hover { background: rgba(255,255,255,0.1); }
  .chip-label { font-size: 13px; font-weight: 500; }
  .chip-detail { font-size: 11.5px; color: #a1a1aa; }

  .help { color: #d4d4d8; }
  .help-lead { margin: 2px 0 14px; font-size: 13px; line-height: 1.5; color: #c4c4c8; }
  .help-groups { display: grid; grid-template-columns: 1fr 1fr; gap: 12px 16px; }
  .help-group-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: #8b8b92; margin-bottom: 5px; }
  .help-examples { list-style: none; margin: 0; padding: 0; }
  .help-examples li { margin: 2px 0; }
  .help-example { background: none; border: none; padding: 2px 0; color: #e4e4e7; font-size: 12.5px; text-align: left; cursor: pointer; line-height: 1.35; }
  .help-example:hover { color: #7dd3fc; }

  .answer :global(p) { margin: 0 0 8px; }
  .answer :global(a) { color: #7dd3fc; }
  .answer :global(pre) { background: rgba(0,0,0,0.4); padding: 10px; border-radius: 8px; overflow-x: auto; font-size: 12px; }
  .answer :global(code) { font-family: ui-monospace, SFMono-Regular, monospace; }

  .copy-btn {
    display: inline-flex; align-items: center; gap: 5px;
    margin-top: 6px; padding: 4px 9px;
    background: rgba(255,255,255,0.06); border: 0.5px solid rgba(255,255,255,0.1);
    border-radius: 7px; color: #a1a1aa; font-size: 12px; cursor: pointer;
  }
  .copy-btn:hover { background: rgba(255,255,255,0.12); color: #f4f4f5; }

  .status { display: flex; align-items: center; gap: 8px; color: #a1a1aa; font-size: 12.5px; margin-top: 8px; }
  .status-sub { color: #71717a; font-size: 12px; margin-top: 4px; padding-left: 18px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .spinner { width: 12px; height: 12px; border-radius: 50%; border: 2px solid rgba(255,255,255,0.2); border-top-color: #fff; animation: spin 0.7s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .error { background: rgba(248, 113, 113, 0.12); border: 0.5px solid rgba(248,113,113,0.3); color: #fca5a5; padding: 10px 12px; border-radius: 8px; font-size: 12.5px; }

  .approval { margin-top: 12px; background: rgba(255,255,255,0.04); border: 0.5px solid rgba(255,255,255,0.08); border-radius: 10px; padding: 12px; }
  .approval-label { font-size: 12.5px; color: #e4e4e7; margin-bottom: 8px; }
  .approval-cmd { display: block; background: rgba(0,0,0,0.4); padding: 8px 10px; border-radius: 6px; font-size: 12px; font-family: ui-monospace, monospace; color: #fde68a; overflow-x: auto; margin-bottom: 10px; }
  .approval-actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .btn { background: rgba(255,255,255,0.08); border: none; color: #f4f4f5; padding: 6px 12px; border-radius: 7px; font-size: 12.5px; cursor: pointer; }
  .btn:hover { background: rgba(255,255,255,0.14); }
  .btn.primary { background: #fff; color: #18181b; font-weight: 600; }
  .btn.primary:hover { background: #e8e8ea; }

  .hud-foot { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-top: 0.5px solid rgba(255,255,255,0.06); }
  .input-wrap { position: relative; flex: 1; display: flex; align-items: center; }
  .hud-input { flex: 1; background: rgba(255,255,255,0.06); border: 0.5px solid rgba(255,255,255,0.1); border-radius: 9px; padding: 9px 38px 9px 12px; color: #f4f4f5; font-size: 13px; outline: none; }
  .hud-input:focus { border-color: rgba(96,165,250,0.6); }
  .send {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: rgba(255,255,255,0.1);
    border: none;
    border-radius: 7px;
    color: #f4f4f5;
    cursor: pointer;
    padding: 0;
    transition: background 0.15s ease;
  }
  .send:disabled { opacity: 0.35; cursor: default; background: rgba(255,255,255,0.06); }
  .send:hover:not(:disabled) { background: rgba(96,165,250,0.9); color: #fff; }
</style>
