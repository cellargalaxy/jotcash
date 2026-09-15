import * as api from './api.js';
import { FILE_META_SORTS } from './config.js';
import { dateInput, emptyRow, filterCard, filterItem, idText, loadingRow, pager, select, textInput } from './component.js';
import { clear, compact, dateToRfc3339, download, el, formatDateTime, formatFileSize, toastErr, toastOk } from './util.js';

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
    file_hash: [],
    operation_id: [],
    file_name_like: '',
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
    const result = await api.selectFileMeta(state.inquiry);
    state.rows = result.object || [];
    state.count = result.count;
  } catch (err) {
    state.rows = [];
    state.count = 0;
    toastErr(err);
  }
  renderTable(false);
}

//D-3：下载时后端会重算哈希与主键比对，不一致就阻止下载，这里只负责把失败原样提示出来
async function downloadFile(row) {
  try {
    const result = await api.downloadFile(row.id);
    const object = result.object;
    download(object.file_name, object.blob || object.data);
    toastOk(`已下载 ${object.file_name}`);
  } catch (err) {
    toastErr(err);
  }
}

function buildFilter() {
  const inquiry = state.inquiry;
  const controls = {};
  const form = el('form', { class: 'row g-2 align-items-end' }, [
    filterItem('文件名', (controls.file_name_like = textInput({ value: inquiry.file_name_like })), 3),
    filterItem('文件ID（逗号分隔）', (controls.id = textInput({ value: inquiry.id.join(',') })), 2),
    filterItem('来源审计ID（逗号分隔）', (controls.operation_id = textInput({ value: inquiry.operation_id.join(',') })), 2),
    filterItem('创建时间起', (controls.created_at_start = dateInput({})), 2),
    filterItem('创建时间止', (controls.created_at_end = dateInput({})), 2),
    filterItem('排序', (controls.sort = select(FILE_META_SORTS, inquiry.sort)), 2),
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
      file_name_like: controls.file_name_like.value.trim(),
      id: compact(controls.id.value.split(',')).map(Number),
      operation_id: compact(controls.operation_id.value.split(',')).map(Number),
      created_at_start: dateToRfc3339(controls.created_at_start.value),
      created_at_end: dateToRfc3339(controls.created_at_end.value, true),
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
    el('td', { text: row.file_name }),
    el('td', { class: 'text-end text-nowrap', text: formatFileSize(row.file_size) }),
    el('td', { class: 'font-monospace small', title: row.file_hash, text: (row.file_hash || '').slice(0, 16) }),
    el('td', { class: 'font-monospace small text-nowrap', text: idText(row.operation_id) }),
    el('td', { class: 'text-nowrap small', text: formatDateTime(row.created_at) }),
    el('td', { class: 'text-nowrap' }, [
      el('button', { class: 'btn btn-sm btn-outline-primary py-0', type: 'button', text: '下载', onclick: () => downloadFile(row) }),
      el('a', { class: 'btn btn-sm btn-outline-secondary py-0 ms-1', href: `#/operation-log?id=${row.operation_id}`, text: '来源审计' }),
      el('a', { class: 'btn btn-sm btn-outline-secondary py-0 ms-1', href: `#/expense?file_id=${row.id}`, text: '本文件明细' }),
    ]),
  ]);
}

function renderTable(loading) {
  if (!tableHost) return;
  const columns = ['文件ID', '文件名', '文件大小', '内容哈希', '来源审计ID', '创建时间', '操作'];
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
  tableHost.appendChild(el('p', { class: 'small text-secondary', text: '文件只留存不删除：同一份内容只存一份，多条元数据可以指向同一个内容哈希，所以不存在孤儿文件。' }));
}

export function render(container, query) {
  host = container;
  if (query.operation_id) state.inquiry = { ...newInquiry(), operation_id: [Number(query.operation_id)] };

  tableHost = el('div');
  clear(container).appendChild(el('div', {}, [
    el('h5', { class: 'mb-3', text: '文件' }),
    buildFilter(),
    tableHost,
  ]));
  reload();
}
