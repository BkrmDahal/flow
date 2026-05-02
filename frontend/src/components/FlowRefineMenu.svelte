<script>
  import { onMount, onDestroy } from 'svelte'
  import { Backend, Events } from '../lib/wails.js'

  // The currently displayed transcript object (FlowTranscript).
  export let transcript = null
  // Called after a refinement completes successfully — parent should reload
  // the transcript so the new entry appears in the refinements list.
  export let onRefined = () => {}

  const PRESETS = [
    { id: 'clean',     label: 'Clean' },
    { id: 'summarize', label: 'Summarize' },
    { id: 'bullets',   label: 'Bullets' },
    { id: 'custom',    label: 'Custom…' },
  ]

  let busy = false
  let liveText = ''
  let activeAction = ''
  let errorMsg = ''
  let customOpen = false
  let customPrompt = ''
  let modelMissing = false

  onMount(async () => {
    Events.on('flow:refine:event', (payload) => {
      if (!payload || payload.transcriptId !== transcript?.id) return
      if (payload.kind === 'start') liveText = ''
      else if (payload.kind === 'delta') liveText += payload.text
      else if (payload.kind === 'done') {
        liveText = payload.text
        busy = false
        onRefined()
      } else if (payload.kind === 'error') {
        errorMsg = payload.text || 'Unknown refine error'
        busy = false
      }
    })
    try {
      const s = await Backend.GetSettings()
      modelMissing = !s?.model
    } catch (_) {
      modelMissing = true
    }
  })

  onDestroy(() => Events.off('flow:refine:event'))

  async function run(actionId) {
    if (!transcript || busy) return
    if (actionId === 'custom' && !customOpen) {
      customOpen = true
      return
    }
    errorMsg = ''
    liveText = ''
    activeAction = actionId
    busy = true
    try {
      await Backend.RefineFlowText(transcript.id, actionId, actionId === 'custom' ? customPrompt : '')
      if (actionId === 'custom') customOpen = false
    } catch (e) {
      errorMsg = String(e?.message ?? e)
      busy = false
    }
  }

  function copy(text) {
    navigator.clipboard?.writeText(text)
  }

  $: if (transcript) {
    // Reset preview when the user switches to a different transcript.
    liveText = ''
    activeAction = ''
    errorMsg = ''
  }
</script>

<div class="refine">
  <div class="header">
    <span class="title">Refine with LLM</span>
  </div>
  {#if modelMissing}
    <p class="hint">Configure a local model in Settings to enable refining.</p>
  {:else}
    <div class="chips">
      {#each PRESETS as p}
        <button
          class="chip"
          class:active={activeAction === p.id && busy}
          disabled={busy && activeAction !== p.id}
          on:click={() => run(p.id)}
        >
          {p.label}
        </button>
      {/each}
    </div>

    {#if customOpen}
      <div class="custom">
        <textarea
          rows="2"
          placeholder="e.g. Translate to Spanish, formal tone"
          bind:value={customPrompt}
        />
        <div class="custom-actions">
          <button class="secondary" on:click={() => (customOpen = false)}>Cancel</button>
          <button class="primary" on:click={() => run('custom')} disabled={!customPrompt.trim() || busy}>
            Run
          </button>
        </div>
      </div>
    {/if}

    {#if busy || liveText || errorMsg}
      <div class="output">
        {#if errorMsg}
          <div class="error">✗ {errorMsg}</div>
        {:else}
          <div class="output-head">
            <span class="badge">{activeAction}</span>
            {#if !busy && liveText}
              <button class="copy" on:click={() => copy(liveText)}>Copy</button>
            {/if}
          </div>
          <div class="output-text">{liveText}{busy ? '▍' : ''}</div>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  .refine {
    margin-top: 16px;
    padding-top: 12px;
    border-top: 1px solid #1f1f29;
  }
  .header { margin-bottom: 10px; }
  .title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #9ca3af;
  }
  .chips {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .chip {
    background: #16161d;
    color: #c0c0c8;
    border: 1px solid #1f1f29;
    border-radius: 999px;
    padding: 6px 14px;
    font-size: 12px;
  }
  .chip:hover { background: #1c1c26; }
  .chip.active { background: #2c2c4a; border-color: #6366f1; color: #ffffff; }
  .chip:disabled { opacity: 0.4; cursor: not-allowed; }

  .custom {
    margin-top: 10px;
    background: #11111a;
    border: 1px solid #1f1f29;
    border-radius: 8px;
    padding: 10px;
  }
  textarea {
    width: 100%;
    background: #0d0d13;
    color: #e6e6e6;
    border: 1px solid #1f1f29;
    border-radius: 6px;
    padding: 8px;
    font-family: inherit;
    font-size: 13px;
    resize: vertical;
  }
  .custom-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }
  .secondary, .primary {
    border: 1px solid #2a2a36;
    border-radius: 6px;
    padding: 5px 12px;
    font-size: 12px;
  }
  .secondary { background: transparent; color: #9ca3af; }
  .secondary:hover { color: #e6e6e6; background: #16161d; }
  .primary { background: #6366f1; color: #ffffff; border-color: #6366f1; }
  .primary:disabled { opacity: 0.5; cursor: not-allowed; }

  .output {
    margin-top: 12px;
    background: #11111a;
    border: 1px solid #1f1f29;
    border-radius: 8px;
    padding: 12px;
  }
  .output-head {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 6px;
  }
  .badge {
    background: #1f1f29;
    color: #c4b5fd;
    border-radius: 6px;
    padding: 2px 8px;
    font-size: 11px;
    text-transform: capitalize;
  }
  .copy {
    margin-left: auto;
    background: transparent;
    border: 1px solid #2a2a36;
    color: #9ca3af;
    border-radius: 6px;
    padding: 3px 10px;
    font-size: 11px;
  }
  .copy:hover { color: #e6e6e6; background: #1c1c26; }
  .output-text {
    white-space: pre-wrap;
    line-height: 1.5;
  }
  .error { color: #f87171; font-size: 13px; }
  .hint {
    color: #9ca3af;
    background: #11111a;
    border: 1px dashed #1f1f29;
    border-radius: 8px;
    padding: 12px;
    font-size: 12px;
  }
</style>
