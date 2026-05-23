import { marked } from 'marked';
import DOMPurify from 'dompurify';

// Configure DOMPurify to allow safe markdown-rendered elements but
// strip anything dangerous (scripts, event handlers, iframes, etc.).
DOMPurify.setConfig({
  ALLOWED_TAGS: [
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'p', 'br', 'hr', 'blockquote',
    'ul', 'ol', 'li',
    'strong', 'em', 'b', 'i', 'u', 's', 'del', 'ins', 'mark',
    'code', 'pre', 'kbd', 'samp', 'var',
    'a', 'img',
    'table', 'thead', 'tbody', 'tr', 'th', 'td',
    'div', 'span', 'button',
    'svg', 'path', 'rect', 'circle', 'polyline', 'line', 'polygon',
    'sup', 'sub',
    'details', 'summary',
    'input',  // for task list checkboxes
  ],
  ALLOWED_ATTR: [
    'href', 'target', 'rel', 'src', 'alt', 'title',
    'class', 'id', 'data-code',
    'width', 'height', 'viewBox', 'fill', 'stroke', 'stroke-width',
    'stroke-linecap', 'stroke-linejoin',
    'd', 'x', 'y', 'rx', 'ry', 'cx', 'cy', 'r', 'x1', 'y1', 'x2', 'y2',
    'points',
    'type', 'checked', 'disabled', 'colspan', 'rowspan',
  ],
  ALLOW_DATA_ATTR: true, // allow data-* attributes (needed for code copy)
  ADD_ATTR: ['target'],  // allow target="_blank" on links
});

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
 * Safely render a markdown string to sanitized HTML.
 * All output is sanitized with DOMPurify to prevent XSS attacks
 * from LLM-generated content (e.g. prompt injection).
 */
export function renderMarkdown(text) {
  if (!text) return '';
  try {
    const rawHtml = marked.parse(text);
    return DOMPurify.sanitize(rawHtml);
  } catch (e) {
    console.error('Markdown parse error:', e);
    // Escape the raw text as a fallback to prevent XSS.
    return text
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }
}

