import * as api from './api.js';
import { OPERATION_LOG_SORTS, OPERATION_RESULTS, OPERATION_TYPES } from './config.js';
import {
  checkGroup,
  dateInput,
  emptyRow,
  filterCard,
  filterItem,
  idText,
  loadingRow,
  pager,
  resultBadge,
  select,
  textInput,
} from './component.js';
import { clear, compact, confirmModal, dateToRfc3339, el, formatDateTime, toastErr } from './util.js';

const state = {
  inquiry: newInquiry(),
  rows: [],
  count: 0,
};

let host = null;
let tableHost = null;

function newInquiry() {
  return {
    id: [],
    operation_type: [],
    object_type: [],
    object_id: [],
    result: [],
    summary_like: '',
    created_at_start: '',
    created_at_end: '',
    sort: 'created_at desc',
    page: 1,
    page_size: 20,
  };
}

async function reload() {
  renderTable(true);
  try {
    const result = await api.selectOperationLog(state.inquiry);
    state.rows = result.object || [];
    state.count = result.count;
  } catch (err) {
    state.rows = [];
    state.count = 0;
    toastErr(err);
  }
  renderTable(false);
}

//变更内容是前后值快照，通常是一段 JSON，列表里只给入口，展开看全文
function openChanges(row) {
  let text = row.changes || '';
  try {
    text = JSON.stringify(JSON.parse(row.changes), null, 2);
  } catch (err) {
    //不是 JSON 就原样展示
  }
  confirmModal(`审计 ${row.id} 的变更内容`, el('pre', { class: 'small bg-body-secondary p-2 rounded mb-0', text }));
}

function buildFilter() {
  const inquiry = state.inquiry;
  const controls = {};
  let operationTypes = inquiry.operation_type.slice();
  let results = inquiry.result.slice();

  const form = el('form', { class: 'row g-2 align-items-start filter-form' }, [
    filterItem('审计ID（逗号分隔）', (controls.id = textInput({ value: inquiry.id.join(',') })), 3),
    filterItem('操作摘要', (controls.summary_like = textInput({ value: inquiry.summary_like })), 3, '模糊匹配，输入片段即可'),
    filterItem('操作时间起', (controls.created_at_start = dateInput({})), 2),
    filterItem('操作时间止', (controls.created_at_end = dateInput({})), 2),
    filterItem('排序', (controls.sort = select(OPERATION_LOG_SORTS, inquiry.sort)), 2),
    filterItem('操作类型', checkGroup(OPERATION_TYPES, operationTypes, (values) => { operationTypes = values; }), 8),
    filterItem('操作结果', checkGroup(OPERATION_RESULTS, results, (values) => { results = values; }), 4),
    el('div', { class: 'col-12 d-flex gap-2 pt-2' }, [
      el('button', { class: 'btn btn-sm btn-primary', type: 'submit', text: '查询' }),
      el('button', {
        class: 'btn btn-sm btn-outline-secondary',
        type: 'button',
        text: '重置',
        onclick: () => {
          state.inquiry = newInquiry();
          render(host, {});
        },
      }),
    ]),
  ]);

  form.addEventListener('submit', (event) => {
    event.preventDefault();
    state.inquiry = {
      ...newInquiry(),
      id: compact(controls.id.value.split(',')).map(Number),
      summary_like: controls.summary_like.value.trim(),
      created_at_start: dateToRfc3339(controls.created_at_start.value),
      created_at_end: dateToRfc3339(controls.created_at_end.value, true),
      operation_type: operationTypes,
      result: results,
      sort: controls.sort.value,
      page: 1,
      page_size: state.inquiry.page_size,
    };
    reload();
  });

  return filterCard(form);
}

function buildRow(row) {
  return el('tr', {}, [
    el('td', { class: 'font-monospace small text-nowrap', text: idText(row.id) }),
    el('td', { class: 'text-nowrap', text: row.operation_type }),
    el('td', { class: 'text-nowrap', text: row.object_type || '—' }),
    el('td', { class: 'font-monospace small text-nowrap', text: row.object_id ? idText(row.object_id) : '—' }),
    el('td', { text: row.summary }),
    el('td', {}, [
      row.changes
        ? el('button', { class: 'btn btn-sm btn-link p-0', type: 'button', text: '查看', onclick: () => openChanges(row) })
        : el('span', { class: 'text-secondary', text: '—' }),
    ]),
    el('td', {}, [resultBadge(row.result)]),
    el('td', { class: 'text-nowrap small', text: formatDateTime(row.created_at) }),
    el('td', { class: 'text-nowrap' }, [
      el('a', { class: 'btn btn-sm btn-outline-secondary py-0', href: `#/expense?operation_id=${row.id}`, text: '本批明细' }),
      el('a', { class: 'btn btn-sm btn-outline-secondary py-0 ms-1', href: `#/file-meta?operation_id=${row.id}`, text: '本批文件' }),
    ]),
  ]);
}

function renderTable(loading) {
  if (!tableHost) return;
  const columns = ['审计ID', '操作类型', '操作对象类型', '对象ID', '操作摘要', '变更内容', '操作结果', '操作时间', '关联'];
  const head = el('thead', {}, [el('tr', {}, columns.map((name) => el('th', { class: 'text-nowrap', text: name })))]);
  const body = el('tbody');
  if (loading) body.appendChild(loadingRow(columns.length));
  else if (state.rows.length === 0) body.appendChild(emptyRow(columns.length));
  else for (const row of state.rows) body.appendChild(buildRow(row));

  clear(tableHost);
  tableHost.appendChild(el('div', { class: 'table-responsive' }, [
    el('table', { class: 'table table-sm table-hover align-middle' }, [head, body]),
  ]));
  if (!loading) {
    tableHost.appendChild(pager(state.inquiry, state.count, (change) => {
      Object.assign(state.inquiry, change);
      reload();
    }));
  }
  tableHost.appendChild(el('p', { class: 'small text-secondary', text: '审计只留痕、不承载业务：这里没有回滚、撤销与重放入口，「本批明细」只是一次按审计ID 的查询。' }));
}

export function render(container, query) {
  host = container;
  if (query.id) state.inquiry = { ...newInquiry(), id: [Number(query.id)] };
  if (query.operation_type) state.inquiry = { ...newInquiry(), operation_type: [query.operation_type] };

  tableHost = el('div');
  clear(container).appendChild(el('div', {}, [
    el('h5', { class: 'mb-3', text: '操作审计' }),
    buildFilter(),
    tableHost,
  ]));
  reload();
}
