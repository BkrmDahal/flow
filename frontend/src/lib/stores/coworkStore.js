/**
 * Cowork store — manages agent task state, streaming, and session history.
 * Adapted from talcon's agentStore.js. Simplified for v1:
 *   - No background stream caching (single in-flight task)
 *   - No file attachment support
 *   - Event channel: cowork:stream:event
 */
import { writable, get } from 'svelte/store';
import { formatToolLabel, formatToolName } from '../utils/formatters.js';
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
export const coworkProgressSteps = writable([]);        // Derived progress steps
export const coworkSkillsUsed = writable([]);           // Skill names loaded via use_skill
export const coworkLoading = writable(false);
export const coworkIsStreaming = writable(false);
export const coworkParseDocuments = writable(true);     // Whether to parse PDF/XLSX text
export const coworkWebSearchEnabled = writable(false);   // Whether web_search/fetch_url tools are available
export const coworkScreenCaptureEnabled = writable(false); // Whether capture_screen tool is available
export const coworkMemoryEnabled = writable(false);       // Whether memory tools are available
export const backgroundCoworkStreamingSessions = writable(new Set());


// Internal state
let _streamCleanup = null;
let _pendingContentReset = false;
let _lastSeenSeq = 0;
let _listenerRegistered = false;
let _usingTodoPlan = false;
const _bgCoworkStreams = new Map();

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
  if (get(coworkStreamingIdx) < 0 && _bgCoworkStreams.size === 0) {
    Events.off('cowork:stream:event');
    if (_streamCleanup) { _streamCleanup(); _streamCleanup = null; }
    _listenerRegistered = false;
  }
}

function saveCurrentCoworkToBackground() {
  const currentId = get(activeCoworkTaskId);
  if (!currentId || !get(coworkLoading)) return;

  _bgCoworkStreams.set(currentId, {
    messages: get(coworkMessages),
    streamingIdx: get(coworkStreamingIdx),
    lastSeenSeq: _lastSeenSeq,
    pendingContentReset: _pendingContentReset,
    usingTodoPlan: _usingTodoPlan,
    createdFiles: get(coworkCreatedFiles),
    progressSteps: get(coworkProgressSteps),
    contextTools: get(coworkContextTools),
    skillsUsed: get(coworkSkillsUsed),
    taskTitle: get(coworkTaskTitle),
  });
  backgroundCoworkStreamingSessions.update(s => {
    s.add(currentId);
    return new Set(s);
  });

  coworkStreamingIdx.set(-1);
  coworkLoading.set(false);
  coworkIsStreaming.set(false);
}

function resetPlanState() {
  _usingTodoPlan = false;
  coworkProgressSteps.set([]);
}

function appendFallbackProgressStep(toolName, toolInput) {
  if (_usingTodoPlan || toolName === 'todo_write' || toolName === 'use_skill') return;

  const label = formatToolLabel(toolName, toolInput);
  coworkProgressSteps.update(steps => {
    const completed = steps.map(step =>
      step.status === 'in_progress' ? { ...step, status: 'completed' } : step
    );
    return [
      ...completed,
      {
        id: `fallback-${completed.length + 1}-${toolName || 'tool'}`,
        label,
        status: 'in_progress',
      },
    ];
  });
}

function completeFallbackProgressStep() {
  if (_usingTodoPlan) return;
  coworkProgressSteps.update(steps =>
    steps.map(step =>
      step.status === 'in_progress' ? { ...step, status: 'completed' } : step
    )
  );
}

function advanceTodoPlanFromToolResult(toolName) {
  if (!_usingTodoPlan || toolName === 'todo_write' || toolName === 'use_skill') return;

  coworkProgressSteps.update(steps => {
    const updated = [...steps];
    const activeIdx = updated.findIndex(step => step.status === 'in_progress');
    if (activeIdx >= 0) {
      updated[activeIdx] = { ...updated[activeIdx], status: 'completed' };
    }

    const nextIdx = updated.findIndex(step => step.status === 'pending');
    if (nextIdx >= 0) {
      updated[nextIdx] = { ...updated[nextIdx], status: 'in_progress' };
    }

    return updated;
  });
}

function completePlanOnDone() {
  coworkProgressSteps.update(steps =>
    steps.map(step =>
      step.status === 'completed' ? step : { ...step, status: 'completed' }
    )
  );
}

function progressStepsFromHistory(steps) {
  const todoCalls = steps.filter(s => s.type === 'tool_call' && s.tool_name === 'todo_write');
  const lastTodoCall = todoCalls[todoCalls.length - 1];
  if (lastTodoCall?.tool_input) {
    try {
      const input = JSON.parse(lastTodoCall.tool_input);
      if (Array.isArray(input.todos)) {
        return input.todos.map(item => ({
          id: item.id,
          label: item.content,
          status: 'completed',
        }));
      }
    } catch { /* fall back to tool-call progress */ }
  }

  return steps
    .filter(s => s.type === 'tool_call' && s.tool_name !== 'todo_write' && s.tool_name !== 'use_skill')
    .map((s, index) => ({
      id: `history-${index + 1}-${s.tool_name || 'tool'}`,
      label: formatToolLabel(s.tool_name, s.tool_input),
      status: 'completed',
    }));
}

// ─── Stream event handler ───

function handleStreamEvent(data) {
  const activeId = get(activeCoworkTaskId);
  if (data.session_id && data.session_id !== activeId) {
    if (_bgCoworkStreams.has(data.session_id)) {
      handleBgCoworkStreamEvent(data.session_id, data);
    }
    return;
  }
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
        appendFallbackProgressStep(data.tool_name, data.tool_input);

        if (data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
          const toolLabel = formatToolName(data.tool_name);
          coworkContextTools.update(tools => {
            if (tools.includes(toolLabel)) return tools;
            return [...tools, toolLabel];
          });
        }
        break;
      }

      case 'tool_result':
        msg.steps = [...(msg.steps || []), {
          type: 'tool_result',
          tool_name: data.tool_name,
          content: data.content,
        }];
        advanceTodoPlanFromToolResult(data.tool_name);
        completeFallbackProgressStep();
        _pendingContentReset = true;
        break;

      case 'todo_update':
        if (data.todo_items && Array.isArray(data.todo_items)) {
          _usingTodoPlan = true;
          coworkProgressSteps.set(
            data.todo_items.map(item => ({
              id: item.id,
              label: item.content,
              status: item.status,
            }))
          );
        }
        return msgs;

      case 'skill_used':
        if (data.content) {
          coworkSkillsUsed.update(skills => {
            if (skills.includes(data.content)) return skills;
            return [...skills, data.content];
          });
        }
        return msgs;

      case 'file_created':
        if (data.path) {
          const fName = data.name || data.path.split('/').pop();
          coworkCreatedFiles.update(files => {
            if (files.find(f => f.path === data.path)) return files;
            return [...files, { name: fName, path: data.path }];
          });
          // Deduplicate inline file cards.
          const existing = msg.filesCreated || [];
          if (!existing.find(f => f.path === data.path)) {
            msg.filesCreated = [...existing, { name: fName, path: data.path }];
          }
        }
        break;

      case 'done':
        msg.role = 'assistant';
        msg.content = data.final_text || msg.content;
        msg.steps = data.steps || msg.steps;
        msg.isStreaming = false;
        shouldFinish = true;
        break;

      case 'error':
        msg.role = 'assistant';
        const errorText = data.error || data.content || 'Unknown error';
        msg.content = msg.content || `Something went wrong: ${errorText}`;
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
      completePlanOnDone();
    }
    finishStream();
  }
}

function handleBgCoworkStreamEvent(sessionId, data) {
  const state = _bgCoworkStreams.get(sessionId);
  if (!state || state.streamingIdx < 0) return;

  if (data.seq) {
    if (data.seq <= state.lastSeenSeq) return;
    state.lastSeenSeq = data.seq;
  }

  const idx = state.streamingIdx;
  const msg = { ...state.messages[idx] };
  if (!msg) return;

  switch (data.type) {
    case 'text':
      if (state.pendingContentReset) {
        msg.content = data.content;
        state.pendingContentReset = false;
      } else {
        msg.content = (msg.content || '') + data.content;
      }
      break;

    case 'tool_call': {
      if (state.pendingContentReset) {
        msg.content = '';
        state.pendingContentReset = false;
      }
      msg.steps = [...(msg.steps || []), {
        type: 'tool_call',
        tool_name: data.tool_name,
        tool_input: data.tool_input,
      }];

      if (!state.usingTodoPlan && data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
        const label = formatToolLabel(data.tool_name, data.tool_input);
        state.progressSteps = [
          ...state.progressSteps.map(step =>
            step.status === 'in_progress' ? { ...step, status: 'completed' } : step
          ),
          {
            id: `fallback-${state.progressSteps.length + 1}-${data.tool_name || 'tool'}`,
            label,
            status: 'in_progress',
          },
        ];
      }

      if (data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
        const toolLabel = formatToolName(data.tool_name);
        if (!state.contextTools.includes(toolLabel)) {
          state.contextTools = [...state.contextTools, toolLabel];
        }
      }
      break;
    }

    case 'tool_result':
      msg.steps = [...(msg.steps || []), {
        type: 'tool_result',
        tool_name: data.tool_name,
        content: data.content,
      }];
      if (state.usingTodoPlan && data.tool_name !== 'todo_write' && data.tool_name !== 'use_skill') {
        const activeIdx = state.progressSteps.findIndex(step => step.status === 'in_progress');
        if (activeIdx >= 0) {
          state.progressSteps[activeIdx] = { ...state.progressSteps[activeIdx], status: 'completed' };
        }
        const nextIdx = state.progressSteps.findIndex(step => step.status === 'pending');
        if (nextIdx >= 0) {
          state.progressSteps[nextIdx] = { ...state.progressSteps[nextIdx], status: 'in_progress' };
        }
      } else {
        state.progressSteps = state.progressSteps.map(step =>
          step.status === 'in_progress' ? { ...step, status: 'completed' } : step
        );
      }
      state.pendingContentReset = true;
      break;

    case 'todo_update':
      if (data.todo_items && Array.isArray(data.todo_items)) {
        state.usingTodoPlan = true;
        state.progressSteps = data.todo_items.map(item => ({
          id: item.id,
          label: item.content,
          status: item.status,
        }));
      }
      return;

    case 'skill_used':
      if (data.content && !state.skillsUsed.includes(data.content)) {
        state.skillsUsed = [...state.skillsUsed, data.content];
      }
      return;

    case 'file_created':
      if (data.path) {
        const fName = data.name || data.path.split('/').pop();
        if (!state.createdFiles.find(f => f.path === data.path)) {
          state.createdFiles = [...state.createdFiles, { name: fName, path: data.path }];
        }
        msg.filesCreated = [
          ...(msg.filesCreated || []),
          { name: fName, path: data.path }
        ];
        state.messages[idx] = msg;
      }
      return;

    case 'done':
      msg.role = 'assistant';
      msg.content = data.final_text || msg.content;
      msg.steps = data.steps || msg.steps;
      msg.isStreaming = false;
      state.messages[idx] = msg;
      state.progressSteps = state.progressSteps.map(step =>
        step.status === 'completed' ? step : { ...step, status: 'completed' }
      );
      _bgCoworkStreams.delete(sessionId);
      backgroundCoworkStreamingSessions.update(s => {
        s.delete(sessionId);
        return new Set(s);
      });
      cleanupListener();
      refreshCoworkHistory();
      return;

    case 'error': {
      msg.role = 'assistant';
      const errorText = data.error || data.content || 'Unknown error';
      msg.content = msg.content || `Something went wrong: ${errorText}`;
      msg.steps = msg.steps || [];
      msg.isError = true;
      msg.isStreaming = false;
      state.messages[idx] = msg;
      _bgCoworkStreams.delete(sessionId);
      backgroundCoworkStreamingSessions.update(s => {
        s.delete(sessionId);
        return new Set(s);
      });
      cleanupListener();
      refreshCoworkHistory();
      return;
    }
  }

  state.messages[idx] = msg;
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
    const sessions = await Backend.ListCoworkSessions();
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

function requireBackendMethod(name) {
  const fn = Backend[name];
  if (typeof fn !== 'function') {
    throw new Error(`${name} is not available. Restart the app so the latest backend bindings are loaded.`);
  }
  return fn;
}

function inferMimeType(file) {
  if (file.type) return file.type;

  const ext = (file.name || '').split('.').pop()?.toLowerCase();
  switch (ext) {
    case 'pdf':
      return 'application/pdf';
    case 'csv':
      return 'text/csv';
    case 'md':
    case 'markdown':
      return 'text/markdown';
    case 'html':
    case 'htm':
      return 'text/html';
    case 'json':
      return 'application/json';
    case 'png':
      return 'image/png';
    case 'jpg':
    case 'jpeg':
      return 'image/jpeg';
    case 'gif':
      return 'image/gif';
    case 'webp':
      return 'image/webp';
    default:
      return 'text/plain';
  }
}

function buildWailsFiles(files) {
  return files.map(file => ({
    name: file.name,
    mime_type: inferMimeType(file),
    data: file.data,
  }));
}

function showStreamStartError(err) {
  coworkMessages.update(msgs => {
    const updated = [...msgs];
    const idx = get(coworkStreamingIdx);
    if (updated[idx]) {
      updated[idx] = {
        ...updated[idx],
        content: `Something went wrong: ${err?.message || err}`,
        isStreaming: false,
        isError: true,
      };
    }
    return updated;
  });
  finishStream();
}

function buildSkillAugmentedText(text, skillName) {
  const trimmed = text?.trim() || '';
  if (!skillName) return trimmed;
  return `<cowork_selected_skill name="${skillName}">Load and use this skill with the use_skill tool before responding.</cowork_selected_skill>\n\n${trimmed}`;
}

function stripSkillAugmentedText(text) {
  return (text || '').replace(/^<cowork_selected_skill name="([^"]+)">[\s\S]*?<\/cowork_selected_skill>\s*/i, '').trim();
}

function extractSelectedSkillName(text) {
  const match = (text || '').match(/^<cowork_selected_skill name="([^"]+)">/i);
  return match?.[1] || '';
}

/** Build the list of tool names the user has toggled OFF. */
function getDisabledTools() {
  const disabled = [];
  if (!get(coworkWebSearchEnabled)) {
    disabled.push('web_search', 'fetch_url');
  }
  if (!get(coworkScreenCaptureEnabled)) {
    disabled.push('capture_screen');
  }
  if (!get(coworkMemoryEnabled)) {
    disabled.push('save_memory', 'memory_search', 'list_memories', 'delete_memory');
  }
  return disabled;
}

export async function startCoworkTask(text, files = [], selectedSkillName = '') {
  if ((!text?.trim() && files.length === 0 && !selectedSkillName) || get(coworkLoading)) return;

  const newId = await requireBackendMethod('NewCoworkSession')();
  activeCoworkTaskId.set(newId);

  let title = text.trim();
  if (title.length > 50) title = title.substring(0, 50) + '...';
  if (!title && selectedSkillName) title = `Use ${selectedSkillName}`;
  if (!title && files.length > 0) title = `${files.length} file${files.length > 1 ? 's' : ''} attached`;
  coworkTaskTitle.set(title);

  // Add to sidebar immediately.
  coworkTaskHistory.update(history => [
    { id: newId, title, timestamp: Date.now() },
    ...history,
  ]);

  coworkMessages.set([
    { role: 'user', content: text, files: files, selectedSkill: selectedSkillName || undefined },
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);
  coworkStreamingIdx.set(1);
  coworkCreatedFiles.set([]);
  coworkContextTools.set([]);
  resetPlanState();
  coworkSkillsUsed.set([]);
  coworkLoading.set(true);
  coworkIsStreaming.set(true);
  _pendingContentReset = false;
  _lastSeenSeq = 0;
  coworkPhase.set('workspace');

  ensureListener();

  try {
    const backendText = buildSkillAugmentedText(text, selectedSkillName);
    const disabled = getDisabledTools();
    if (files.length > 0) {
      const wailsFiles = buildWailsFiles(files);
      const extractText = get(coworkParseDocuments);
      await requireBackendMethod('SendCoworkTaskStreamWithFiles')(backendText, wailsFiles, extractText, newId, disabled);
    } else {
      await requireBackendMethod('SendCoworkTaskStream')(backendText, newId, disabled);
    }
  } catch (err) {
    showStreamStartError(err);
  }
}

export async function sendCoworkFollowUp(text, files = [], selectedSkillName = '') {
  if ((!text?.trim() && files.length === 0 && !selectedSkillName) || get(coworkLoading)) return;

  coworkMessages.update(msgs => [
    ...msgs,
    { role: 'user', content: text, files: files, selectedSkill: selectedSkillName || undefined },
    { role: 'assistant', content: '', steps: [], isStreaming: true },
  ]);

  const msgList = get(coworkMessages);
  coworkStreamingIdx.set(msgList.length - 1);
  coworkLoading.set(true);
  coworkIsStreaming.set(true);
  _pendingContentReset = false;
  _lastSeenSeq = 0;
  resetPlanState();

  ensureListener();

  try {
    const sid = get(activeCoworkTaskId);
    const backendText = buildSkillAugmentedText(text, selectedSkillName);
    const disabled = getDisabledTools();
    if (files.length > 0) {
      const wailsFiles = buildWailsFiles(files);
      const extractText = get(coworkParseDocuments);
      await requireBackendMethod('SendCoworkTaskStreamWithFiles')(backendText, wailsFiles, extractText, sid, disabled);
    } else {
      await requireBackendMethod('SendCoworkTaskStream')(backendText, sid, disabled);
    }
  } catch (err) {
    showStreamStartError(err);
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
  saveCurrentCoworkToBackground();

  coworkPhase.set('welcome');
  coworkTaskTitle.set('');
  coworkMessages.set([]);
  coworkStreamingIdx.set(-1);
  coworkCreatedFiles.set([]);
  coworkContextTools.set([]);
  resetPlanState();
  coworkSkillsUsed.set([]);
  coworkLoading.set(false);
  coworkIsStreaming.set(false);
  activeCoworkTaskId.set(null);
}

export async function selectCoworkTask(sessionId) {
  if (sessionId === get(activeCoworkTaskId) && get(coworkPhase) === 'workspace') return;

  saveCurrentCoworkToBackground();

  if (_bgCoworkStreams.has(sessionId)) {
    const state = _bgCoworkStreams.get(sessionId);
    _bgCoworkStreams.delete(sessionId);
    backgroundCoworkStreamingSessions.update(s => {
      s.delete(sessionId);
      return new Set(s);
    });

    activeCoworkTaskId.set(sessionId);
    coworkMessages.set(state.messages);
    coworkStreamingIdx.set(state.streamingIdx);
    _lastSeenSeq = state.lastSeenSeq;
    _pendingContentReset = state.pendingContentReset;
    _usingTodoPlan = state.usingTodoPlan;
    coworkCreatedFiles.set(state.createdFiles);
    coworkProgressSteps.set(state.progressSteps);
    coworkContextTools.set(state.contextTools);
    coworkSkillsUsed.set(state.skillsUsed);
    coworkTaskTitle.set(state.taskTitle);
    coworkLoading.set(state.streamingIdx >= 0);
    coworkIsStreaming.set(state.streamingIdx >= 0);
    coworkPhase.set('workspace');
    return;
  }

  try {
    const loaded = await Backend.LoadCoworkSession?.(sessionId);
    activeCoworkTaskId.set(sessionId);

    const msgs = (loaded || [])
      .filter(m => m.role === 'user' || m.role === 'assistant')
      .map(m => ({
        role: m.role,
        content: m.role === 'user' ? stripSkillAugmentedText(m.content) : (m.content || ''),
        selectedSkill: m.role === 'user' ? extractSelectedSkillName(m.content) || undefined : undefined,
        files: m.files || undefined,
        steps: (m.steps || []).map(s => ({
          type: s.type,
          content: s.content,
          tool_name: s.tool_name,
          tool_input: s.tool_input,
        })),
      }));

    coworkMessages.set(msgs);

    // Check if title is loaded in history, otherwise derive from first user message.
    const history = get(coworkTaskHistory);
    const histItem = history.find(h => h.id === sessionId);
    if (histItem) {
      coworkTaskTitle.set(histItem.title);
    } else {
      const firstUser = msgs.find(m => m.role === 'user');
      let title = (firstUser?.content || '').trim();
      if (title.length > 50) title = title.substring(0, 50) + '...';
      coworkTaskTitle.set(title);
    }

    // Rebuild context tools from steps.
    const allSteps = msgs.flatMap(m => m.steps || []);
    coworkContextTools.set([
      ...new Set(
        allSteps
          .filter(s => s.type === 'tool_call' && s.tool_name !== 'todo_write' && s.tool_name !== 'use_skill')
          .map(s => formatToolName(s.tool_name))
      ),
    ]);

    _usingTodoPlan = allSteps.some(s => s.type === 'tool_call' && s.tool_name === 'todo_write');
    coworkProgressSteps.set(progressStepsFromHistory(allSteps));
    coworkSkillsUsed.set([]);
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
    if (_bgCoworkStreams.has(sessionId)) {
      try { await Backend.CancelCoworkStream?.(sessionId); } catch (_) {}
      _bgCoworkStreams.delete(sessionId);
      backgroundCoworkStreamingSessions.update(s => {
        s.delete(sessionId);
        return new Set(s);
      });
      cleanupListener();
    }

    await Backend.DeleteCoworkSession?.(sessionId);
    if (sessionId === get(activeCoworkTaskId)) {
      newCoworkTask();
    }
    await refreshCoworkHistory();
  } catch (e) {
    console.error('Failed to delete cowork task:', e);
  }
}

export async function renameCoworkTask(sessionId, newTitle) {
  if (!sessionId || !newTitle.trim()) return;
  try {
    await requireBackendMethod('RenameCoworkSession')(sessionId, newTitle.trim());
    
    // Update local task history title
    coworkTaskHistory.update(history =>
      history.map(item =>
        item.id === sessionId ? { ...item, title: newTitle.trim() } : item
      )
    );

    // If active session, update the workspace title store
    if (sessionId === get(activeCoworkTaskId)) {
      coworkTaskTitle.set(newTitle.trim());
    }
  } catch (e) {
    console.error('Failed to rename cowork task:', e);
  }
}
