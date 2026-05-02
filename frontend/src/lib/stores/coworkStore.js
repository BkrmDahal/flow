/**
 * Cowork store — manages agent task state, streaming, and session history.
 * Adapted from talcon's agentStore.js. Simplified for v1:
 *   - No background stream caching (single in-flight task)
 *   - No file attachment support
 *   - Event channel: cowork:stream:event
 */
import { writable, get } from 'svelte/store';
import { formatToolName } from '../utils/formatters.js';
import { Backend, Events } from '../wails.js';

// ─── Cowork state stores ───
export const coworkPhase = writable('welcome');        // 'welcome' | 'workspace'
export const coworkTaskHistory = writable([]);          // Array of { id, title, timestamp }
export const activeCoworkTaskId = writable(null);
export const coworkTaskTitle = writable('');
export const coworkMessages = writable([]);             // Array of { role, content, steps?, isStreaming? }
export const coworkStreamingIdx = writable(-1);
export const coworkCreatedFiles = writable([]);         // Files created during the task
export const coworkContextTools = writable([]);         // Distinct tool names used
export const coworkLoading = writable(false);
export const coworkIsStreaming = writable(false);

// Internal state
let _streamCleanup = null;
let _pendingContentReset = false;
let _lastSeenSeq = 0;
let _listenerRegistered = false;

// ─── Event listener management ───

function ensureListener() {
  if (!_listenerRegistered) {
    Events.off('cowork:stream:event');
    if (_streamCleanup) { _streamCleanup(); _streamCleanup = null; }
    _streamCleanup = Events.on('cowork:stream:event', handleStreamEvent);
    _listenerRegistered = true;
  }
}

function cleanupListener() {
  if (get(coworkStreamingIdx) < 0) {
    Events.off('cowork:stream:event');
    if (_streamCleanup) { _streamCleanup(); _streamCleanup = null; }
    _listenerRegistered = false;
  }
}

// ─── Stream event handler ───

function handleStreamEvent(data) {
  const activeId = get(activeCoworkTaskId);
  if (data.session_id && data.session_id !== activeId) return;
  if (!get(coworkIsStreaming)) return;
  const idx = get(coworkStreamingIdx);
  if (idx < 0) return;

  // Sequence-based dedup.
  if (data.seq) {
    if (data.seq <= _lastSeenSeq) return;
    _lastSeenSeq = data.seq;
  }

  let shouldFinish = false;

  coworkMessages.update(msgs => {
    const updated = [...msgs];
    const msg = { ...updated[idx] };
    if (!msg) return msgs;

    switch (data.type) {
      case 'text':
        if (_pendingContentReset) {
          msg.content = data.content;
          _pendingContentReset = false;
        } else {
          msg.content = (msg.content || '') + data.content;
        }
        break;

      case 'tool_call': {
        if (_pendingContentReset) {
          msg.content = '';
          _pendingContentReset = false;
        }
        msg.steps = [...(msg.steps || []), {
          type: 'tool_call',
          tool_name: data.tool_name,
          tool_input: data.tool_input,
        }];

        const toolLabel = formatToolName(data.tool_name);
        coworkContextTools.update(tools => {
          if (tools.includes(toolLabel)) return tools;
          return [...tools, toolLabel];
        });
        break;
      }

      case 'tool_result':
        msg.steps = [...(msg.steps || []), {
          type: 'tool_result',
          tool_name: data.tool_name,
          content: data.content,
        }];
        _pendingContentReset = true;
        break;

      case 'file_created':
        if (data.path) {
          const fName = data.name || data.path.split('/').pop();
          coworkCreatedFiles.update(files => {
            if (files.find(f => f.path === data.path)) return files;
            return [...files, { name: fName, path: data.path }];
          });
        }
        return msgs;

      case 'done':
        msg.role = 'assistant';
        msg.content = data.final_text || msg.content;
        msg.steps = data.steps || msg.steps;
        msg.isStreaming = false;
        shouldFinish = true;
        break;

      case 'error':
        msg.role = 'assistant';
        msg.content = msg.content || `Something went wrong: ${data.error}`;
        msg.steps = msg.steps || [];
        msg.isError = true;
        msg.isStreaming = false;
        shouldFinish = true;
        break;
    }

    updated[idx] = msg;
    return updated;
  });

  if (shouldFinish) {
    finishStream();
  }
}

function finishStream() {
  coworkIsStreaming.set(false);
  coworkLoading.set(false);
  _pendingContentReset = false;
  coworkStreamingIdx.set(-1);
  cleanupListener();
  refreshCoworkHistory();
}

// ─── Public API ───

export async function refreshCoworkHistory() {
  try {
    const sessions = await Backend.ListCoworkSessions?.();
    coworkTaskHistory.set(
      (sessions || []).map(s => ({
        id: s.id,
        title: s.title || 'Untitled',
        timestamp: s.timestamp,
      }))
    );
  } catch (e) {
    console.error('Failed to load cowork history:', e);
  }
}

export async function startCoworkTask(text) {
  if (!text?.trim() || get(coworkLoading)) return;

  const newId = await Backend.NewCoworkSession?.();
  activeCoworkTaskId.set(newId);

  let title = text.trim();
  if (title.length > 50) title = title.substring(0, 50) + '...';
  coworkTaskTitle.set(title);

  // Add to sidebar immediately.
  coworkTaskHistory.update(history => [
    { id: newId, title, timestamp: Date.now() },
    ...history,
  ]);

  coworkMessages.set([
    { role: 'user', content: text },
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);
  coworkStreamingIdx.set(1);
  coworkCreatedFiles.set([]);
  coworkContextTools.set([]);
  coworkLoading.set(true);
  coworkIsStreaming.set(true);
  _pendingContentReset = false;
  _lastSeenSeq = 0;
  coworkPhase.set('workspace');

  ensureListener();

  try {
    await Backend.SendCoworkTaskStream?.(text, newId);
  } catch (err) {
    coworkMessages.update(msgs => {
      const updated = [...msgs];
      const idx = get(coworkStreamingIdx);
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], content: `Something went wrong: ${err}`, isStreaming: false };
      }
      return updated;
    });
    finishStream();
  }
}

export async function sendCoworkFollowUp(text) {
  if (!text?.trim() || get(coworkLoading)) return;

  coworkMessages.update(msgs => [
    ...msgs,
    { role: 'user', content: text },
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);

  const msgList = get(coworkMessages);
  coworkStreamingIdx.set(msgList.length - 1);
  coworkLoading.set(true);
  coworkIsStreaming.set(true);
  _pendingContentReset = false;
  _lastSeenSeq = 0;

  ensureListener();

  try {
    const sid = get(activeCoworkTaskId);
    await Backend.SendCoworkTaskStream?.(text, sid);
  } catch (err) {
    coworkMessages.update(msgs => {
      const updated = [...msgs];
      const idx = get(coworkStreamingIdx);
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], content: `Something went wrong: ${err}`, isStreaming: false };
      }
      return updated;
    });
    finishStream();
  }
}

export async function cancelCowork() {
  try {
    await Backend.CancelCoworkStream?.(get(activeCoworkTaskId) || '');
  } catch (err) {
    console.error('Cancel failed:', err);
  }
  const idx = get(coworkStreamingIdx);
  if (idx >= 0) {
    coworkMessages.update(msgs => {
      const updated = [...msgs];
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], isStreaming: false };
      }
      return updated;
    });
  }
  finishStream();
}

export function newCoworkTask() {
  coworkPhase.set('welcome');
  coworkTaskTitle.set('');
  coworkMessages.set([]);
  coworkStreamingIdx.set(-1);
  coworkCreatedFiles.set([]);
  coworkContextTools.set([]);
  coworkLoading.set(false);
  coworkIsStreaming.set(false);
  activeCoworkTaskId.set(null);
}

export async function selectCoworkTask(sessionId) {
  if (sessionId === get(activeCoworkTaskId) && get(coworkPhase) === 'workspace') return;

  try {
    const loaded = await Backend.LoadCoworkSession?.(sessionId);
    activeCoworkTaskId.set(sessionId);

    const msgs = (loaded || [])
      .filter(m => m.role === 'user' || m.role === 'assistant')
      .map(m => ({
        role: m.role,
        content: m.content || '',
        steps: (m.steps || []).map(s => ({
          type: s.type,
          content: s.content,
          tool_name: s.tool_name,
          tool_input: s.tool_input,
        })),
      }));

    coworkMessages.set(msgs);

    // Derive title from first user message.
    const firstUser = msgs.find(m => m.role === 'user');
    let title = (firstUser?.content || '').trim();
    if (title.length > 50) title = title.substring(0, 50) + '...';
    coworkTaskTitle.set(title);

    // Rebuild context tools from steps.
    const allSteps = msgs.flatMap(m => m.steps || []);
    coworkContextTools.set([
      ...new Set(
        allSteps
          .filter(s => s.type === 'tool_call')
          .map(s => formatToolName(s.tool_name))
      ),
    ]);

    coworkCreatedFiles.set([]);

    // Try loading files from the backend.
    try {
      const files = await Backend.ListCoworkFiles?.(sessionId);
      if (files && files.length > 0) {
        coworkCreatedFiles.set(files.map(f => ({ name: f.name, path: f.path })));
      }
    } catch (_) {}

    coworkStreamingIdx.set(-1);
    coworkPhase.set('workspace');
    coworkLoading.set(false);
    coworkIsStreaming.set(false);
  } catch (e) {
    console.error('Failed to load cowork task:', e);
  }
}

export async function deleteCoworkTask(sessionId) {
  try {
    await Backend.DeleteCoworkSession?.(sessionId);
    if (sessionId === get(activeCoworkTaskId)) {
      newCoworkTask();
    }
    await refreshCoworkHistory();
  } catch (e) {
    console.error('Failed to delete cowork task:', e);
  }
}
