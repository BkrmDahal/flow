<!-- Inline loading spinner shown during LLM streaming -->
<span class="loading-spinner">
  <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <circle
      cx="12" cy="12" r="10"
      stroke="currentColor"
      stroke-opacity="0.2"
      stroke-width="3"
    />
    <path
      d="M12 2a10 10 0 0 1 10 10"
      stroke="currentColor"
      stroke-width="3"
      stroke-linecap="round"
    />
  </svg>
</span>

<style>
  .loading-spinner {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1em;
    height: 1em;
    vertical-align: text-bottom;
    margin-left: 4px;
    color: var(--accent);
  }

  .loading-spinner svg {
    width: 100%;
    height: 100%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to   { transform: rotate(360deg); }
  }
</style>
