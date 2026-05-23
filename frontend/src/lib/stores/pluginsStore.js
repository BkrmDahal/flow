import { writable, get } from 'svelte/store';
import {
  ListCommands,
  GetCommand,
  AddCommand,
  UpdateCommand,
  DeleteCommand,
  ListSkills,
  GetSkill,
  AddSkill,
  UpdateSkill,
  DeleteSkill,
  GetMasterPrompt,
  SaveMasterPrompt as SaveMasterPromptAPI,
  GetCoworkPrompt,
  SaveCoworkPrompt as SaveCoworkPromptAPI,
  ListMemoryFiles,
  GetMemoryFile,
  AddMemoryFile,
  SaveMemoryFile as SaveMemoryFileAPI,
  DeleteMemoryFile as DeleteMemoryFileAPI,
} from '../../../wailsjs/go/backend/App';

// ─── Commands State ───
export const commands = writable([]);
export const skills = writable([]);
export const activeSection = writable('commands'); // 'commands' | 'skills' | 'memory' | 'prompt' | 'cowork_prompt'
export const activeItemId = writable(null);
export const activeItemDetail = writable(null);
export const detailLoading = writable(false);

// ─── Master Prompt ───
export const masterPrompt = writable('');
export const masterPromptLoading = writable(false);

// ─── Cowork Prompt ───
export const coworkPrompt = writable('');
export const coworkPromptLoading = writable(false);

// ─── Memory Files ───
export const memoryFiles = writable([]);
export const activeMemoryName = writable(null);
export const activeMemoryDetail = writable(null);
export const memoryDetailLoading = writable(false);

// ─── Commands CRUD ───

export async function refreshCommands() {
  try {
    const items = await ListCommands();
    commands.set(items || []);
  } catch (e) {
    console.error('Failed to load commands:', e);
  }
}

export async function selectCommand(id) {
  activeSection.set('commands');
  activeItemId.set(id);
  detailLoading.set(true);
  try {
    const detail = await GetCommand(id);
    activeItemDetail.set(detail);
  } catch (e) {
    console.error('Failed to load command:', e);
    activeItemDetail.set(null);
  }
  detailLoading.set(false);
}

export async function addCommand(name, description, body) {
  const cmd = await AddCommand(name, description, body);
  await refreshCommands();
  return cmd;
}

export async function updateCommand(id, name, description, body) {
  await UpdateCommand(id, name, description, body);
  await refreshCommands();
  if (get(activeItemId) === id) {
    const detail = await GetCommand(id);
    activeItemDetail.set(detail);
  }
}

export async function deleteCommand(id) {
  await DeleteCommand(id);
  await refreshCommands();
  if (get(activeItemId) === id) {
    activeItemId.set(null);
    activeItemDetail.set(null);
  }
}

// ─── Skills CRUD ───

export async function refreshSkills() {
  try {
    const items = await ListSkills();
    skills.set(items || []);
  } catch (e) {
    console.error('Failed to load skills:', e);
  }
}

export async function selectSkill(id) {
  activeSection.set('skills');
  activeItemId.set(id);
  detailLoading.set(true);
  try {
    const detail = await GetSkill(id);
    activeItemDetail.set(detail);
  } catch (e) {
    console.error('Failed to load skill:', e);
    activeItemDetail.set(null);
  }
  detailLoading.set(false);
}

export async function addSkill(name, description, body) {
  const sk = await AddSkill(name, description, body);
  await refreshSkills();
  return sk;
}

export async function updateSkill(id, name, description, body) {
  await UpdateSkill(id, name, description, body);
  await refreshSkills();
  if (get(activeItemId) === id) {
    const detail = await GetSkill(id);
    activeItemDetail.set(detail);
  }
}

export async function deleteSkill(id) {
  await DeleteSkill(id);
  await refreshSkills();
  if (get(activeItemId) === id) {
    activeItemId.set(null);
    activeItemDetail.set(null);
  }
}

// ─── Master Prompt ───

export async function loadMasterPrompt() {
  masterPromptLoading.set(true);
  try {
    const body = await GetMasterPrompt();
    masterPrompt.set(body || '');
  } catch (e) {
    console.error('Failed to load master prompt:', e);
  }
  masterPromptLoading.set(false);
}

export async function saveMasterPrompt(body) {
  await SaveMasterPromptAPI(body);
  masterPrompt.set(body);
}

// ─── Cowork Prompt ───

export async function loadCoworkPrompt() {
  coworkPromptLoading.set(true);
  try {
    const body = await GetCoworkPrompt();
    coworkPrompt.set(body || '');
  } catch (e) {
    console.error('Failed to load cowork prompt:', e);
  }
  coworkPromptLoading.set(false);
}

export async function saveCoworkPrompt(body) {
  await SaveCoworkPromptAPI(body);
  coworkPrompt.set(body);
}


// ─── Memory Files CRUD ───

export async function refreshMemoryFiles() {
  try {
    const items = await ListMemoryFiles();
    memoryFiles.set(items || []);
  } catch (e) {
    console.error('Failed to load memory files:', e);
  }
}

export async function selectMemoryFile(name) {
  activeMemoryName.set(name);
  memoryDetailLoading.set(true);
  try {
    const detail = await GetMemoryFile(name);
    activeMemoryDetail.set(detail);
  } catch (e) {
    console.error('Failed to load memory file:', e);
    activeMemoryDetail.set(null);
  }
  memoryDetailLoading.set(false);
}

export async function addMemoryFile(name, body) {
  const mem = await AddMemoryFile(name, body);
  await refreshMemoryFiles();
  return mem;
}

export async function saveMemoryFile(name, body) {
  await SaveMemoryFileAPI(name, body);
  await refreshMemoryFiles();
  if (get(activeMemoryName) === name) {
    const detail = await GetMemoryFile(name);
    activeMemoryDetail.set(detail);
  }
}

export async function deleteMemoryFile(name) {
  await DeleteMemoryFileAPI(name);
  await refreshMemoryFiles();
  if (get(activeMemoryName) === name) {
    activeMemoryName.set(null);
    activeMemoryDetail.set(null);
  }
}

export function clearMemorySelection() {
  activeMemoryName.set(null);
  activeMemoryDetail.set(null);
}

// ─── Navigation ───

export function switchSection(section) {
  activeSection.set(section);
  activeItemId.set(null);
  activeItemDetail.set(null);
  activeMemoryName.set(null);
  activeMemoryDetail.set(null);
}

export async function refreshPlugins() {
  await Promise.all([refreshCommands(), refreshSkills()]);
}

export function clearPluginSelection() {
  activeItemId.set(null);
  activeItemDetail.set(null);
}
