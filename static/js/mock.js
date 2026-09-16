import { AMOUNT_SCALE, CSV_FIELDS, DELETED_ALL, DELETED_NO, DELETED_ONLY, EXPENSE_FIELDS } from './config.js';
import { LANGS, t, textOf } from './i18n.js';
import { addMonth, dateToRfc3339, formatDate, monthOf, parseCsv, toCsv } from './util.js';

//内存库：与后端三张表同名同字段，筛选、排序、分页、乐观锁都按后端语义复刻，
//切到真实接口时 api.js 不必改调用方一行
const db = {
  expense: [],
  operationLog: [],
  fileMeta: [],
  fileBlob: {},
  clientToken: '',
};

//种子数据是我们自己造的演示内容，它跟着界面语言走，所以要记住哪些文件是种子文件
const seedFileIds = new Set();

// ===== ID 与时间 =====

let idSeq = 0;

function pad(value, len) {
  return String(value).padStart(len, '0');
}

//与后端 util.GenIdByTime 同形：yyMMddHHmmss + 4 位序号，16 位十进制，仍在 JS 安全整数内
function genId() {
  const now = new Date();
  idSeq = (idSeq + 1) % 10000;
  const text =
    pad(now.getFullYear() % 100, 2) +
    pad(now.getMonth() + 1, 2) +
    pad(now.getDate(), 2) +
    pad(now.getHours(), 2) +
    pad(now.getMinutes(), 2) +
    pad(now.getSeconds(), 2) +
    pad(idSeq, 4);
  return Number(text);
}

function now() {
  return new Date().toISOString();
}

function fail(message) {
  return Promise.reject(new Error(message));
}

function ok(object, count) {
  return Promise.resolve({ object, count: count === undefined ? 0 : count });
}

// ===== 汇率 =====

//mock 的外部汇率源：只给常见币种对人民币的价，其余币种对走「取不到汇率」分支
const RATE_TO_CNY = { CNY: '1', USD: '7.12', EUR: '7.78', JPY: '0.048', HKD: '0.91', GBP: '9.05', KRW: '0.0053', SGD: '5.31' };

function getExchangeRate(expenseCurrency, accountingCurrency) {
  if (expenseCurrency === accountingCurrency) return '1';
  const from = RATE_TO_CNY[expenseCurrency];
  const to = RATE_TO_CNY[accountingCurrency];
  if (!from || !to) return '';
  return new Decimal(from).div(new Decimal(to)).toDecimalPlaces(6).toString();
}

function checkCurrency(name, code) {
  if (/^[A-Z]{3}$/.test(code || '')) return '';
  return `${name}，不在枚举内: ${code || ''}`;
}

// ===== 派生字段 =====

//与后端 fillExpense 同一套派生：汇率 → 记账金额 → 摊销起止月
function fillExpense(object, accountingCurrency) {
  if (!object.accounting_currency) object.accounting_currency = accountingCurrency;
  let message = checkCurrency('记账币种', object.accounting_currency);
  if (message) return message;
  message = checkCurrency('支出币种', object.expense_currency);
  if (message) return message;

  let rate = object.exchange_rate;
  if (object.expense_currency === object.accounting_currency) {
    rate = '1';
  } else if (!rate || new Decimal(rate).isZero()) {
    rate = getExchangeRate(object.expense_currency, object.accounting_currency);
    if (!rate) return `获取汇率，取不到 ${object.expense_currency}→${object.accounting_currency} 的汇率`;
  }
  if (!new Decimal(rate).gt(0)) return `折算汇率非正: ${rate}`;

  if (!(object.amortization_months >= 1)) object.amortization_months = 1;
  object.exchange_rate = rate;
  object.accounting_amount = new Decimal(object.expense_amount).mul(new Decimal(rate)).toDecimalPlaces(AMOUNT_SCALE).toString();
  const startMonth = monthOf(object.expense_date.slice(0, 10));
  object.amortization_start_month = dateToRfc3339(`${startMonth}-01`);
  object.amortization_end_month = dateToRfc3339(`${addMonth(startMonth, object.amortization_months - 1)}-01`);
  return '';
}

// ===== 通用筛选 =====

function inList(list, value) {
  return !list || list.length === 0 || list.some((item) => String(item) === String(value));
}

function likeMatch(keyword, value) {
  if (!keyword) return true;
  return String(value || '').toLowerCase().includes(String(keyword).toLowerCase());
}

function timeIn(value, start, end) {
  if (!value) return !start && !end;
  const at = new Date(value).getTime();
  if (start && at < new Date(start).getTime()) return false;
  if (end && at > new Date(end).getTime()) return false;
  return true;
}

function checkTimeRange(start, end) {
  if (!start || !end) return '';
  return new Date(start).getTime() > new Date(end).getTime() ? '查询，时间区间倒挂' : '';
}

//排序白名单越界后端直接报错，mock 同样报错，免得前端偷偷传了个不支持的值还看着正常
function sortRows(rows, sort, whitelist, comparators) {
  if (!whitelist.includes(sort)) return `排序，不在白名单内: ${sort}`;
  const [field, direction] = sort.split(' ');
  const sign = direction === 'desc' ? -1 : 1;
  const compare = comparators[field];
  rows.sort((left, right) => sign * compare(left, right));
  return '';
}

//与后端 rdb.pageLimit 同语义：分页参数非正即不限，全量导出与图表统计要的正是这个
function pageRows(rows, page, pageSize) {
  if (!pageSize || pageSize <= 0) return rows;
  const index = page && page > 1 ? page - 1 : 0;
  return rows.slice(index * pageSize, index * pageSize + pageSize);
}

function compareNumber(left, right) {
  return Number(left) - Number(right);
}

function compareTime(left, right) {
  return new Date(left || 0).getTime() - new Date(right || 0).getTime();
}

function compareText(left, right) {
  return String(left || '').localeCompare(String(right || ''));
}

// ===== 明细 =====

const EXPENSE_SORT_WHITELIST = [
  'id asc', 'id desc',
  'expense_date asc', 'expense_date desc',
  'expense_amount asc', 'expense_amount desc',
  'created_at asc', 'created_at desc',
  'updated_at asc', 'updated_at desc',
];

const EXPENSE_COMPARATORS = {
  id: (left, right) => compareNumber(left.id, right.id),
  expense_date: (left, right) => compareTime(left.expense_date, right.expense_date),
  expense_amount: (left, right) => new Decimal(left.expense_amount).cmp(new Decimal(right.expense_amount)),
  created_at: (left, right) => compareTime(left.created_at, right.created_at),
  updated_at: (left, right) => compareTime(left.updated_at, right.updated_at),
};

function filterExpense(inquiry) {
  return db.expense.filter((row) => {
    switch (inquiry.deleted) {
      case DELETED_ONLY:
        if (!row.deleted_at) return false;
        break;
      case DELETED_ALL:
        break;
      default:
        if (row.deleted_at) return false;
    }
    if (!inList(inquiry.id, row.id)) return false;
    if (!inList(inquiry.bank_name, row.bank_name)) return false;
    if (!inList(inquiry.card_last_4, row.card_last_4)) return false;
    if (!inList(inquiry.expense_currency, row.expense_currency)) return false;
    if (!inList(inquiry.accounting_currency, row.accounting_currency)) return false;
    if (inquiry.accounting_currency_not && inquiry.accounting_currency_not.length > 0
      && inList(inquiry.accounting_currency_not, row.accounting_currency)) return false;
    if (!inList(inquiry.expense_type, row.expense_type)) return false;
    if (!inList(inquiry.amortization_months, row.amortization_months)) return false;
    if (!inList(inquiry.operation_id, row.operation_id)) return false;
    if (!inList(inquiry.file_id, row.file_id)) return false;
    if (!inList(inquiry.version, row.version)) return false;
    if (!timeIn(row.expense_date, inquiry.expense_date_start, inquiry.expense_date_end)) return false;
    if (inquiry.expense_amount_min !== undefined && inquiry.expense_amount_min !== null
      && new Decimal(row.expense_amount).lt(new Decimal(inquiry.expense_amount_min))) return false;
    if (inquiry.expense_amount_max !== undefined && inquiry.expense_amount_max !== null
      && new Decimal(row.expense_amount).gt(new Decimal(inquiry.expense_amount_max))) return false;
    if (!likeMatch(inquiry.counterparty_like, row.counterparty)) return false;
    if (!likeMatch(inquiry.remark_like, row.remark)) return false;
    return true;
  });
}

function checkExpenseInquiry(inquiry) {
  const deleted = inquiry.deleted || DELETED_NO;
  if (![DELETED_NO, DELETED_ALL, DELETED_ONLY].includes(deleted)) return `查询明细，删除筛选非法: ${deleted}`;
  const message = checkTimeRange(inquiry.expense_date_start, inquiry.expense_date_end);
  if (message) return message;
  const min = inquiry.expense_amount_min;
  const max = inquiry.expense_amount_max;
  if (min !== undefined && min !== null && max !== undefined && max !== null && new Decimal(min).gt(new Decimal(max))) {
    return '查询，金额区间倒挂';
  }
  return '';
}

export function selectExpense(inquiry) {
  const message = checkExpenseInquiry(inquiry);
  if (message) return fail(message);
  const rows = filterExpense(inquiry);
  const sortMessage = sortRows(rows, inquiry.sort || 'expense_date desc', EXPENSE_SORT_WHITELIST, EXPENSE_COMPARATORS);
  if (sortMessage) return fail(sortMessage);
  return ok(pageRows(rows, inquiry.page, inquiry.page_size).map((row) => ({ ...row })), rows.length);
}

export function insertExpense(filename, csvText, accountingCurrency) {
  const lines = parseCsv(csvText);
  const header = (lines[0] || []).map((cell) => cell.trim());
  const expect = CSV_FIELDS.map((field) => field.column);
  if (header.length !== expect.length || header.some((cell, index) => cell !== expect[index])) {
    return fail('解析明细，文件格式无法识别');
  }
  if (lines.length < 2) return fail('明细入库，入库内容为空');

  const operationId = genId();
  const fileId = genId();
  const at = now();
  const objects = [];
  for (let index = 1; index < lines.length; index += 1) {
    const cells = lines[index];
    const value = (key) => (cells[CSV_FIELDS.findIndex((field) => field.key === key)] || '').trim();
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value('expense_date'))) {
      return fail(`解析CSV，第${index + 1}行，支出日期非法: ${value('expense_date')}`);
    }
    if (!value('expense_amount') || isNaN(Number(value('expense_amount')))) {
      return fail(`解析CSV，第${index + 1}行，支出金额非法: ${value('expense_amount')}`);
    }
    //填了折算汇率就必须填记账币种，与后端 base_csv.parseExpense 的判据一致
    if (value('exchange_rate') && !value('accounting_currency')) {
      return fail(`解析CSV，第${index + 1}行，填了折算汇率就必须填记账币种`);
    }
    //后端走的是 Str2Int，小数串解不出整数就是 0，一并落进「小于 1」这条判据里被拒
    const months = value('amortization_months');
    if (months && !(Number.isInteger(Number(months)) && Number(months) >= 1)) {
      return fail(`解析CSV，第${index + 1}行，摊销月数非法: ${months}`);
    }
    const object = {
      id: genId(),
      bank_name: value('bank_name'),
      card_last_4: value('card_last_4'),
      expense_date: dateToRfc3339(value('expense_date')),
      expense_currency: value('expense_currency').toUpperCase(),
      expense_amount: value('expense_amount'),
      counterparty: value('counterparty'),
      remark: value('remark'),
      exchange_rate: value('exchange_rate'),
      accounting_currency: value('accounting_currency').toUpperCase(),
      accounting_amount: '',
      expense_type: value('expense_type'),
      amortization_months: months ? Number(months) : 0,
      amortization_start_month: '',
      amortization_end_month: '',
      operation_id: operationId,
      file_id: fileId,
      version: 1,
      created_at: at,
      updated_at: at,
      deleted_at: null,
    };
    const message = fillExpense(object, accountingCurrency);
    if (message) return fail(`解析明细，第${index}笔，${message}`);
    objects.push(object);
  }

  db.expense.push(...objects);
  db.fileMeta.push({
    id: fileId,
    file_hash: `mock${String(fileId).slice(-12)}`,
    file_name: filename,
    file_size: new Blob([csvText]).size,
    operation_id: operationId,
    created_at: at,
  });
  db.fileBlob[fileId] = csvText;
  db.operationLog.push({
    id: operationId,
    operation_type: '数据入库',
    object_type: '',
    object_id: 0,
    summary: `入库 ${objects.length} 笔，来源 ${filename}`,
    changes: '',
    result: '成功',
    created_at: at,
  });
  return ok(operationId, objects.length);
}

export function deleteExpense(inquiry) {
  const message = checkExpenseInquiry({ ...inquiry, deleted: DELETED_NO });
  if (message) return fail(message);
  //批量删除强制只删未删除的行，已删除行跳过、不覆盖它的删除时间
  const rows = filterExpense({ ...inquiry, deleted: DELETED_NO, page: 0, page_size: 0 });
  const at = now();
  for (const row of rows) row.deleted_at = at;
  db.operationLog.push({
    id: genId(),
    operation_type: '明细删除',
    object_type: '',
    object_id: 0,
    summary: '批量软删除明细',
    changes: '',
    result: '成功',
    created_at: at,
  });
  return ok(null, rows.length);
}

//E-4 单行编辑：带版本号乐观锁；来源审计ID/文件ID 不动，只记一条明细编辑审计
export function updateExpense(object) {
  const row = db.expense.find((item) => item.id === object.id);
  if (!row) return fail(`明细编辑，明细不存在: ${object.id}`);
  if (row.deleted_at) return fail('明细编辑，已删除明细不可编辑');
  if (row.version !== object.version) return fail('数据已落后，请刷新页面重新加载');

  const before = { ...row };
  const next = { ...row };
  for (const field of EXPENSE_FIELDS) {
    if (field.editable) next[field.key] = object[field.key];
  }
  //改支出日期或支出币种都要按新值重新取汇率，手填了汇率则以手填值为准
  if (before.expense_date !== next.expense_date || before.expense_currency !== next.expense_currency) {
    if (next.exchange_rate === before.exchange_rate) next.exchange_rate = '';
  }
  const message = fillExpense(next, row.accounting_currency);
  if (message) return fail(`明细编辑，${message}`);
  next.version = row.version + 1;
  next.updated_at = now();

  //与后端 model.ExpenseChanges 同形：记前后两份整快照，逐字段比对交给渲染层做
  const changes = JSON.stringify({ before, after: next });
  Object.assign(row, next);
  db.operationLog.push({
    id: genId(),
    operation_type: '明细编辑',
    object_type: '支出明细',
    object_id: row.id,
    summary: `编辑明细 ${row.id}`,
    changes,
    result: '成功',
    created_at: next.updated_at,
  });
  return ok({ ...row }, 1);
}

//F-4 记账币种切换：记账币种≠目标的全部明细（含已删除）逐笔重取汇率并无条件覆盖
export function switchAccountingCurrency(target) {
  const message = checkCurrency('记账币种', target);
  if (message) return fail(message);
  const rows = db.expense.filter((row) => row.accounting_currency !== target);
  let done = 0;
  let failed = 0;
  for (const row of rows) {
    const rate = row.expense_currency === target ? '1' : getExchangeRate(row.expense_currency, target);
    if (!rate) {
      failed += 1;
      continue;
    }
    row.exchange_rate = rate;
    row.accounting_currency = target;
    row.accounting_amount = new Decimal(row.expense_amount).mul(new Decimal(rate)).toDecimalPlaces(AMOUNT_SCALE).toString();
    row.updated_at = now();
    done += 1;
  }
  db.operationLog.push({
    id: genId(),
    operation_type: '记账币种切换',
    object_type: '',
    object_id: 0,
    summary: `切换记账币种为 ${target}，成功 ${done} 笔，失败 ${failed} 笔`,
    changes: '',
    result: failed === 0 ? '成功' : '部分成功',
    created_at: now(),
  });
  return ok({ done, failed }, done);
}

//候选下拉的取值来自运行时 distinct，不建字典表
const DISTINCT_FIELDS = ['expense_type', 'bank_name', 'card_last_4', 'expense_currency', 'accounting_currency'];

export function selectDistinct(field) {
  if (!DISTINCT_FIELDS.includes(field)) return fail(`查询候选，字段不支持: ${field}`);
  const values = new Set();
  for (const row of db.expense) {
    if (row[field]) values.add(String(row[field]));
  }
  const object = [...values].sort();
  return ok(object, object.length);
}

// ===== 审计 =====

const OPERATION_LOG_SORT_WHITELIST = ['id asc', 'id desc', 'created_at asc', 'created_at desc'];

const OPERATION_LOG_COMPARATORS = {
  id: (left, right) => compareNumber(left.id, right.id),
  created_at: (left, right) => compareTime(left.created_at, right.created_at),
};

export function selectOperationLog(inquiry) {
  const message = checkTimeRange(inquiry.created_at_start, inquiry.created_at_end);
  if (message) return fail(message);
  const rows = db.operationLog.filter((row) => {
    if (!inList(inquiry.id, row.id)) return false;
    if (!inList(inquiry.operation_type, row.operation_type)) return false;
    if (!inList(inquiry.object_type, row.object_type)) return false;
    if (!inList(inquiry.object_id, row.object_id)) return false;
    if (!inList(inquiry.result, row.result)) return false;
    if (!likeMatch(inquiry.summary_like, row.summary)) return false;
    if (!timeIn(row.created_at, inquiry.created_at_start, inquiry.created_at_end)) return false;
    return true;
  });
  const sortMessage = sortRows(rows, inquiry.sort || 'created_at desc', OPERATION_LOG_SORT_WHITELIST, OPERATION_LOG_COMPARATORS);
  if (sortMessage) return fail(sortMessage);
  return ok(pageRows(rows, inquiry.page, inquiry.page_size).map((row) => ({ ...row })), rows.length);
}

// ===== 文件 =====

const FILE_META_SORT_WHITELIST = ['id asc', 'id desc', 'file_name asc', 'file_name desc', 'created_at asc', 'created_at desc'];

const FILE_META_COMPARATORS = {
  id: (left, right) => compareNumber(left.id, right.id),
  file_name: (left, right) => compareText(left.file_name, right.file_name),
  created_at: (left, right) => compareTime(left.created_at, right.created_at),
};

export function selectFileMeta(inquiry) {
  const message = checkTimeRange(inquiry.created_at_start, inquiry.created_at_end);
  if (message) return fail(message);
  const rows = db.fileMeta.filter((row) => {
    if (!inList(inquiry.id, row.id)) return false;
    if (!inList(inquiry.file_hash, row.file_hash)) return false;
    if (!inList(inquiry.file_name, row.file_name)) return false;
    if (!inList(inquiry.operation_id, row.operation_id)) return false;
    if (!likeMatch(inquiry.file_name_like, row.file_name)) return false;
    if (!timeIn(row.created_at, inquiry.created_at_start, inquiry.created_at_end)) return false;
    return true;
  });
  const sortMessage = sortRows(rows, inquiry.sort || 'created_at desc', FILE_META_SORT_WHITELIST, FILE_META_COMPARATORS);
  if (sortMessage) return fail(sortMessage);
  return ok(pageRows(rows, inquiry.page, inquiry.page_size).map((row) => ({ ...row })), rows.length);
}

//D-3：下载前重算哈希与主键比对，mock 里只演示这条链路的成败分支
export function downloadFileBlob(fileId) {
  const meta = db.fileMeta.find((row) => row.id === fileId);
  if (!meta) return fail(`文件下载，文件不存在: ${fileId}`);
  const data = db.fileBlob[fileId];
  if (data === undefined) return fail(`文件下载，内容缺失: ${fileId}`);
  return ok({ file_name: meta.file_name, data }, 1);
}

// ===== 口令与整库 =====

//首个带口令的请求定下这把口令，与当前后端「第一个开事务的请求带什么口令，库就用什么口令」一致
export function ping(clientToken) {
  if (!clientToken) return fail('获取口令，为空');
  if (!db.clientToken) {
    db.clientToken = clientToken;
    return ok({ sn: 'jotcash-mock', ts: Math.floor(Date.now() / 1000) }, 0);
  }
  if (db.clientToken !== clientToken) return fail('打开数据库，口令错误或数据库文件损坏');
  return ok({ sn: 'jotcash-mock', ts: Math.floor(Date.now() / 1000) }, 0);
}

//副本改密 → 新口令校验能打开 → 原子替换，任一步失败原库原封不动
export function changeToken(clientToken, newToken) {
  return ping(clientToken).then(() => {
    db.clientToken = newToken;
    db.operationLog.push({
      id: genId(),
      operation_type: '更换口令',
      object_type: '',
      object_id: 0,
      summary: '数据库已用新口令重新加密',
      changes: '',
      result: '成功',
      created_at: now(),
    });
    return { object: null, count: 0 };
  });
}

export function exportDb() {
  db.operationLog.push({
    id: genId(),
    operation_type: '数据库导出',
    object_type: '',
    object_id: 0,
    summary: '导出加密数据库快照',
    changes: '',
    result: '成功',
    created_at: now(),
  });
  //mock 没有真实的加密库文件，导出的是同构的 JSON 快照，够验收「导出即下载」这条链路
  const snapshot = JSON.stringify({ expense: db.expense, operation_log: db.operationLog, file_meta: db.fileMeta }, null, 2);
  return ok({ file_name: `jotcash-mock-${genId()}.json`, data: snapshot }, 0);
}

export function importDb() {
  db.operationLog.push({
    id: genId(),
    operation_type: '数据库导入',
    object_type: '',
    object_id: 0,
    summary: '导入加密数据库，整库覆盖',
    changes: '',
    result: '成功',
    created_at: now(),
  });
  return ok(null, 0);
}

// ===== 种子数据 =====

function seedExpense(seed) {
  const object = {
    id: genId(),
    bank_name: seed.bank_name || '',
    card_last_4: seed.card_last_4 || '',
    expense_date: dateToRfc3339(seed.expense_date),
    expense_currency: seed.expense_currency,
    expense_amount: seed.expense_amount,
    counterparty: seed.counterparty || '',
    remark: seed.remark || '',
    exchange_rate: seed.exchange_rate || '',
    accounting_currency: seed.accounting_currency,
    accounting_amount: '',
    expense_type: seed.expense_type || '',
    amortization_months: seed.amortization_months || 1,
    amortization_start_month: '',
    amortization_end_month: '',
    operation_id: seed.operation_id,
    file_id: seed.file_id,
    version: seed.version || 1,
    created_at: seed.created_at,
    updated_at: seed.created_at,
    deleted_at: seed.deleted_at || null,
  };
  fillExpense(object, seed.accounting_currency);
  db.expense.push(object);
  return object;
}

const BANKS = [
  { bank_name: '招商银行', card_last_4: '8821' },
  { bank_name: '中国银行', card_last_4: '3097' },
  { bank_name: '交通银行', card_last_4: '5540' },
  { bank_name: '', card_last_4: '' },
];

//备注写成一个独立的词而不是「对手方+消费」拼出来的句子：拼出来的句子换语言时没法整值认回来
const SHOPS = [
  { counterparty: '盒马鲜生', expense_type: '餐饮', remark: '买菜' },
  { counterparty: '滴滴出行', expense_type: '交通', remark: '打车' },
  { counterparty: '国家电网', expense_type: '居住', remark: '电费' },
  { counterparty: '京东商城', expense_type: '日用', remark: '日用补货' },
  { counterparty: 'Apple Store', expense_type: '数码', remark: '配件' },
  { counterparty: '链家物业', expense_type: '居住', remark: '物业费' },
  { counterparty: '星巴克', expense_type: '餐饮', remark: '咖啡' },
  { counterparty: 'Steam', expense_type: '娱乐', remark: '游戏' },
  { counterparty: '中国移动', expense_type: '通讯', remark: '话费' },
  { counterparty: '同仁堂药房', expense_type: '医疗', remark: '买药' },
];

//固定序列而不是 Math.random：每次刷新看到同一批数据，验收时才对得上前后两次的差异
function pseudo(index, mod) {
  return (index * 7919 + 104729) % mod;
}

//种子日期一律相对今天算。写死年月的话，默认的「最近一年」窗口过一年就会把整批数据全筛掉；
//当月还得停在今天：未来日期的支出既不真实，也正好落在那个窗口之外
function seedMonth(monthsAgo) {
  const now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth() - monthsAgo, 1);
  return {
    tag: `${pad(start.getFullYear() % 100, 2)}${pad(start.getMonth() + 1, 2)}`,
    //往月统一封到 28，免得碰上 2 月越界；当月只取到今天
    dayLimit: monthsAgo === 0 ? now.getDate() : 28,
    dateOf: (day) => `${start.getFullYear()}-${pad(start.getMonth() + 1, 2)}-${pad(Math.min(day, monthsAgo === 0 ? now.getDate() : 28), 2)}`,
  };
}

function daysAgo(count) {
  const date = new Date();
  date.setDate(date.getDate() - count);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1, 2)}-${pad(date.getDate(), 2)}`;
}

// ===== 种子数据的语言 =====

//种子里这些词是我们自己造的演示内容，不是用户录进来的，所以只有它们跟着界面语言走。
//用户在页面上录入或上传的值不在这两张表里，一个字都不会被改
const SEED_WORDS = [
  ...BANKS.map((bank) => bank.bank_name).filter(Boolean),
  ...SHOPS.map((shop) => shop.counterparty),
  ...SHOPS.map((shop) => shop.expense_type),
  ...SHOPS.map((shop) => shop.remark),
  '星巴克（重复导入）', '盒马鲜生（重复导入）', 'Apple Store 退款冲正', '退款', '退款重复',
];

//批次名嵌在文件名里、文件名又嵌在入库摘要里，整值认不出来，只能按子串换
const SEED_FILE_WORDS = ['招商', '中行', '手工补录'];

//一个种子词在各语言下的全部写法。换语言时库里存着的是上一门语言的写法，得先认回来
function renderingsOf(word) {
  return LANGS.map((lang) => textOf(lang.value, word));
}

function localizeWord(value) {
  const text = value === null || value === undefined ? '' : String(value);
  if (!text) return text;
  const word = SEED_WORDS.find((item) => renderingsOf(item).includes(text));
  return word ? t(word) : text;
}

function localizeInside(value) {
  let text = value === null || value === undefined ? '' : String(value);
  for (const word of SEED_FILE_WORDS) {
    const target = t(word);
    for (const rendering of renderingsOf(word)) {
      if (rendering !== target && text.includes(rendering)) text = text.replace(rendering, target);
    }
  }
  return text;
}

//种子文件的内容就是它那一批明细导出的 CSV。列名是落库契约，任何语言下都不翻；
//但字段值跟着明细走，所以换语言之后这份内容要重新生成，不然下载与预览出来的还是上一门语言
function fillSeedFile(fileId) {
  const rows = db.expense.filter((row) => row.file_id === fileId);
  const lines = rows.map((row) => CSV_FIELDS.map(({ key }) => {
    if (key === 'expense_date') return formatDate(row[key]);
    const value = row[key];
    return value === null || value === undefined ? '' : String(value);
  }));
  const csv = `\ufeff${toCsv(CSV_FIELDS.map((field) => field.column), lines)}`;
  db.fileBlob[fileId] = csv;
  const meta = db.fileMeta.find((row) => row.id === fileId);
  if (meta) meta.file_size = new Blob([csv]).size;
}

//内存库里存的就是展示值，筛选也是按存的值匹配的，所以换语言必须把库里重写一遍——
//只在展示时翻译的话，按「餐饮」筛选会在英文界面下一条都筛不出来
export function relocalize() {
  for (const row of db.expense) {
    row.bank_name = localizeWord(row.bank_name);
    row.counterparty = localizeWord(row.counterparty);
    row.remark = localizeWord(row.remark);
    row.expense_type = localizeWord(row.expense_type);
  }
  for (const row of db.fileMeta) row.file_name = localizeInside(row.file_name);
  for (const row of db.operationLog) row.summary = localizeInside(row.summary);
  for (const fileId of seedFileIds) fillSeedFile(fileId);
}

export function seed() {
  if (db.expense.length > 0) return;
  const batches = [
    { monthsAgo: 0, word: '招商', count: 22, time: '10:12:33' },
    { monthsAgo: 1, word: '中行', count: 18, time: '21:41:07' },
    { monthsAgo: 2, word: '手工补录', count: 12, time: '09:03:52' },
  ];

  let index = 0;
  for (const batch of batches) {
    const month = seedMonth(batch.monthsAgo);
    batch.operation_id = genId();
    batch.file_id = genId();
    batch.name = `${month.tag}-${batch.word}.csv`;
    batch.created_at = `${month.dateOf(2)}T${batch.time}+08:00`;
    seedFileIds.add(batch.file_id);
    for (let line = 0; line < batch.count; line += 1) {
      const bank = BANKS[pseudo(index, BANKS.length)];
      const shop = SHOPS[pseudo(index + 3, SHOPS.length)];
      //绝大多数是人民币记账，留两笔美元记账把 F-5 的「多种记账币种」提示撑出来
      const accountingCurrency = index % 19 === 5 ? 'USD' : 'CNY';
      const expenseCurrency = index % 11 === 3 ? 'USD' : index % 13 === 7 ? 'JPY' : 'CNY';
      seedExpense({
        ...bank,
        ...shop,
        expense_date: month.dateOf(1 + pseudo(index, month.dayLimit)),
        expense_currency: expenseCurrency,
        expense_amount: new Decimal(pseudo(index + 11, 90000) + 137).div(100).toDecimalPlaces(2).toString(),
        accounting_currency: accountingCurrency,
        amortization_months: index % 17 === 4 ? 12 : index % 23 === 9 ? 6 : 1,
        remark: index % 5 === 0 ? shop.remark : '',
        operation_id: batch.operation_id,
        file_id: batch.file_id,
        created_at: batch.created_at,
        //留几笔已删除的行，验收软删除终态与「复制新增」这条找回路径
        deleted_at: index % 29 === 13 ? `${daysAgo(9)}T15:20:00+08:00` : null,
      });
      index += 1;
    }
  }

  //手工造几组疑似重复：三要素相同，既有同批次的也有跨批次的，
  //其中一组就落在今天，默认按支出日期倒序时第一页就能看见高亮
  const duplicates = [
    { daysAgo: 0, expense_amount: '68.00', expense_currency: 'CNY', counterparty: '星巴克', expense_type: '餐饮' },
    { daysAgo: 0, expense_amount: '68.00', expense_currency: 'CNY', counterparty: '星巴克（重复导入）', expense_type: '餐饮' },
    { daysAgo: 14, expense_amount: '328.00', expense_currency: 'CNY', counterparty: '盒马鲜生', expense_type: '餐饮' },
    { daysAgo: 14, expense_amount: '328.00', expense_currency: 'CNY', counterparty: '盒马鲜生（重复导入）', expense_type: '餐饮' },
    { daysAgo: 33, expense_amount: '99.90', expense_currency: 'USD', counterparty: 'Apple Store', expense_type: '数码' },
    { daysAgo: 33, expense_amount: '99.90', expense_currency: 'USD', counterparty: 'Apple Store', expense_type: '数码' },
    { daysAgo: 33, expense_amount: '99.90', expense_currency: 'USD', counterparty: 'Apple Store 退款冲正', expense_type: '数码' },
    { daysAgo: 58, expense_amount: '-120.00', expense_currency: 'CNY', counterparty: '京东商城', expense_type: '日用', remark: '退款' },
    { daysAgo: 58, expense_amount: '-120.00', expense_currency: 'CNY', counterparty: '京东商城', expense_type: '日用', remark: '退款重复' },
  ];
  for (let i = 0; i < duplicates.length; i += 1) {
    const batch = batches[i % batches.length];
    seedExpense({
      ...duplicates[i],
      expense_date: daysAgo(duplicates[i].daysAgo),
      bank_name: '招商银行',
      card_last_4: '8821',
      accounting_currency: 'CNY',
      amortization_months: 1,
      operation_id: batch.operation_id,
      file_id: batch.file_id,
      created_at: batch.created_at,
    });
  }

  //文件内容与入库摘要都等明细全部落库之后再生成：疑似重复那几笔也挂在这些批次上，
  //先写的话就会出现「文件里 22 行、库里却挂着 24 行」这种对不上的账
  for (const batch of batches) {
    db.fileMeta.push({
      id: batch.file_id,
      file_hash: `mock${String(batch.file_id).slice(-12)}`,
      file_name: batch.name,
      file_size: 0,
      operation_id: batch.operation_id,
      created_at: batch.created_at,
    });
    fillSeedFile(batch.file_id);
    db.operationLog.push({
      id: batch.operation_id,
      operation_type: '数据入库',
      object_type: '',
      object_id: 0,
      summary: `入库 ${db.expense.filter((row) => row.file_id === batch.file_id).length} 笔，来源 ${batch.name}`,
      changes: '',
      result: '成功',
      created_at: batch.created_at,
    });
  }

  db.clientToken = '';
  db.operationLog.push({
    id: genId(),
    operation_type: '系统初始化',
    object_type: '',
    object_id: 0,
    summary: '系统初始化，创建加密数据库',
    changes: '',
    result: '成功',
    created_at: `${daysAgo(90)}T08:00:00+08:00`,
  });
  //种子是按中文原文造的，这里把它转成当前语言；之后每次换语言再走一遍
  relocalize();
}
