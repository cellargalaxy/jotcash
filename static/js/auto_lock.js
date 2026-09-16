import { getAutoLockPreference, isUnlocked, setAutoLockPreference } from './store.js';

//自动锁定超时选项：5分钟、10分钟、30分钟、1小时、永不自动锁定
export const AUTO_LOCK_OPTIONS = [
  { value: 5, name: '5分钟' },
  { value: 10, name: '10分钟' },
  { value: 30, name: '30分钟' },
  { value: 60, name: '1小时' },
  { value: 0, name: '永不自动锁定' },
];

export const AUTO_LOCK_DEFAULT = 5;

let currentMinutes = null;
const listeners = [];

export function getAutoLock() {
  if (currentMinutes === null) {
    const saved = getAutoLockPreference();
    if (saved !== '') {
      const num = Number(saved);
      currentMinutes = AUTO_LOCK_OPTIONS.some((opt) => opt.value === num) ? num : AUTO_LOCK_DEFAULT;
    } else {
      currentMinutes = AUTO_LOCK_DEFAULT;
    }
  }
  return currentMinutes;
}

export function setAutoLock(minutes) {
  const num = Number(minutes);
  currentMinutes = AUTO_LOCK_OPTIONS.some((opt) => opt.value === num) ? num : AUTO_LOCK_DEFAULT;
  setAutoLockPreference(String(currentMinutes));
  resetAutoLockTimer();
  for (const listener of listeners) {
    try {
      listener(currentMinutes);
    } catch (_) {}
  }
}

export function watchAutoLock(listener) {
  listeners.push(listener);
}

let lastActivityTime = Date.now();
let timerId = null;
let onLockHandler = null;

export function recordActivity() {
  lastActivityTime = Date.now();
}

export function getLastActivity() {
  return lastActivityTime;
}

export function setLastActivity(time) {
  lastActivityTime = time;
}

export function resetAutoLockTimer() {
  if (timerId) {
    clearInterval(timerId);
    timerId = null;
  }
  lastActivityTime = Date.now();
  const minutes = getAutoLock();
  if (minutes <= 0) return;

  timerId = setInterval(checkAutoLock, 1000);
  if (timerId && typeof timerId.unref === 'function') {
    timerId.unref();
  }
}

export function stopAutoLockTimer() {
  if (timerId) {
    clearInterval(timerId);
    timerId = null;
  }
}

export function checkAutoLock() {
  const minutes = getAutoLock();
  if (minutes <= 0) return;
  if (!isUnlocked()) {
    lastActivityTime = Date.now();
    return;
  }
  const timeoutMs = minutes * 60 * 1000;
  if (Date.now() - lastActivityTime >= timeoutMs) {
    if (timerId) {
      clearInterval(timerId);
      timerId = null;
    }
    if (typeof onLockHandler === 'function') {
      onLockHandler();
    }
  }
}

export function initAutoLock(onLock) {
  onLockHandler = onLock;
  lastActivityTime = Date.now();

  const events = ['mousedown', 'mousemove', 'keydown', 'scroll', 'touchstart', 'click'];
  if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    for (const event of events) {
      window.addEventListener(event, recordActivity, { passive: true });
    }
  }

  resetAutoLockTimer();
}
