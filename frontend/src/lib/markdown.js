import { marked } from 'marked';

// Custom renderer that wraps fenced code blocks in a container with a
// language label and a copy button.
const renderer = {
  code(text, lang) {
    const language = lang || '';
    const displayLang = language || 'code';
    const codeText = text || '';
    const escaped = codeText
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');

    return `<div class="code-block-wrapper">
  <div class="code-block-header">
    <span class="code-block-lang">${displayLang}</span>
    <button class="code-copy-btn" data-code="${escaped}" title="Copy code">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>
        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
      </svg>
      <span class="code-copy-label">Copy</span>
    </button>
  </div>
  <pre><code class="language-${language}">${escaped}</code></pre>
</div>`;
  }
};

marked.use({
  breaks: true,
  gfm: true,
  renderer,
});

/**
 * Safely render a markdown string to HTML.
 * Returns the raw text on failure.
 */
export function renderMarkdown(text) {
  if (!text) return '';
  try {
    return marked.parse(text);
  } catch (e) {
    console.error('Markdown parse error:', e);
    return text;
  }
}
