import {
  API_BASE,
  PATH_CHANGE_TOKEN,
  PATH_EXPENSE_DELETE,
  PATH_EXPENSE_INSERT,
  PATH_EXPENSE_SELECT,
  PATH_EXPORT_DB,
  PATH_FILE_META_SELECT,
  PATH_IMPORT_DB,
  PATH_OPERATION_LOG_SELECT,
  PATH_PING,
  UPLOAD_FILE_KEY,
  USE_MOCK,
} from './config.js';
import * as mock from './mock.js';
import { getAccountingCurrency, getClientToken, getServerToken } from './store.js';

// ===== jwt =====

function base64Url(data) {
  const bytes = data instanceof Uint8Array ? data : new TextEncoder().encode(data);
  let text = '';
  for (const byte of bytes) text += String.fromCharCode(byte);
  return btoa(text).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

//后端 util.EnJwt 用的密钥是 sha256(后端口令) 的原始字节，不是十六进制串，签名前必须先摘要
async function signKey(serverToken) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(serverToken));
  return crypto.subtle.importKey('raw', digest, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
}

//口令与记账币种都签进 jwt，且只允许走请求头；一旦落到 query 就会被访问日志原样记下来
async function signJwt() {
  const serverToken = getServerToken();
  if (!serverToken) throw new Error('未解锁，缺少后端口令');
  if (!crypto.subtle) throw new Error('当前环境不支持 Web Crypto，请改用 HTTPS 或 localhost 访问');
  const now = Math.floor(Date.now() / 1000);
  const header = base64Url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const payload = base64Url(JSON.stringify({
    iat: now,
    exp: now + 300,
    client_token: getClientToken(),
    accounting_currency: getAccountingCurrency(),
  }));
  const key = await signKey(serverToken);
  const signature = await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(`${header}.${payload}`));
  return `${header}.${payload}.${base64Url(new Uint8Array(signature))}`;
}

async function authHeader() {
  return { Authorization: `Bearer ${await signJwt()}` };
}

// ===== 请求 =====

//业务失败时 HTTP 状态码仍是 200，成败一律看响应体里的 code
function unwrap(resp) {
  if (!resp || resp.code !== 200) throw new Error((resp && resp.msg) || '请求失败');
  const data = resp.data || {};
  return { object: data.object, count: data.count || 0 };
}

async function postJson(path, body) {
  const response = await fetch(API_BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(await authHeader()) },
    body: JSON.stringify(body || {}),
  });
  return unwrap(await response.json());
}

async function postFile(path, file, filename) {
  const form = new FormData();
  form.append(UPLOAD_FILE_KEY, file, filename);
  const response = await fetch(API_BASE + path, { method: 'POST', headers: await authHeader(), body: form });
  return unwrap(await response.json());
}

//导出成功时流出的是二进制库文件，失败时才是 JSON 响应体，按 Content-Type 分流
async function postDownload(path) {
  const response = await fetch(API_BASE + path, { method: 'POST', headers: await authHeader() });
  const contentType = response.headers.get('Content-Type') || '';
  if (contentType.includes('application/json')) {
    unwrap(await response.json());
    throw new Error('数据库导出，响应体异常');
  }
  const disposition = response.headers.get('Content-Disposition') || '';
  const matched = /filename="?([^";]+)"?/.exec(disposition);
  return { file_name: matched ? matched[1] : 'jotcash.db', blob: await response.blob() };
}

//时间字段的零值、留空的模糊匹配都不能进请求体：Go 的 time.Time 吃不下空串，空数组会把结果筛没
function cleanInquiry(inquiry) {
  const body = {};
  for (const key in inquiry) {
    const value = inquiry[key];
    if (value === undefined || value === null || value === '') continue;
    if (Array.isArray(value) && value.length === 0) continue;
    body[key] = value;
  }
  return body;
}

function notImplemented(name) {
  return Promise.reject(new Error(`${name}，后端接口尚未实现，当前只能在 mock 模式下体验`));
}

// ===== 对外能力：mock 与真实实现共用同一组签名与返回形态 =====

export function ping() {
  if (USE_MOCK) return mock.ping(getClientToken());
  return postJson(PATH_PING, {});
}

export function selectExpense(inquiry) {
  if (USE_MOCK) return mock.selectExpense(inquiry);
  return postJson(PATH_EXPENSE_SELECT, cleanInquiry(inquiry));
}

export function insertExpense(filename, csvText) {
  if (USE_MOCK) return mock.insertExpense(filename, csvText, getAccountingCurrency());
  return postFile(PATH_EXPENSE_INSERT, new Blob([csvText], { type: 'text/csv' }), filename);
}

export function insertExpenseFile(file) {
  if (USE_MOCK) return file.text().then((text) => mock.insertExpense(file.name, text, getAccountingCurrency()));
  return postFile(PATH_EXPENSE_INSERT, file, file.name);
}

export function deleteExpense(inquiry) {
  if (USE_MOCK) return mock.deleteExpense(inquiry);
  return postJson(PATH_EXPENSE_DELETE, cleanInquiry(inquiry));
}

export function updateExpense(expense) {
  if (USE_MOCK) return mock.updateExpense(expense);
  return notImplemented('明细编辑');
}

export function switchAccountingCurrency(target) {
  if (USE_MOCK) return mock.switchAccountingCurrency(target);
  return notImplemented('记账币种切换');
}

export function selectExpenseType() {
  if (USE_MOCK) return mock.selectExpenseType();
  return notImplemented('支出类型候选');
}

export function selectAccountingCurrency() {
  if (USE_MOCK) return mock.selectAccountingCurrency();
  return notImplemented('记账币种集合');
}

export function selectOperationLog(inquiry) {
  if (USE_MOCK) return mock.selectOperationLog(inquiry);
  return postJson(PATH_OPERATION_LOG_SELECT, cleanInquiry(inquiry));
}

export function selectFileMeta(inquiry) {
  if (USE_MOCK) return mock.selectFileMeta(inquiry);
  return postJson(PATH_FILE_META_SELECT, cleanInquiry(inquiry));
}

export function downloadFile(fileId) {
  if (USE_MOCK) return mock.downloadFileBlob(fileId);
  return notImplemented('文件下载');
}

export function changeToken(newToken) {
  if (USE_MOCK) return mock.changeToken(getClientToken(), newToken);
  return postJson(PATH_CHANGE_TOKEN, { new_token: newToken });
}

export function exportDb() {
  if (USE_MOCK) return mock.exportDb();
  return postDownload(PATH_EXPORT_DB);
}

export function importDb(file) {
  if (USE_MOCK) return mock.importDb(file.name);
  return postFile(PATH_IMPORT_DB, file, file.name);
}

export function seedMock() {
  if (USE_MOCK) mock.seed();
}
