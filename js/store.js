import { CURRENCY_DEFAULT, EXPENSE_COLUMN_DEFAULT, MODE_DEFAULT, MODE_MOCK } from './config.js';

//口令只活在 sessionStorage 里，关标签页即失效，绝不进 localStorage、不进 URL
const SESSION_KEY = 'jotcash.session';
//列显隐是偏好不是凭据，可以跨会话留着
const COLUMN_KEY = 'jotcash.columns';

let session = null;

function read() {
  if (session) return session;
  try {
    session = JSON.parse(sessionStorage.getItem(SESSION_KEY) || 'null');
  } catch (err) {
    session = null;
  }
  return session;
}

function write(value) {
  session = value;
  if (value) sessionStorage.setItem(SESSION_KEY, JSON.stringify(value));
  else sessionStorage.removeItem(SESSION_KEY);
}

//mode 不给默认值：没有数据来源的会话是坏会话，让它在调用处显形，好过在 api 那一层静默回落
export function unlock(serverToken, clientToken, accountingCurrency, mode) {
  write({ serverToken, clientToken, accountingCurrency, mode });
}

export function lock() {
  write(null);
}

export function isUnlocked() {
  return read() !== null;
}

export function getServerToken() {
  const value = read();
  return value ? value.serverToken : '';
}

export function getClientToken() {
  const value = read();
  return value ? value.clientToken : '';
}

//数据来源与口令同生共死：锁定即清，关标签页即失效，刷新之后仍是同一门来源
export function getMode() {
  const value = read();
  return value && value.mode ? value.mode : MODE_DEFAULT;
}

export function isMock() {
  return getMode() === MODE_MOCK;
}

export function getAccountingCurrency() {
  const value = read();
  return value ? value.accountingCurrency : CURRENCY_DEFAULT;
}

//记账币种是逐请求携带的口径，换了之后新数据才按它入库，已有数据要走记账币种切换
export function setAccountingCurrency(accountingCurrency) {
  const value = read();
  if (!value) return;
  write({ ...value, accountingCurrency });
}

//换口令成功后前端要以新口令继续，不能让用户重新解锁一次
export function setClientToken(clientToken) {
  const value = read();
  if (!value) return;
  write({ ...value, clientToken });
}

export function getColumns() {
  try {
    const columns = JSON.parse(localStorage.getItem(COLUMN_KEY) || 'null');
    if (Array.isArray(columns) && columns.length > 0) return columns;
  } catch (err) {
    //偏好读坏了就回默认列，不该拦住页面
  }
  return EXPENSE_COLUMN_DEFAULT.slice();
}

export function setColumns(columns) {
  localStorage.setItem(COLUMN_KEY, JSON.stringify(columns));
}

//语言与主题都是偏好不是凭据，与列显隐同一档，跨会话留着。
//键名同时写在 index.html 的首屏主题脚本里，改这里要一并改那边
export const LANG_KEY = 'jotcash.lang';
export const THEME_KEY = 'jotcash.theme';
export const AUTO_LOCK_KEY = 'jotcash.auto_lock';

//隐私模式下 storage 的读写都可能直接抛，取不到偏好该回落到自动判定，不该把整页带崩
function readPreference(key) {
  try {
    return localStorage.getItem(key) || '';
  } catch (err) {
    return '';
  }
}

function writePreference(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch (err) {
    //存不下就只在本次会话内生效，不影响当前这一屏
  }
}

export function getLang() {
  return readPreference(LANG_KEY);
}

export function setLang(lang) {
  writePreference(LANG_KEY, lang);
}

export function getTheme() {
  return readPreference(THEME_KEY);
}

export function setTheme(theme) {
  writePreference(THEME_KEY, theme);
}

export function getAutoLockPreference() {
  return readPreference(AUTO_LOCK_KEY);
}

export function setAutoLockPreference(autoLock) {
  writePreference(AUTO_LOCK_KEY, autoLock);
}
