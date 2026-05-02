import { writable } from 'svelte/store';
import {
  ListSnippets,
  AddSnippet,
  UpdateSnippet,
  DeleteSnippet,
} from '../../../wailsjs/go/backend/App';

// ─── Snippets state ───
export const snippets = writable([]);
export const managingSnippets = writable(false);

// ─── Methods ───

export async function refreshSnippets() {
  try {
    const items = await ListSnippets();
    snippets.set(
      (items || []).map(s => ({
        id: s.id,
        trigger: s.trigger,
        expansion: s.expansion,
        createdAt: s.createdAt,
      }))
    );
  } catch (e) {
    console.error('Failed to load snippets:', e);
  }
}

export async function addSnippet(trigger, expansion) {
  try {
    await AddSnippet(trigger, expansion);
    await refreshSnippets();
  } catch (e) {
    console.error('Failed to add snippet:', e);
    throw e;
  }
}

export async function updateSnippet(id, trigger, expansion) {
  try {
    await UpdateSnippet(id, trigger, expansion);
    await refreshSnippets();
  } catch (e) {
    console.error('Failed to update snippet:', e);
    throw e;
  }
}

export async function deleteSnippet(id) {
  try {
    await DeleteSnippet(id);
    await refreshSnippets();
  } catch (e) {
    console.error('Failed to delete snippet:', e);
  }
}

export function showSnippetsPanel() {
  managingSnippets.set(true);
}

export function hideSnippetsPanel() {
  managingSnippets.set(false);
}
