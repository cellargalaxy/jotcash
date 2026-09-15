import * as api from './api.js';
import {
  CSV_FIELDS,
  DELETED_ALL,
  DELETED_NO,
  DELETED_ONLY,
  DUPLICATE_KEYS,
  EXPENSE_COLUMN_DEFAULT,
  EXPENSE_FIELDS,
  EXPENSE_SORTS,
  PAGE_SIZE_MAX,
} from './config.js';
import {
  currencyInput,
  dateInput,
  emptyRow,
  filterCard,
  fieldText,
  filterItem,
  loadingRow,
  numberInput,
  pager,
  select,
  textInput,
} from './component.js';
import { getAccountingCurrency, getColumns, setColumns } from './store.js';
import {
  addMonth,
  appendChildren,
  clear,
  confirmModal,
  compact,
  dateToRfc3339,
  download,
  el,
  formatDate,
  isDecimal,
  isPositiveDecimal,
  monthOf,
  multiplyAmount,
  toCsv,
  toastErr,
  toastOk,
} from './util.js';

const FIELD_OF = Object.fromEntries(EXPENSE_FIELDS.map((field) => [field.key, field]));
const EDITABLE_FIELDS = EXPENSE_FIELDS.filter((field) => field.editable);

//跨次渲染保留筛选条件与列偏好，切页面回来不用重新填一遍
const state = {
  inquiry: newInquiry(),
  columns: getColumns(),
  rows: [],
  count: 0,
  selected: new Set(),
  editingId: 0,
  editDraft: null,
  adding: false,
  addDraft: null,
  verify: null,
  expenseTypes: [],
  currencySet: [],
};

let host = null;
let tableHost = null;
let toolbarHost = null;
//行内勾选框的引用：改选中只需刷工具条与这些框，不必重绘整张表，免得把正在编辑的输入框焦点弄丢
let rowCheckboxes = [];

function newInquiry() {
  return {
    id: [],
    bank_name: [],
    card_last_4: [],
    expense_currency: [],
    accounting_currency: [],
    expense_type: [],
    operation_id: [],
    file_id: [],
    expense_date_start: '',
    expense_date_end: '',
    expense_amount_min: null,
    expense_amount_max: null,
    counterparty_like: '',
    remark_like: '',
    expense_type_like: '',
    deleted: DELETED_NO,
    sort: 'expense_date desc',
    page: 1,
    page_size: 20,
  };
}

// ===== 疑似重复 =====

//三要素相同即同组；只标记不阻断，人工裁定后走软删除
function duplicateKey(row) {
  return DUPLICATE_KEYS.map((key) => (key === 'expense_date' ? formatDate(row[key]) : String(row[key]))).join('|');
}

function groupDuplicate(rows) {
  const counter = new Map();
  for (const row of rows) {
    const key = duplicateKey(row);
    counter.set(key, (counter.get(key) || 0) + 1);
  }
  const marks = new Map();
  let index = 0;
  for (const [key, count] of counter.entries()) {
    if (count > 1) {
      marks.set(key, index % 2 === 0 ? 'duplicate-a' : 'duplicate-b');
      index += 1;
    }
  }
  return marks;
}

// ===== 明细编辑器：新增与编辑共用同一套字段、校验与派生预览 =====

function draftOfRow(row) {
  const draft = {};
  for (const field of EDITABLE_FIELDS) {
    draft[field.key] = field.key === 'expense_date' ? formatDate(row[field.key]) : row[field.key];
  }
  draft.accounting_currency = row.accounting_currency || getAccountingCurrency();
  return draft;
}

function emptyDraft() {
  const draft = {};
  for (const field of EDITABLE_FIELDS) draft[field.key] = '';
  draft.expense_date = formatDate(new Date());
  draft.expense_currency = getAccountingCurrency();
  draft.amortization_months = 1;
  draft.accounting_currency = getAccountingCurrency();
  return draft;
}

function checkDraft(draft) {
  if (!draft.expense_date) return '支出日期必填';
  if (!/^[A-Za-z]{3}$/.test(draft.expense_currency || '')) return '支出币种必须是三位币种代码';
  if (!isDecimal(draft.expense_amount)) return `支出金额非法: ${draft.expense_amount}`;
  if (draft.exchange_rate && !isPositiveDecimal(draft.exchange_rate)) return `折算汇率非正: ${draft.exchange_rate}`;
  if (!(Number(draft.amortization_months) >= 1)) return '摊分月数不得小于 1';
  return '';
}

function expenseEditor(draft, options) {
  const controls = {};
  const preview = el('div', { class: 'small text-secondary' });

  //§8.1 联动：支出币种=记账币种时汇率恒为 1，改日期或改币种则清空汇率交给系统自动获取
  function refresh() {
    const sameCurrency = (draft.expense_currency || '').toUpperCase() === (draft.accounting_currency || '').toUpperCase();
    if (sameCurrency) {
      draft.exchange_rate = '1';
      controls.exchange_rate.value = '1';
      controls.exchange_rate.disabled = true;
    } else {
      controls.exchange_rate.disabled = false;
    }
    const rate = draft.exchange_rate;
    const startMonth = monthOf(draft.expense_date);
    const months = Number(draft.amortization_months) || 1;
    clear(preview).appendChild(el('span', {}, [
      `记账币种 ${draft.accounting_currency} · 记账金额 `,
      el('strong', { text: draft.expense_amount === '' || draft.expense_amount === null || draft.expense_amount === undefined
        ? '—'
        : rate ? multiplyAmount(draft.expense_amount, rate) : '（汇率留空，落库时自动获取）' }),
      ` · 摊分 ${startMonth || '—'} 至 ${startMonth ? addMonth(startMonth, months - 1) : '—'}`,
    ]));
  }

  function bind(field, control, transform) {
    controls[field.key] = control;
    control.value = draft[field.key] === null || draft[field.key] === undefined ? '' : String(draft[field.key]);
    control.addEventListener('input', () => {
      draft[field.key] = transform ? transform(control.value) : control.value;
      //改支出日期或支出币种都要按新值重新取汇率，手填过的值也一并让位
      if (field.key === 'expense_date' || field.key === 'expense_currency') {
        draft.exchange_rate = '';
        controls.exchange_rate.value = '';
      }
      refresh();
    });
    return control;
  }

  function cell(field, control, width) {
    return el('div', { class: `col-6 col-md-${width || 2}` }, [
      el('label', { class: 'form-label small text-secondary mb-1', text: field.name }),
      control,
    ]);
  }

  const typeList = el('datalist', { id: 'expense-type-options' });
  for (const type of state.expenseTypes) typeList.appendChild(el('option', { value: type }));

  const grid = el('div', { class: 'row g-2' }, [
    cell(FIELD_OF.expense_date, bind(FIELD_OF.expense_date, dateInput({}))),
    cell(FIELD_OF.expense_currency, bind(FIELD_OF.expense_currency, textInput({ class: 'form-control form-control-sm text-uppercase', maxlength: '3' }), (value) => value.toUpperCase())),
    cell(FIELD_OF.expense_amount, bind(FIELD_OF.expense_amount, textInput({ placeholder: '允许 0 与负数' }))),
    cell(FIELD_OF.exchange_rate, bind(FIELD_OF.exchange_rate, textInput({ placeholder: '留空自动获取' }))),
    cell(FIELD_OF.expense_type, bind(FIELD_OF.expense_type, textInput({ list: 'expense-type-options' }))),
    cell(FIELD_OF.amortization_months, bind(FIELD_OF.amortization_months, numberInput({ min: '1', step: '1' }), (value) => Number(value) || 1)),
    cell(FIELD_OF.counterparty, bind(FIELD_OF.counterparty, textInput({})), 3),
    cell(FIELD_OF.remark, bind(FIELD_OF.remark, textInput({})), 3),
    cell(FIELD_OF.bank_name, bind(FIELD_OF.bank_name, textInput({})), 3),
    cell(FIELD_OF.card_last_4, bind(FIELD_OF.card_last_4, textInput({ maxlength: '4' })), 3),
    typeList,
  ]);

  const saveButton = el('button', { class: 'btn btn-sm btn-primary', type: 'button', text: options.saveText || '保存' });
  saveButton.addEventListener('click', async () => {
    const message = checkDraft(draft);
    if (message) {
      toastErr(new Error(message));
      return;
    }
    saveButton.disabled = true;
    try {
      await options.onSave(draft);
    } catch (err) {
      toastErr(err);
    } finally {
      saveButton.disabled = false;
    }
  });

  const node = el('div', { class: 'expense-editor p-3' }, [
    el('div', { class: 'd-flex align-items-center mb-2' }, [
      el('strong', { class: 'small', text: options.title }),
      el('div', { class: 'ms-auto d-flex gap-2' }, [
        saveButton,
        el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '取消', onclick: options.onCancel }),
      ]),
    ]),
    grid,
    el('div', { class: 'mt-2' }, [preview]),
  ]);
  refresh();
  return node;
}

// ===== CSV =====

function csvHeader() {
  return CSV_FIELDS.map((key) => FIELD_OF[key].name);
}

//单行新增就是一份只有 1 行的 CSV，与上传真实 CSV 走同一条入库链路
function draftToCsv(draft) {
  const row = CSV_FIELDS.map((key) => {
    if (key === 'expense_date') return draft.expense_date;
    //填了折算汇率就必须一并填记账币种，否则后端解析这一行会直接报错；
    //汇率留空时记账币种也留空，让请求携带的口径生效
    if (key === 'accounting_currency') return draft.exchange_rate ? draft.accounting_currency : '';
    const value = draft[key];
    return value === null || value === undefined ? '' : String(value);
  });
  return toCsv(csvHeader(), [row]);
}

function csvFilename(prefix) {
  const now = new Date();
  const pad = (value) => String(value).padStart(2, '0');
  const stamp = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `${prefix}-${stamp}.csv`;
}

function downloadTemplate() {
  const sample = CSV_FIELDS.map((key) => {
    switch (key) {
      case 'expense_date': return formatDate(new Date());
      case 'expense_currency': return 'CNY';
      case 'expense_amount': return '128.50';
      case 'counterparty': return '盒马鲜生';
      case 'remark': return '晚饭';
      case 'expense_type': return '餐饮';
      case 'amortization_months': return '1';
      default: return '';
    }
  });
  download('jotcash-template.csv', `﻿${toCsv(csvHeader(), [sample])}`);
}

// ===== 数据加载 =====

async function reload() {
  renderTable(true);
  try {
    if (state.verify) {
      await loadVerify();
    } else {
      const result = await api.selectExpense(state.inquiry);
      state.rows = result.object || [];
      state.count = result.count;
    }
    await loadHeaderHint();
  } catch (err) {
    state.rows = [];
    state.count = 0;
    toastErr(err);
  }
  renderTable(false);
}

//E-3 核实视图：审计ID 定位本批次 → 取三要素集合 → 反查全库未删除明细中命中的行
async function loadVerify() {
  const batch = await api.selectExpense({
    ...newInquiry(),
    operation_id: [state.verify],
    deleted: DELETED_ALL,
    page_size: PAGE_SIZE_MAX,
  });
  const batchRows = batch.object || [];
  const keys = new Set(batchRows.map(duplicateKey));
  let merged = batchRows.slice();
  if (batchRows.length > 0) {
    const dates = batchRows.map((row) => formatDate(row.expense_date)).sort();
    const amounts = batchRows.map((row) => new Decimal(row.expense_amount));
    const currencies = [...new Set(batchRows.map((row) => row.expense_currency))];
    //现有接口没有「三要素整组匹配」的条件，先用日期/币种/金额区间把候选集缩小，再按集合精确命中
    const candidate = await api.selectExpense({
      ...newInquiry(),
      expense_date_start: dateToRfc3339(dates[0]),
      expense_date_end: dateToRfc3339(dates[dates.length - 1], true),
      expense_currency: currencies,
      expense_amount_min: Decimal.min(...amounts).toString(),
      expense_amount_max: Decimal.max(...amounts).toString(),
      deleted: DELETED_NO,
      page_size: PAGE_SIZE_MAX,
    });
    const seen = new Set(batchRows.map((row) => row.id));
    for (const row of candidate.object || []) {
      if (!seen.has(row.id) && keys.has(duplicateKey(row))) merged.push(row);
    }
  }
  merged.sort((left, right) => duplicateKey(left).localeCompare(duplicateKey(right)));
  state.rows = merged;
  state.count = merged.length;
}

async function loadHeaderHint() {
  try {
    const [types, currencies] = await Promise.all([api.selectExpenseType(), api.selectAccountingCurrency()]);
    state.expenseTypes = types.object || [];
    state.currencySet = currencies.object || [];
  } catch (err) {
    //候选与表头提示是锦上添花，取不到不该拦住列表
    state.expenseTypes = [];
    state.currencySet = [];
  }
}

// ===== 动作 =====

async function saveEdit(row, draft) {
  const object = { ...row };
  for (const field of EDITABLE_FIELDS) object[field.key] = draft[field.key];
  object.expense_date = dateToRfc3339(draft.expense_date);
  await api.updateExpense(object);
  state.editingId = 0;
  state.editDraft = null;
  toastOk(`明细 ${row.id} 已保存`);
  await reload();
}

async function saveAdd(draft) {
  const result = await api.insertExpense(csvFilename('single'), draftToCsv(draft));
  state.adding = false;
  state.addDraft = null;
  toastOk(`入库 ${result.count} 笔，进入核实视图`);
  location.hash = `#/expense?operation_id=${result.object}&verify=1`;
}

async function deleteSelected() {
  const ids = [...state.selected];
  if (ids.length === 0) return;
  if (!(await confirmModal('批量软删除', `本次将删除 ${ids.length} 笔。删除是终态，没有恢复入口，找回只能靠复制新增。`))) return;
  try {
    const result = await api.deleteExpense({ ...newInquiry(), id: ids });
    state.selected.clear();
    toastOk(`已删除 ${result.count} 笔`);
    await reload();
  } catch (err) {
    toastErr(err);
  }
}

async function deleteFiltered() {
  if (!(await confirmModal('按当前筛选全选删除', `本次将删除当前筛选结果全集，共 ${state.count} 笔。删除是终态，没有恢复入口。`, '确认删除'))) return;
  try {
    const inquiry = { ...state.inquiry, page: 1, page_size: 0, sort: '' };
    const result = await api.deleteExpense(inquiry);
    state.selected.clear();
    toastOk(`已删除 ${result.count} 笔`);
    await reload();
  } catch (err) {
    toastErr(err);
  }
}

function openUpload() {
  const input = el('input', { class: 'form-control', type: 'file', accept: '.csv,text/csv' });
  const body = el('div', {}, [
    el('p', { class: 'small text-secondary', text: '表头必须与契约完全一致，列名与顺序都不能差，否则后端不认领这个文件。' }),
    el('pre', { class: 'small bg-body-secondary p-2 rounded', text: csvHeader().join(',') }),
    el('ul', { class: 'small text-secondary ps-3' }, [
      el('li', { text: '支出日期格式 2006-01-02；支出金额允许 0 与负数' }),
      el('li', { text: '折算汇率可留空，留空按支出日期自动获取；填了折算汇率就必须填记账币种' }),
      el('li', { text: '记账币种留空则取本次请求携带的记账币种；摊分月数留空按 1 处理' }),
    ]),
    input,
  ]);
  confirmModal('上传 CSV 批量新增', body).then(async (confirmed) => {
    if (!confirmed) return;
    const file = input.files && input.files[0];
    if (!file) {
      toastErr(new Error('没有选择文件'));
      return;
    }
    try {
      const result = await api.insertExpenseFile(file);
      toastOk(`入库 ${result.count} 笔，进入核实视图`);
      location.hash = `#/expense?operation_id=${result.object}&verify=1`;
    } catch (err) {
      toastErr(err);
    }
  });
}

function openColumnSetting() {
  const chosen = new Set(state.columns);
  const list = el('div', { class: 'row g-1' });
  const boxes = [];
  for (const field of EXPENSE_FIELDS) {
    const input = el('input', { class: 'form-check-input', type: 'checkbox', checked: chosen.has(field.key) ? true : null });
    boxes.push({ key: field.key, input });
    list.appendChild(el('div', { class: 'col-6 col-md-4' }, [
      el('label', { class: 'form-check' }, [input, el('span', { class: 'form-check-label small ms-1', text: field.name })]),
    ]));
  }
  const setAll = (keys) => {
    for (const box of boxes) box.input.checked = keys.includes(box.key);
  };
  const body = el('div', {}, [
    el('div', { class: 'd-flex gap-2 mb-3' }, [
      el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '默认 10 列', onclick: () => setAll(EXPENSE_COLUMN_DEFAULT) }),
      el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '全字段平铺', onclick: () => setAll(EXPENSE_FIELDS.map((field) => field.key)) }),
    ]),
    list,
  ]);
  confirmModal('表头设置', body).then((confirmed) => {
    if (!confirmed) return;
    const columns = boxes.filter((box) => box.input.checked).map((box) => box.key);
    state.columns = columns.length > 0 ? columns : EXPENSE_COLUMN_DEFAULT.slice();
    setColumns(state.columns);
    renderTable(false);
  });
}

function openCurrencySwitch() {
  const control = currencyInput(getAccountingCurrency(), {});
  const body = el('div', {}, [
    el('p', { class: 'small text-secondary', text: '筛选记账币种不等于目标币种的全部明细（含已删除），逐笔按其支出日期重新获取汇率并覆盖，同步重算记账金额。允许部分失败，重试即续跑。' }),
    control,
  ]);
  confirmModal('记账币种切换', body).then(async (confirmed) => {
    if (!confirmed) return;
    const target = control.querySelector('input').value.trim().toUpperCase();
    try {
      const result = await api.switchAccountingCurrency(target);
      toastOk(`记账币种切换完成，成功 ${result.object.done} 笔，失败 ${result.object.failed} 笔`);
      await reload();
    } catch (err) {
      toastErr(err);
    }
  });
}

// ===== 渲染 =====

function buildFilter() {
  const inquiry = state.inquiry;
  const controls = {};
  const items = [
    filterItem('支出日期起', (controls.expense_date_start = dateInput({ value: inquiry.expense_date_start ? formatDate(inquiry.expense_date_start) : '' })), 2),
    filterItem('支出日期止', (controls.expense_date_end = dateInput({ value: inquiry.expense_date_end ? formatDate(inquiry.expense_date_end) : '' })), 2),
    filterItem('支出金额下限', (controls.expense_amount_min = textInput({ value: inquiry.expense_amount_min || '' })), 2),
    filterItem('支出金额上限', (controls.expense_amount_max = textInput({ value: inquiry.expense_amount_max || '' })), 2),
    filterItem('支出币种（逗号分隔）', (controls.expense_currency = textInput({ class: 'form-control form-control-sm text-uppercase', value: inquiry.expense_currency.join(',') })), 2),
    filterItem('记账币种（逗号分隔）', (controls.accounting_currency = textInput({ class: 'form-control form-control-sm text-uppercase', value: inquiry.accounting_currency.join(',') })), 2),
    filterItem('交易对手方', (controls.counterparty_like = textInput({ value: inquiry.counterparty_like })), 3),
    filterItem('交易备注', (controls.remark_like = textInput({ value: inquiry.remark_like })), 3),
    filterItem('支出类型', (controls.expense_type_like = textInput({ value: inquiry.expense_type_like, list: 'filter-type-options' })), 2),
    filterItem('银行名称（逗号分隔）', (controls.bank_name = textInput({ value: inquiry.bank_name.join(',') })), 2),
    filterItem('卡号后四位（逗号分隔）', (controls.card_last_4 = textInput({ value: inquiry.card_last_4.join(',') })), 2),
    filterItem('来源审计ID', (controls.operation_id = textInput({ value: inquiry.operation_id.join(',') })), 2),
    filterItem('文件ID', (controls.file_id = textInput({ value: inquiry.file_id.join(',') })), 2),
    filterItem('已删除', (controls.deleted = select([
      { value: DELETED_NO, name: '不显示已删除' },
      { value: DELETED_ALL, name: '全部' },
      { value: DELETED_ONLY, name: '只看已删除' },
    ], inquiry.deleted)), 2),
    filterItem('排序', (controls.sort = select(EXPENSE_SORTS, inquiry.sort)), 3),
  ];

  const typeList = el('datalist', { id: 'filter-type-options' });
  for (const type of state.expenseTypes) typeList.appendChild(el('option', { value: type }));

  function apply() {
    state.inquiry = {
      ...newInquiry(),
      expense_date_start: dateToRfc3339(controls.expense_date_start.value),
      expense_date_end: dateToRfc3339(controls.expense_date_end.value, true),
      expense_amount_min: controls.expense_amount_min.value.trim() || null,
      expense_amount_max: controls.expense_amount_max.value.trim() || null,
      expense_currency: compact(controls.expense_currency.value.toUpperCase().split(',')),
      accounting_currency: compact(controls.accounting_currency.value.toUpperCase().split(',')),
      counterparty_like: controls.counterparty_like.value.trim(),
      remark_like: controls.remark_like.value.trim(),
      expense_type_like: controls.expense_type_like.value.trim(),
      bank_name: compact(controls.bank_name.value.split(',')),
      card_last_4: compact(controls.card_last_4.value.split(',')),
      operation_id: compact(controls.operation_id.value.split(',')).map(Number),
      file_id: compact(controls.file_id.value.split(',')).map(Number),
      deleted: Number(controls.deleted.value),
      sort: controls.sort.value,
      page: 1,
      page_size: state.inquiry.page_size,
    };
    state.selected.clear();
    reload();
  }

  const form = el('form', { class: 'row g-2 align-items-end' }, [
    ...items,
    typeList,
    el('div', { class: 'col-12 d-flex gap-2 pt-2' }, [
      el('button', { class: 'btn btn-sm btn-primary', type: 'submit', text: '查询' }),
      el('button', {
        class: 'btn btn-sm btn-outline-secondary',
        type: 'button',
        text: '重置',
        onclick: () => {
          state.inquiry = newInquiry();
          state.selected.clear();
          render(host, {});
        },
      }),
    ]),
  ]);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    apply();
  });

  return filterCard(form);
}

function buildToolbar() {
  const selectedCount = state.selected.size;
  return el('div', { class: 'd-flex flex-wrap align-items-center gap-2 mb-3' }, [
    el('button', {
      class: 'btn btn-sm btn-primary',
      type: 'button',
      text: '新增一行',
      onclick: () => {
        state.adding = true;
        state.addDraft = emptyDraft();
        renderTable(false);
      },
    }),
    el('button', { class: 'btn btn-sm btn-outline-primary', type: 'button', text: '上传 CSV', onclick: openUpload }),
    el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '下载 CSV 模板', onclick: downloadTemplate }),
    el('button', {
      class: 'btn btn-sm btn-outline-danger',
      type: 'button',
      disabled: selectedCount === 0 ? true : null,
      text: `批量删除（${selectedCount}）`,
      onclick: deleteSelected,
    }),
    el('button', {
      class: 'btn btn-sm btn-outline-danger',
      type: 'button',
      disabled: state.count === 0 || state.verify ? true : null,
      text: '全选筛选结果并删除',
      onclick: deleteFiltered,
    }),
    el('div', { class: 'ms-auto d-flex gap-2' }, [
      el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '表头设置', onclick: openColumnSetting }),
      el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '刷新', onclick: () => reload() }),
    ]),
  ]);
}

//F-5：表头列出全库存在的记账币种，多于一种或与当前选择不一致就提示去切换
function buildCurrencyHint() {
  const current = getAccountingCurrency();
  const codes = state.currencySet.map((item) => item.code);
  if (codes.length === 0) return null;
  const mixed = codes.length > 1 || codes[0] !== current;
  if (!mixed) return null;
  return el('div', { class: 'alert alert-warning py-2 d-flex flex-wrap align-items-center gap-2' }, [
    el('span', { class: 'small', text: `全库记账币种：${state.currencySet.map((item) => `${item.code}（${item.count} 笔）`).join('、')}；当前选择：${current}。混合口径会让金额统计失真。` }),
    el('button', { class: 'btn btn-sm btn-warning ms-auto', type: 'button', text: '执行记账币种切换', onclick: openCurrencySwitch }),
  ]);
}

function buildVerifyHint() {
  if (!state.verify) return null;
  return el('div', { class: 'alert alert-info py-2 d-flex flex-wrap align-items-center gap-2' }, [
    el('span', { class: 'small', text: `核实视图：审计ID ${state.verify} 这一批次，以及全库中与本批次「支出日期+支出金额+支出币种」命中的未删除明细，共 ${state.count} 行。` }),
    el('a', { class: 'btn btn-sm btn-outline-secondary ms-auto', href: '#/expense', text: '返回全部明细' }),
  ]);
}

function headerCell(field) {
  if (field.key !== 'accounting_currency' || state.currencySet.length === 0) {
    return el('th', { class: 'text-nowrap', text: field.name });
  }
  return el('th', { class: 'text-nowrap' }, [
    field.name,
    el('span', { class: 'text-secondary fw-normal small ms-1', text: `（${state.currencySet.map((item) => item.code).join('/')}）` }),
  ]);
}

function rowCells(row) {
  const cells = [];
  for (const key of state.columns) {
    const field = FIELD_OF[key];
    if (!field) continue;
    const text = fieldText(field, row);
    const numeric = field.type === 'amount' || field.type === 'rate' || field.type === 'int';
    cells.push(el('td', { class: `${numeric ? 'text-end' : ''} ${field.type === 'id' ? 'text-nowrap font-monospace small' : ''}`, text }));
  }
  return cells;
}

function buildRow(row, marks) {
  const deleted = Boolean(row.deleted_at);
  const mark = marks.get(duplicateKey(row));
  const checkbox = el('input', {
    class: 'form-check-input',
    type: 'checkbox',
    checked: state.selected.has(row.id) ? true : null,
    disabled: deleted ? true : null,
    onchange: (event) => {
      if (event.target.checked) state.selected.add(row.id);
      else state.selected.delete(row.id);
      renderToolbar();
    },
  });
  if (!deleted) rowCheckboxes.push({ id: row.id, node: checkbox });
  const actions = el('td', { class: 'text-nowrap' }, [
    deleted
      ? el('button', {
        class: 'btn btn-sm btn-outline-primary py-0',
        type: 'button',
        text: '复制新增',
        onclick: () => {
          //复制已删除行：以其字段为初值，明细ID 与删除时间都清空，按新增处理
          state.adding = true;
          state.addDraft = draftOfRow(row);
          state.addDraft.accounting_currency = getAccountingCurrency();
          renderTable(false);
        },
      })
      : el('button', {
        class: 'btn btn-sm btn-outline-secondary py-0',
        type: 'button',
        text: state.editingId === row.id ? '收起' : '编辑',
        onclick: () => {
          state.editingId = state.editingId === row.id ? 0 : row.id;
          state.editDraft = null;
          renderTable(false);
        },
      }),
    el('a', { class: 'btn btn-sm btn-outline-secondary py-0 ms-1', href: `#/operation-log?id=${row.operation_id}`, text: '来源' }),
  ]);

  const tr = el('tr', { class: `${deleted ? 'row-deleted' : ''} ${mark || ''}` }, [
    el('td', {}, [checkbox]),
    ...rowCells(row),
    actions,
  ]);
  return tr;
}

function buildTable() {
  const marks = groupDuplicate(state.rows);
  rowCheckboxes = [];
  const columnCount = state.columns.length + 2;
  const head = el('thead', {}, [
    el('tr', {}, [
      el('th', { style: 'width:2.5rem' }, [
        el('input', {
          class: 'form-check-input',
          type: 'checkbox',
          onchange: (event) => {
            for (const checkbox of rowCheckboxes) {
              checkbox.node.checked = event.target.checked;
              if (event.target.checked) state.selected.add(checkbox.id);
              else state.selected.delete(checkbox.id);
            }
            renderToolbar();
          },
        }),
      ]),
      ...state.columns.map((key) => FIELD_OF[key]).filter(Boolean).map(headerCell),
      el('th', { class: 'text-nowrap', text: '操作' }),
    ]),
  ]);

  const body = el('tbody');
  if (state.adding && state.addDraft) {
    body.appendChild(el('tr', {}, [el('td', { colspan: columnCount, class: 'p-0' }, [
      expenseEditor(state.addDraft, {
        title: '新增一行（等价于一份只有 1 行的 CSV）',
        saveText: '入库',
        onSave: saveAdd,
        onCancel: () => {
          state.adding = false;
          state.addDraft = null;
          renderTable(false);
        },
      }),
    ])]));
  }
  if (state.rows.length === 0 && !state.adding) {
    body.appendChild(emptyRow(columnCount));
  }
  for (const row of state.rows) {
    body.appendChild(buildRow(row, marks));
    if (state.editingId === row.id) {
      //草稿存在 state 里，勾选、改列这类重绘不会把填到一半的内容冲掉
      if (!state.editDraft) state.editDraft = draftOfRow(row);
      body.appendChild(el('tr', {}, [el('td', { colspan: columnCount, class: 'p-0' }, [
        expenseEditor(state.editDraft, {
          title: `编辑明细 ${row.id}（版本号 ${row.version}，保存走乐观锁）`,
          onSave: (draft) => saveEdit(row, draft),
          onCancel: () => {
            state.editingId = 0;
            state.editDraft = null;
            renderTable(false);
          },
        }),
      ])]));
    }
  }

  return el('div', { class: 'table-responsive' }, [
    el('table', { class: 'table table-sm table-hover align-middle expense-table' }, [head, body]),
  ]);
}

function renderToolbar() {
  if (!toolbarHost) return;
  clear(toolbarHost).appendChild(buildToolbar());
}

function renderTable(loading) {
  if (!tableHost) return;
  const columnCount = state.columns.length + 2;
  clear(tableHost);
  toolbarHost = el('div');
  //核实提示与币种提示都可能为空，交给 appendChildren 统一跳过 null
  appendChildren(tableHost, [buildVerifyHint(), buildCurrencyHint(), toolbarHost]);
  renderToolbar();
  if (loading) {
    tableHost.appendChild(el('div', { class: 'table-responsive' }, [
      el('table', { class: 'table table-sm' }, [el('tbody', {}, [loadingRow(columnCount)])]),
    ]));
    return;
  }
  tableHost.appendChild(buildTable());
  if (!state.verify) {
    tableHost.appendChild(pager(state.inquiry, state.count, (change) => {
      Object.assign(state.inquiry, change);
      state.selected.clear();
      reload();
    }));
  }
  tableHost.appendChild(el('p', { class: 'small text-secondary' }, [
    '带框高亮的行是「支出日期 + 支出金额 + 支出币种」相同的疑似重复组，系统只标记不阻断，由你人工裁定后批量软删除。',
  ]));
}

export function render(container, query) {
  host = container;
  //从审计页跳过来的批次核实与按来源筛选，都以 URL 参数为准，覆盖上次的筛选条件
  if (query.verify === '1' && query.operation_id) {
    state.verify = Number(query.operation_id);
  } else {
    state.verify = null;
    if (query.operation_id) {
      state.inquiry = { ...newInquiry(), operation_id: [Number(query.operation_id)] };
    } else if (query.file_id) {
      state.inquiry = { ...newInquiry(), file_id: [Number(query.file_id)] };
    }
  }
  state.selected.clear();
  state.editingId = 0;
  state.editDraft = null;
  state.adding = false;
  state.addDraft = null;

  tableHost = el('div');
  clear(container).appendChild(el('div', {}, [
    el('h5', { class: 'mb-3', text: '支出明细' }),
    state.verify ? null : buildFilter(),
    tableHost,
  ]));
  reload();
}
