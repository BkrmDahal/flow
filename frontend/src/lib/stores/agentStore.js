import { writable, get } from 'svelte/store';
import { formatToolName } from '../utils/formatters.js';
import {
  NewAgentSession,
  ListAgentSessions,
  LoadAgentSession,
  DeleteAgentSession,
  SendAgentTaskStream,
  SendAgentTaskStreamWithFiles,
  CancelStream,
  ListTaskFiles,
} from '../../../wailsjs/go/backend/App';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';

// ─── Agent task state ───
export const agentPhase = writable('welcome');       // 'welcome' | 'workspace'
export const agentTaskHistory = writable([]);         // Array of { id, title, timestamp }
export const activeAgentTaskId = writable(null);
export const agentTaskTitle = writable('');
export const agentMessages = writable([]);            // Array of { role, content, steps?, isStreaming? }
export const agentStreamingIdx = writable(-1);
export const agentProgressSteps = writable([]);       // Derived progress steps
export const agentCreatedFiles = writable([]);        // Files created during the task
export const agentContextTools = writable([]);        // Distinct tool names used
export const agentSkillsUsed = writable([]);          // Skill names loaded via use_skill
export const agentLoading = writable(false);
export const agentIsStreaming = writable(false);

// Internal state
let _agentStreamCleanup = null;
let _agentCurrentThinkingIdx = -1;
// Deferred content reset: instead of clearing msg.content immediately on
// tool_result (which causes a visual jump), we set this flag and clear on
// the next text or tool_call event so the previous text stays visible.
let _agentPendingContentReset = false;

// Sequence-based dedup: the backend attaches a monotonically increasing `seq`
// number to each event. Duplicates from the Wails macOS WebKit bridge carry
// the same seq and are dropped.
let _agentLastSeenSeq = 0;

// ─── Background stream support ───
// Caches state for agent sessions whose streams are still running but the user
// has navigated away.
const _bgAgentStreams = new Map();

// Exported store: set of agent session IDs streaming in the background.
export const backgroundAgentStreamingSessions = writable(new Set());

// Whether the global agent event listener is currently registered.
let _agentListenerRegistered = false;

function ensureAgentListener() {
  if (!_agentListenerRegistered) {
    EventsOff('agent:stream:event');
    if (_agentStreamCleanup) { _agentStreamCleanup(); _agentStreamCleanup = null; }
    _agentStreamCleanup = EventsOn('agent:stream:event', handleAgentStreamEvent);
    _agentListenerRegistered = true;
  }
}

function cleanupAgentListenerIfNeeded() {
  if (get(agentStreamingIdx) < 0 && _bgAgentStreams.size === 0) {
    EventsOff('agent:stream:event');
    if (_agentStreamCleanup) { _agentStreamCleanup(); _agentStreamCleanup = null; }
    _agentListenerRegistered = false;
  }
}

// Save current foreground agent streaming state into the background cache.
function saveCurrentAgentToBackground() {
  const currentId = get(activeAgentTaskId);
  if (!currentId || !get(agentLoading)) return;
  _bgAgentStreams.set(currentId, {
    messages: get(agentMessages),
    streamingIdx: get(agentStreamingIdx),
    currentThinkingIdx: _agentCurrentThinkingIdx,
    lastSeenSeq: _agentLastSeenSeq,
    pendingContentReset: _agentPendingContentReset,
    createdFiles: get(agentCreatedFiles),
    progressSteps: get(agentProgressSteps),
    contextTools: get(agentContextTools),
    skillsUsed: get(agentSkillsUsed),
    taskTitle: get(agentTaskTitle),
  });
  backgroundAgentStreamingSessions.update(s => { s.add(currentId); return new Set(s); });
  // Reset foreground streaming indicators (backend keeps running).
  agentStreamingIdx.set(-1);
  agentLoading.set(false);
  agentIsStreaming.set(false);
  _agentCurrentThinkingIdx = -1;
}

// ─── Methods ───

export async function refreshAgentTaskHistory() {
  try {
    const sessions = await ListAgentSessions();
    agentTaskHistory.set(
      (sessions || []).map(s => ({
        id: s.id,
        title: s.title || 'Untitled',
        timestamp: s.timestamp,
      }))
    );
  } catch (e) {
    console.error('Failed to load agent task history:', e);
  }
}

function handleAgentStreamEvent(data) {
  const activeId = get(activeAgentTaskId);
  const sessionId = data.session_id;

  // Route events for background agent sessions to the background handler.
  if (sessionId && sessionId !== activeId) {
    if (_bgAgentStreams.has(sessionId)) {
      handleBgAgentStreamEvent(sessionId, data);
    }
    return;
  }

  if (!get(agentIsStreaming)) return;
  const idx = get(agentStreamingIdx);
  if (idx < 0) return;

  // Sequence-based dedup: reject events with an already-seen seq number.
  if (data.seq) {
    if (data.seq <= _agentLastSeenSeq) return;
    _agentLastSeenSeq = data.seq;
  }

  let shouldFinish = false;

  agentMessages.update(msgs => {
    const updated = [...msgs];
    const msg = { ...updated[idx] };
    if (!msg) return msgs;

    switch (data.type) {
      case 'thinking_start': {
        const steps = [...(msg.steps || [])];
        if (steps.length > 0 && steps[steps.length - 1].type === 'thinking') {
          _agentCurrentThinkingIdx = steps.length - 1;
          steps[_agentCurrentThinkingIdx] = {
            ...steps[_agentCurrentThinkingIdx],
            content: steps[_agentCurrentThinkingIdx].content + '\n\n',
          };
        } else {
          _agentCurrentThinkingIdx = steps.length;
          steps.push({ type: 'thinking', content: '' });
        }
        msg.steps = steps;
        break;
      }

      case 'thinking':
        if (_agentCurrentThinkingIdx >= 0 && msg.steps[_agentCurrentThinkingIdx]) {
          const steps = [...msg.steps];
          steps[_agentCurrentThinkingIdx] = {
            ...steps[_agentCurrentThinkingIdx],
            content: steps[_agentCurrentThinkingIdx].content + data.content,
          };
          msg.steps = steps;
        }
        break;

      case 'text':
        if (_agentPendingContentReset) {
          msg.content = data.content;
          _agentPendingContentReset = false;
        } else {
          msg.content = (msg.content || '') + data.content;
        }
        break;

      case 'tool_call': {
        _agentCurrentThinkingIdx = -1;
        if (_agentPendingContentReset) {
          msg.content = '';
          _agentPendingContentReset = false;
        }
        msg.steps = [...(msg.steps || []), { type: 'tool_call', tool_name: data.tool_name, tool_input: data.tool_input }];

        // Track context tools (deduplicated), skip meta-tools.
        if (data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
          const toolLabel = formatToolName(data.tool_name);
          agentContextTools.update(tools => {
            if (tools.includes(toolLabel)) return tools;
            return [...tools, toolLabel];
          });
        }
        break;
      }

      case 'tool_result':
        msg.steps = [...(msg.steps || []), { type: 'tool_result', tool_name: data.tool_name, content: data.content }];
        _agentPendingContentReset = true;
        break;

      case 'todo_update':
        if (data.todo_items && Array.isArray(data.todo_items)) {
          agentProgressSteps.set(
            data.todo_items.map(item => ({
              id: item.id,
              label: item.content,
              status: item.status,
            }))
          );
        }
        return msgs;

      case 'file_created':
        if (data.path) {
          const fName = data.name || data.path.split('/').pop();
          agentCreatedFiles.update(files => {
            if (files.find(f => f.path === data.path)) return files;
            return [...files, { name: fName, path: data.path }];
          });
        }
        return msgs;

      case 'skill_used':
        if (data.content) {
          agentSkillsUsed.update(skills => {
            if (skills.includes(data.content)) return skills;
            return [...skills, data.content];
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
    if (data.type === 'done') {
      agentProgressSteps.update(steps =>
        steps.some(s => s.status === 'in_progress')
          ? steps.map(s => s.status === 'in_progress' ? { ...s, status: 'completed' } : s)
          : steps
      );
    }
    finishAgentStream();
  }
}

// Handle stream events for agent sessions running in the background.
function handleBgAgentStreamEvent(sessionId, data) {
  const state = _bgAgentStreams.get(sessionId);
  if (!state || state.streamingIdx < 0) return;

  if (data.seq) {
    if (data.seq <= state.lastSeenSeq) return;
    state.lastSeenSeq = data.seq;
  }

  const idx = state.streamingIdx;
  const msg = { ...state.messages[idx] };
  if (!msg) return;

  switch (data.type) {
    case 'thinking_start': {
      const steps = [...(msg.steps || [])];
      if (steps.length > 0 && steps[steps.length - 1].type === 'thinking') {
        state.currentThinkingIdx = steps.length - 1;
        steps[state.currentThinkingIdx] = { ...steps[state.currentThinkingIdx], content: steps[state.currentThinkingIdx].content + '\n\n' };
      } else {
        state.currentThinkingIdx = steps.length;
        steps.push({ type: 'thinking', content: '' });
      }
      msg.steps = steps;
      break;
    }

    case 'thinking':
      if (state.currentThinkingIdx >= 0 && msg.steps[state.currentThinkingIdx]) {
        const steps = [...msg.steps];
        steps[state.currentThinkingIdx] = { ...steps[state.currentThinkingIdx], content: steps[state.currentThinkingIdx].content + data.content };
        msg.steps = steps;
      }
      break;

    case 'text':
      if (state.pendingContentReset) {
        msg.content = data.content;
        state.pendingContentReset = false;
      } else {
        msg.content = (msg.content || '') + data.content;
      }
      break;

    case 'tool_call':
      state.currentThinkingIdx = -1;
      if (state.pendingContentReset) {
        msg.content = '';
        state.pendingContentReset = false;
      }
      msg.steps = [...(msg.steps || []), { type: 'tool_call', tool_name: data.tool_name, tool_input: data.tool_input }];
      if (data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
        const toolLabel = formatToolName(data.tool_name);
        if (!state.contextTools.includes(toolLabel)) {
          state.contextTools = [...state.contextTools, toolLabel];
        }
      }
      break;

    case 'tool_result':
      msg.steps = [...(msg.steps || []), { type: 'tool_result', tool_name: data.tool_name, content: data.content }];
      state.pendingContentReset = true;
      break;

    case 'todo_update':
      if (data.todo_items && Array.isArray(data.todo_items)) {
        state.progressSteps = data.todo_items.map(item => ({
          id: item.id,
          label: item.content,
          status: item.status,
        }));
      }
      return;

    case 'file_created':
      if (data.path) {
        const fName = data.name || data.path.split('/').pop();
        if (!state.createdFiles.find(f => f.path === data.path)) {
          state.createdFiles.push({ name: fName, path: data.path });
        }
      }
      return;

    case 'skill_used':
      if (data.content && !state.skillsUsed.includes(data.content)) {
        state.skillsUsed = [...state.skillsUsed, data.content];
      }
      return;

    case 'done':
      msg.role = 'assistant';
      msg.content = data.final_text || msg.content;
      msg.steps = data.steps || msg.steps;
      delete msg.isStreaming;
      state.messages[idx] = msg;
      state.progressSteps = state.progressSteps.map(s =>
        s.status === 'in_progress' ? { ...s, status: 'completed' } : s
      );
      _bgAgentStreams.delete(sessionId);
      backgroundAgentStreamingSessions.update(s => { s.delete(sessionId); return new Set(s); });
      cleanupAgentListenerIfNeeded();
      refreshAgentTaskHistory();
      return;

    case 'error':
      msg.role = 'assistant';
      msg.content = msg.content || `Something went wrong: ${data.error}`;
      msg.steps = msg.steps || [];
      msg.isError = true;
      delete msg.isStreaming;
      state.messages[idx] = msg;
      _bgAgentStreams.delete(sessionId);
      backgroundAgentStreamingSessions.update(s => { s.delete(sessionId); return new Set(s); });
      cleanupAgentListenerIfNeeded();
      refreshAgentTaskHistory();
      return;
  }

  state.messages[idx] = msg;
}

function finishAgentStream() {
  agentIsStreaming.set(false);
  agentLoading.set(false);
  _agentCurrentThinkingIdx = -1;
  _agentPendingContentReset = false;
  agentStreamingIdx.set(-1);
  cleanupAgentListenerIfNeeded();
  refreshAgentTaskHistory();
}

export async function startAgentTask(text, readyFlag, files) {
  if ((!text && (!files || files.length === 0)) || get(agentLoading) || !readyFlag) return;

  const newId = await NewAgentSession();
  activeAgentTaskId.set(newId);

  let title = (text || '').trim();
  if (title.length > 50) title = title.substring(0, 50) + '...';
  if (!title && files && files.length > 0) title = `${files.length} file${files.length > 1 ? 's' : ''} attached`;
  agentTaskTitle.set(title);

  // Immediately show the new task in the sidebar.
  agentTaskHistory.update(history => [
    { id: newId, title, timestamp: Date.now() },
    ...history,
  ]);

  const userMessage = {
    role: 'user',
    content: text || '',
    files: files && files.length > 0
      ? files.map(f => ({ name: f.name, type: f.type, size: f.size, dataUrl: f.dataUrl }))
      : undefined,
  };

  agentMessages.set([
    userMessage,
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);
  agentStreamingIdx.set(1);
  agentProgressSteps.set([]);
  agentCreatedFiles.set([]);
  agentContextTools.set([]);
  agentSkillsUsed.set([]);
  agentLoading.set(true);
  agentIsStreaming.set(true);
  _agentCurrentThinkingIdx = -1;
  _agentPendingContentReset = false;
  agentPhase.set('workspace');

  _agentLastSeenSeq = 0;
  ensureAgentListener();

  try {
    const sid = get(activeAgentTaskId);
    if (files && files.length > 0) {
      const attachments = files.map(f => ({
        name: f.name,
        mime_type: f.type,
        data: f.data,
      }));
      await SendAgentTaskStreamWithFiles(text || '', attachments, sid);
    } else {
      await SendAgentTaskStream(text, sid);
    }
  } catch (err) {
    agentMessages.update(msgs => {
      const updated = [...msgs];
      const idx = get(agentStreamingIdx);
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], content: `Something went wrong: ${err}`, isStreaming: false };
      }
      return updated;
    });
    finishAgentStream();
  }
}

export async function sendAgentFollowUp(text, readyFlag, files) {
  if ((!text && (!files || files.length === 0)) || get(agentLoading) || !readyFlag) return;

  const userMessage = {
    role: 'user',
    content: text || '',
    files: files && files.length > 0
      ? files.map(f => ({ name: f.name, type: f.type, size: f.size, dataUrl: f.dataUrl }))
      : undefined,
  };

  agentMessages.update(msgs => [
    ...msgs,
    userMessage,
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);

  const msgList = get(agentMessages);
  agentStreamingIdx.set(msgList.length - 1);
  agentLoading.set(true);
  agentIsStreaming.set(true);
  _agentCurrentThinkingIdx = -1;
  _agentPendingContentReset = false;

  _agentLastSeenSeq = 0;
  ensureAgentListener();

  try {
    const sid = get(activeAgentTaskId);
    if (files && files.length > 0) {
      const attachments = files.map(f => ({
        name: f.name,
        mime_type: f.type,
        data: f.data,
      }));
      await SendAgentTaskStreamWithFiles(text || '', attachments, sid);
    } else {
      await SendAgentTaskStream(text, sid);
    }
  } catch (err) {
    agentMessages.update(msgs => {
      const updated = [...msgs];
      const idx = get(agentStreamingIdx);
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], content: `Something went wrong: ${err}`, isStreaming: false };
      }
      return updated;
    });
    finishAgentStream();
  }
}

export async function cancelAgent() {
  try {
    await CancelStream(get(activeAgentTaskId) || '');
  } catch (err) {
    console.error('Agent cancel failed:', err);
  }
  const idx = get(agentStreamingIdx);
  if (idx >= 0) {
    agentMessages.update(msgs => {
      const updated = [...msgs];
      if (updated[idx]) {
        updated[idx] = { ...updated[idx], isStreaming: false };
      }
      return updated;
    });
  }
  finishAgentStream();
}

export function newAgentTask() {
  // If the current task is streaming, move it to the background.
  saveCurrentAgentToBackground();

  agentPhase.set('welcome');
  agentTaskTitle.set('');
  agentMessages.set([]);
  agentStreamingIdx.set(-1);
  agentProgressSteps.set([]);
  agentCreatedFiles.set([]);
  agentContextTools.set([]);
  agentSkillsUsed.set([]);
  agentLoading.set(false);
  agentIsStreaming.set(false);
  activeAgentTaskId.set(null);
}

export async function selectAgentTask(sessionId) {
  if (sessionId === get(activeAgentTaskId) && get(agentPhase) === 'workspace') return;

  // If the current task is streaming, move it to the background.
  saveCurrentAgentToBackground();

  // If the target session is streaming in the background, restore it.
  if (_bgAgentStreams.has(sessionId)) {
    const state = _bgAgentStreams.get(sessionId);
    _bgAgentStreams.delete(sessionId);
    backgroundAgentStreamingSessions.update(s => { s.delete(sessionId); return new Set(s); });

    activeAgentTaskId.set(sessionId);
    agentMessages.set(state.messages);
    agentStreamingIdx.set(state.streamingIdx);
    _agentCurrentThinkingIdx = state.currentThinkingIdx;
    _agentLastSeenSeq = state.lastSeenSeq;
    _agentPendingContentReset = state.pendingContentReset || false;
    agentCreatedFiles.set(state.createdFiles);
    agentProgressSteps.set(state.progressSteps);
    agentContextTools.set(state.contextTools);
    agentSkillsUsed.set(state.skillsUsed || []);
    agentTaskTitle.set(state.taskTitle);
    agentLoading.set(state.streamingIdx >= 0);
    agentIsStreaming.set(state.streamingIdx >= 0);
    agentPhase.set('workspace');
    return;
  }

  // Otherwise load from the backend.
  try {
    const loaded = await LoadAgentSession(sessionId);
    activeAgentTaskId.set(sessionId);

    // The session manager returns raw Messages — extract user/assistant turns.
    const msgs = [];
    for (const m of (loaded || [])) {
      if (m.role !== 'user' && m.role !== 'assistant') continue;
      let content = '';
      try {
        const parsed = JSON.parse(m.content);
        if (typeof parsed === 'string') content = parsed;
        else if (Array.isArray(parsed)) {
          content = parsed.filter(b => b.type === 'text').map(b => b.text || '').join('');
        }
      } catch {
        content = m.content || '';
      }
      msgs.push({ role: m.role, content, steps: [] });
    }

    agentMessages.set(msgs);

    // Derive title from first user message.
    const firstUser = msgs.find(m => m.role === 'user');
    let title = (firstUser?.content || '').trim();
    if (title.length > 50) title = title.substring(0, 50) + '...';
    agentTaskTitle.set(title);

    agentProgressSteps.set([]);
    agentContextTools.set([]);
    agentSkillsUsed.set([]);
    agentCreatedFiles.set([]);

    // Try loading files from the backend.
    try {
      const taskFiles = await ListTaskFiles(sessionId);
      if (taskFiles && taskFiles.length > 0) {
        agentCreatedFiles.set(taskFiles.map(f => ({ name: f.name, path: f.path })));
      }
    } catch (_) {}

    agentStreamingIdx.set(-1);
    agentPhase.set('workspace');
    agentLoading.set(false);
    agentIsStreaming.set(false);
  } catch (e) {
    console.error('Failed to load agent task:', e);
  }
}

export async function deleteAgentTask(sessionId) {
  try {
    if (_bgAgentStreams.has(sessionId)) {
      try { await CancelStream(sessionId); } catch (_) {}
      _bgAgentStreams.delete(sessionId);
      backgroundAgentStreamingSessions.update(s => { s.delete(sessionId); return new Set(s); });
      cleanupAgentListenerIfNeeded();
    }

    await DeleteAgentSession(sessionId);
    if (sessionId === get(activeAgentTaskId)) {
      newAgentTask();
    }
    await refreshAgentTaskHistory();
  } catch (e) {
    console.error('Failed to delete agent task:', e);
  }
}
