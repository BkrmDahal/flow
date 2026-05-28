/**
 * Schedules store — manages scheduled task CRUD via Wails backend.
 */
import { writable } from 'svelte/store';
import { Backend } from '../wails.js';

export const schedules = writable([]);
export const schedulesLoading = writable(false);

export async function refreshSchedules() {
  schedulesLoading.set(true);
  try {
    const list = await Backend.GetSchedules();
    schedules.set(list || []);
  } catch (e) {
    console.error('Failed to load schedules:', e);
    schedules.set([]);
  } finally {
    schedulesLoading.set(false);
  }
}

export async function createSchedule(task) {
  try {
    await Backend.SaveSchedule(task);
    await refreshSchedules();
  } catch (e) {
    console.error('Failed to save schedule:', e);
    throw e;
  }
}

export async function deleteSchedule(id) {
  try {
    await Backend.DeleteSchedule(id);
    await refreshSchedules();
  } catch (e) {
    console.error('Failed to delete schedule:', e);
    throw e;
  }
}

export async function toggleSchedule(id, enabled) {
  try {
    await Backend.ToggleSchedule(id, enabled);
    await refreshSchedules();
  } catch (e) {
    console.error('Failed to toggle schedule:', e);
    throw e;
  }
}

export async function runScheduleNow(id) {
  try {
    const sessionId = await Backend.RunScheduleNow(id);
    return sessionId;
  } catch (e) {
    console.error('Failed to run schedule:', e);
    throw e;
  }
}

// ── Catch-up setting ──

export const catchUpEnabled = writable(false);

export async function loadCatchUpSetting() {
  try {
    const enabled = await Backend.GetScheduleCatchUp();
    catchUpEnabled.set(!!enabled);
  } catch (e) {
    console.error('Failed to load catch-up setting:', e);
  }
}

export async function setCatchUpSetting(enabled) {
  try {
    await Backend.SetScheduleCatchUp(enabled);
    catchUpEnabled.set(enabled);
  } catch (e) {
    console.error('Failed to set catch-up setting:', e);
    throw e;
  }
}
