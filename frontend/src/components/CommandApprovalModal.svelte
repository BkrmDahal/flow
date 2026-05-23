<script>
  import { onMount, onDestroy } from 'svelte'
  import { Backend, Events } from '../lib/wails.js'
  import appIcon from '../assets/appicon.png'

  let activeRequest = null // { id, command, exe }

  onMount(() => {
    Events.on('command:request_approval', handleRequest)
  })

  onDestroy(() => {
    Events.off('command:request_approval')
  })

  function handleRequest(req) {
    if (req && req.id && req.command) {
      activeRequest = req
    }
  }

  async function submit(choice) {
    if (!activeRequest) return
    try {
      await Backend.SubmitCommandApproval(activeRequest.id, choice)
    } catch (e) {
      console.error('SubmitCommandApproval failed:', e)
    }
    activeRequest = null
  }
</script>

{#if activeRequest}
  <div class="overlay" on:click={() => submit('deny')} on:keydown={(e) => e.key === 'Escape' && submit('deny')} role="presentation">
    <div class="modal" on:click|stopPropagation on:keydown|stopPropagation role="dialog" aria-modal="true" aria-labelledby="modal-title">
      <div class="header">
        <img class="app-icon" src={appIcon} alt="Flow logo" />
        <h2 id="modal-title" class="title">Command Access Request</h2>
      </div>

      <div class="body">
        <p class="desc">Flow wants to run a terminal command that is not in your allowlist:</p>
        <div class="command-card">
          <code>{activeRequest.command}</code>
        </div>
        <p class="question">How would you like to handle this request?</p>
      </div>

      <div class="actions">
        <button class="btn-action btn-always" on:click={() => submit('always')} type="button">
          Allow Always
          <span class="btn-subtext">Add permanently</span>
        </button>
        <button class="btn-action btn-session" on:click={() => submit('session')} type="button">
          Allow Session
          <span class="btn-subtext">Temporary allow</span>
        </button>
        <button class="btn-action btn-deny" on:click={() => submit('deny')} type="button">
          Block
          <span class="btn-subtext">Reject execution</span>
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 99999;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    animation: fadeIn 0.2s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  .modal {
    width: 360px;
    padding: 24px;
    background: rgba(30, 30, 30, 0.75);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 24px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.4);
    color: var(--text-primary, #ffffff);
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    animation: scaleIn 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) forwards;
  }

  .header {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    width: 100%;
    margin-bottom: 16px;
  }

  .app-icon {
    width: 64px;
    height: 64px;
    border-radius: 14px;
    object-fit: cover;
    box-shadow: 0 8px 16px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(255, 255, 255, 0.05);
  }

  .title {
    font-size: 16px;
    font-weight: 700;
    line-height: 1.3;
    margin: 0;
    color: var(--text-primary, #ffffff);
    letter-spacing: -0.2px;
  }

  .body {
    width: 100%;
    margin-bottom: 24px;
  }

  .desc {
    font-size: 13px;
    line-height: 1.45;
    color: var(--text-muted, #9ca3af);
    margin: 0 0 12px;
  }

  .command-card {
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 12px;
    padding: 12px 14px;
    margin-bottom: 14px;
    width: 100%;
    box-sizing: border-box;
    overflow-x: auto;
    word-break: break-all;
    text-align: left;
    max-height: 150px;
    overflow-y: auto;
  }

  .command-card code {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11.5px;
    color: var(--accent, #3b82f6);
    line-height: 1.4;
  }

  .question {
    font-size: 13px;
    font-weight: 500;
    line-height: 1.45;
    color: var(--text-secondary, #e5e7eb);
    margin: 0;
  }

  .actions {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  .btn-action {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 52px;
    border-radius: 14px;
    cursor: pointer;
    border: none;
    transition: all 0.15s ease;
  }

  .btn-subtext {
    font-size: 10px;
    font-weight: 500;
    opacity: 0.7;
    margin-top: 2px;
  }

  .btn-always {
    background: #007aff; /* Apple Blue / Brand Accent */
    color: #ffffff;
    font-size: 13.5px;
    font-weight: 700;
    box-shadow: 0 4px 12px rgba(0, 122, 255, 0.25);
  }

  .btn-always:hover {
    background: #1a88ff;
    box-shadow: 0 6px 16px rgba(0, 122, 255, 0.35);
    transform: translateY(-0.5px);
  }

  .btn-always:active {
    background: #0066d6;
    transform: scale(0.98);
  }

  .btn-session {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary, #ffffff);
    border: 1px solid rgba(255, 255, 255, 0.04);
    font-size: 13px;
    font-weight: 600;
  }

  .btn-session:hover {
    background: rgba(255, 255, 255, 0.12);
  }

  .btn-session:active {
    background: rgba(255, 255, 255, 0.06);
    transform: scale(0.98);
  }

  .btn-deny {
    background: rgba(239, 68, 68, 0.15);
    color: #f87171;
    border: 1px solid rgba(239, 68, 68, 0.1);
    font-size: 13px;
    font-weight: 600;
  }

  .btn-deny:hover {
    background: rgba(239, 68, 68, 0.22);
  }

  .btn-deny:active {
    background: rgba(239, 68, 68, 0.12);
    transform: scale(0.98);
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes scaleIn {
    from {
      opacity: 0;
      transform: scale(0.92);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }
</style>
