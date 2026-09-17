import {
  API_BASE,
  DELETED_ALL,
  PATH_CHANGE_TOKEN,
  PATH_EXPENSE_DELETE,
  PATH_EXPENSE_DISTINCT,
  PATH_EXPENSE_INSERT,
  PATH_EXPENSE_SELECT,
  PATH_EXPENSE_SWITCH,
  PATH_EXPENSE_UPDATE,
  PATH_EXPORT_DB,
  PATH_FILE_META_DOWNLOAD,
  PATH_FILE_META_SELECT,
  PATH_IMPORT_DB,
  PATH_OPERATION_LOG_SELECT,
  PATH_PING,
  UPLOAD_FILE_KEY,
} from './config.js';
import { t } from './i18n.js';
import * as mock from './mock.js';
import { getAccountingCurrency, getClientToken, getServerToken, isMock } from './store.js';

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
  if (!serverToken) throw new Error(t('未解锁，缺少后端口令'));
  if (!crypto.subtle) throw new Error(t('当前环境不支持 Web Crypto，请改用 HTTPS 或 localhost 访问'));
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
  if (!resp || resp.code !== 200) throw new Error((resp && resp.msg) || t('请求失败'));
  const data = resp.data || {};
  return { object: data.object, count: data.count || 0 };
}

//后端的业务失败一律是 HTTP 200 + JSON，所以响应不是 JSON 就说明根本没走到业务层：
//反代配错、页面不是后端托的、请求体被网关掐了。直接 response.json() 的话，
//用户看到的会是 Unexpected token '<'，照着它什么也做不了
async function readJson(response) {
  const contentType = response.headers.get('Content-Type') || '';
  if (!contentType.includes('json')) {
    throw new Error(t('请求失败，HTTP {status}，响应不是 JSON。页面可能不是由后端托管的，可改用 mock 模式试用', { status: response.status }));
  }
  return response.json();
}

//fetch 只在「请求根本没发出去」时才抛：服务没起、端口不通、证书被拦。
//它抛的是浏览器自己的原文，词表里没有、也不该有，所以在这里换成一句能照着办的话
async function send(path, init) {
  try {
    return await fetch(API_BASE + path, init);
  } catch (err) {
    throw new Error(t('连不上后端接口 {path}，请确认服务在跑、地址没被反代改掉', { path: API_BASE + path }));
  }
}

async function postJson(path, body) {
  const response = await send(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(await authHeader()) },
    body: JSON.stringify(body || {}),
  });
  return unwrap(await readJson(response));
}

async function postFile(path, file, filename) {
  const form = new FormData();
  form.append(UPLOAD_FILE_KEY, file, filename);
  const response = await send(path, { method: 'POST', headers: await authHeader(), body: form });
  return unwrap(await readJson(response));
}

//文件名后端写了两份：filename= 是原始字节，中文走它会被按 ISO-8859-1 解成乱码，
//所以优先认 filename*=UTF-8''。后端转义用的是 url.QueryEscape，它把空格编成 +，
//而真正的 + 会被编成 %2B——所以剩下的 + 一定是空格变来的，先还原再解码
function dispositionName(disposition, fallback) {
  const encoded = /filename\*=UTF-8''([^;]+)/i.exec(disposition || '');
  if (encoded) {
    try {
      return decodeURIComponent(encoded[1].replace(/\+/g, ' '));
    } catch (err) {
      //转义串坏了就退回到下面的原始形态，不该让一个文件名把下载整条掀掉
    }
  }
  const plain = /filename="?([^";]+)"?/.exec(disposition || '');
  return plain ? plain[1] : fallback;
}

//导出与文件下载成功时流出的都是二进制，失败时才是 JSON 响应体，按 Content-Type 分流。
//返回形态与 mock 对齐：调用方拿到的一律是 {object,count} 这层外壳
async function postDownload(path, body, fallbackName) {
  const response = await send(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(await authHeader()) },
    body: JSON.stringify(body || {}),
  });
  const contentType = response.headers.get('Content-Type') || '';
  if (contentType.includes('json')) {
    unwrap(await response.json());
    throw new Error(t('下载失败，响应体异常'));
  }
  const file_name = dispositionName(response.headers.get('Content-Disposition'), fallbackName);
  return { object: { file_name, blob: await response.blob() }, count: 1 };
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

// ===== 对外能力：mock 与真实实现共用同一组签名与返回形态 =====

//探针只证明「这把口令开得了库」。它的响应体是共享库的 PingData，不是 {object,count} 那层外壳，
//所以真实模式下 object 恒为 undefined——别拿它的载荷做判断，成败只看有没有抛错
export function ping() {
  if (isMock()) return mock.ping(getClientToken());
  return postJson(PATH_PING, {});
}

export function selectExpense(inquiry) {
  if (isMock()) return mock.selectExpense(inquiry);
  return postJson(PATH_EXPENSE_SELECT, cleanInquiry(inquiry));
}

export function insertExpense(filename, csvText) {
  if (isMock()) return mock.insertExpense(filename, csvText, getAccountingCurrency());
  return postFile(PATH_EXPENSE_INSERT, new Blob([csvText], { type: 'text/csv' }), filename);
}

export function insertExpenseFile(file) {
  if (isMock()) return file.text().then((text) => mock.insertExpense(file.name, text, getAccountingCurrency()));
  return postFile(PATH_EXPENSE_INSERT, file, file.name);
}

export function deleteExpense(inquiry) {
  if (isMock()) return mock.deleteExpense(inquiry);
  return postJson(PATH_EXPENSE_DELETE, cleanInquiry(inquiry));
}

//整条送过去：后端以请求为底，再把 ID/版本号/记账币种/来源/时间戳按库里的旧值还原回来。
//前端另挑一份「可编辑字段清单」去拼请求体的话，清单一变长就会漏字段，且漏了不报错
export function updateExpense(expense) {
  if (isMock()) return mock.updateExpense(expense);
  return postJson(PATH_EXPENSE_UPDATE, expense);
}

export function switchAccountingCurrency(target) {
  if (isMock()) return mock.switchAccountingCurrency(target);
  return postJson(PATH_EXPENSE_SWITCH, { accounting_currency: target });
}

//支出类型、银行名称、卡号后四位、币种的候选下拉都走这一个 distinct 查询。
//含已删除：这个集合要与「记账币种切换」的作用域对齐，那边改的就是含已删除的全部明细
export function selectDistinct(field) {
  if (isMock()) return mock.selectDistinct(field);
  return postJson(PATH_EXPENSE_DISTINCT, { field, deleted: DELETED_ALL });
}

export function selectOperationLog(inquiry) {
  if (isMock()) return mock.selectOperationLog(inquiry);
  return postJson(PATH_OPERATION_LOG_SELECT, cleanInquiry(inquiry));
}

export function selectFileMeta(inquiry) {
  if (isMock()) return mock.selectFileMeta(inquiry);
  return postJson(PATH_FILE_META_SELECT, cleanInquiry(inquiry));
}

export function downloadFile(fileId) {
  if (isMock()) return mock.downloadFileBlob(fileId);
  return postDownload(PATH_FILE_META_DOWNLOAD, { id: fileId }, String(fileId));
}

export function changeToken(newToken) {
  if (isMock()) return mock.changeToken(getClientToken(), newToken);
  return postJson(PATH_CHANGE_TOKEN, { new_token: newToken });
}

export function exportDb() {
  if (isMock()) return mock.exportDb();
  return postDownload(PATH_EXPORT_DB, {}, 'jotcash.db');
}

export function importDb(file) {
  if (isMock()) return mock.importDb();
  return postFile(PATH_IMPORT_DB, file, file.name);
}

//种子只在 mock 模式下有意义，且 seed() 自己认得出已经播过，解锁与启动各调一次都不会重来
export function seedMock() {
  if (isMock()) mock.seed();
}

//换语言时把 mock 库里的种子数据重写成新语言。真实后端里没有这一步：那边存的是用户自己的数据
export function relocalizeMock() {
  if (isMock()) mock.relocalize();
}
