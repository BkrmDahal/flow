/**
 * Shared formatting utilities for tool calls and text truncation.
 * Trimmed from talcon — only covers the 3 v1 tools (read_file, write_file, run_bash).
 */

/**
 * Convert a snake_case tool name to a readable label.
 * e.g. "read_file" → "read file"
 */
export function formatToolName(name) {
  return (name || '').replace(/_/g, ' ');
}

/**
 * Truncate a string to `max` characters, appending "…" if truncated.
 */
export function truncate(s, max) {
  if (!s) return '';
  return s.length > max ? s.slice(0, max) + '…' : s;
}

/**
 * Shorten a file path to just the last two segments.
 * e.g. "/Users/me/project/src/main.go" → "…/src/main.go"
 */
export function shortPath(p) {
  if (!p) return '';
  const parts = p.split('/');
  return parts.length > 2
    ? '…/' + parts.slice(-2).join('/')
    : parts.join('/');
}

/**
 * Build a descriptive label for a tool call from its name + input JSON.
 * e.g. "read file — config.json", "run bash — ls -la"
 */
export function formatToolLabel(name, inputJson) {
  let detail = '';
  try {
    const input = JSON.parse(inputJson || '{}');
    switch (name) {
      case 'read_file':
        detail = shortPath(input.path);
        break;
      case 'write_file':
        detail = shortPath(input.path);
        break;
      case 'run_bash':
        detail = truncate(input.command, 50);
        break;
    }
  } catch { /* ignore parse errors */ }

  const label = formatToolName(name);
  return detail ? `${label} — ${detail}` : label;
}
