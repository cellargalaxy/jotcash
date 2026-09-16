import { AMOUNT_SCALE, CURRENCIES } from './config.js';

// ===== DOM =====

//attrs 里 class/style/dataset 走属性，on 开头的走事件，其余直接赋到属性上
export function el(tag, attrs, children) {
  const node = document.createElement(tag);
  for (const key in attrs || {}) {
    const value = attrs[key];
    if (value === null || value === undefined || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'html') node.innerHTML = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else node.setAttribute(key, value === true ? '' : value);
  }
  appendChildren(node, children);
  return node;
}

export function appendChildren(node, children) {
  if (children === null || children === undefined) return node;
  const list = Array.isArray(children) ? children : [children];
  for (const child of list) {
    if (child === null || child === undefined || child === false) continue;
    node.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
  return node;
}

export function query(selector, root) {
  return (root || document).querySelector(selector);
}

// ===== 时间 =====

//后端的时间字段是 Go 的 time.Time，序列化出来是 RFC3339；零值是 0001-01-01
function parseTime(value) {
  if (!value) return null;
  const date = new Date(value);
  if (isNaN(date.getTime()) || date.getFullYear() <= 1) return null;
  return date;
}

function pad(value, len) {
  return String(value).padStart(len || 2, '0');
}

export function formatDate(value) {
  const date = parseTime(value);
  if (!date) return '';
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function formatMonth(value) {
  const date = parseTime(value);
  if (!date) return '';
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}`;
}

export function formatDateTime(value) {
  const date = parseTime(value);
  if (!date) return '';
  return `${formatDate(value)} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

//Go 的 time.Time 只认 RFC3339，日期框给的是 2026-09-15，得补上时刻与本地时区偏移
export function dateToRfc3339(date, endOfDay) {
  if (!date) return '';
  const offset = new Date(`${date}T00:00:00`).getTimezoneOffset();
  const sign = offset > 0 ? '-' : '+';
  const abs = Math.abs(offset);
  const zone = `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
  return `${date}T${endOfDay ? '23:59:59' : '00:00:00'}${zone}`;
}

export function today() {
  return formatDate(new Date());
}

export function monthOf(date) {
  return date ? date.slice(0, 7) : '';
}

//摊销结束月 = 起始月 + 摊销月数 - 1
export function addMonth(month, count) {
  if (!month) return '';
  const year = Number(month.slice(0, 4));
  const index = Number(month.slice(5, 7)) - 1 + count;
  const date = new Date(year, index, 1);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}`;
}

// ===== 金额与币种 =====

export function currencyDigits(code) {
  const currency = CURRENCIES.find((item) => item.code === code);
  return currency ? currency.digits : AMOUNT_SCALE;
}

export function currencyName(code) {
  const currency = CURRENCIES.find((item) => item.code === code);
  return currency ? `${code} ${currency.name}` : code;
}

//金额字段在后端是 decimal，序列化出来是字符串。按金额精度补齐小数位，
//只补不截：68 与 68.00 混排看不齐，而截位会把 JPY 这种真实小数位吃掉。
//但「只补不截」碰上二进制浮点会放大噪声：0.01+0.14+0.15 在 number 里是 0.30000000000000004，
//小数位数一数就是 17 位，照着补就把这串尾巴原样印到屏幕上。所以 number 先按 15 位有效数字
//收一次再量小数位——number 本来也只有约 15~17 位有效数字，噪声全在这条线之外。
//字符串不动：那是后端给的精确十进制，它有几位小数就是几位
export function formatAmount(value) {
  if (value === null || value === undefined || value === '') return '';
  try {
    const amount = typeof value === 'number' ? new Decimal(value).toSignificantDigits(15) : new Decimal(value);
    return amount.toFixed(Math.max(AMOUNT_SCALE, amount.decimalPlaces()));
  } catch (err) {
    return String(value);
  }
}

//记账金额 = 支出金额 × 折算汇率，按金额精度取整，与后端 fillExpense 的算法一致
export function multiplyAmount(amount, rate) {
  try {
    return new Decimal(amount || 0).mul(new Decimal(rate || 0)).toDecimalPlaces(AMOUNT_SCALE).toString();
  } catch (err) {
    return '';
  }
}

export function isPositiveDecimal(value) {
  try {
    return new Decimal(value).gt(0);
  } catch (err) {
    return false;
  }
}

export function isDecimal(value) {
  try {
    new Decimal(value);
    return true;
  } catch (err) {
    return false;
  }
}

export function formatFileSize(size) {
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = Number(size) || 0;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${index === 0 ? value : value.toFixed(2)} ${units[index]}`;
}

// ===== CSV =====

//RFC4180：含逗号、引号、换行的字段要整体加引号，内部引号翻倍
function csvCell(value) {
  const text = value === null || value === undefined ? '' : String(value);
  if (/[",\r\n]/.test(text)) return `"${text.replace(/"/g, '""')}"`;
  return text;
}

export function toCsv(header, rows) {
  const lines = [header.map(csvCell).join(',')];
  for (const row of rows) lines.push(row.map(csvCell).join(','));
  return lines.join('\r\n');
}

//手写状态机而不是按行 split：字段里的换行与逗号都在引号内，split 会把行拆断
export function parseCsv(text) {
  const rows = [];
  let row = [];
  let cell = '';
  let quoted = false;
  let index = 0;
  const content = text.replace(/^﻿/, '');
  while (index < content.length) {
    const char = content[index];
    if (quoted) {
      if (char === '"') {
        if (content[index + 1] === '"') {
          cell += '"';
          index += 2;
          continue;
        }
        quoted = false;
        index += 1;
        continue;
      }
      cell += char;
      index += 1;
      continue;
    }
    if (char === '"') {
      quoted = true;
      index += 1;
      continue;
    }
    if (char === ',') {
      row.push(cell);
      cell = '';
      index += 1;
      continue;
    }
    if (char === '\r' || char === '\n') {
      if (char === '\r' && content[index + 1] === '\n') index += 1;
      row.push(cell);
      rows.push(row);
      row = [];
      cell = '';
      index += 1;
      continue;
    }
    cell += char;
    index += 1;
  }
  if (cell !== '' || row.length > 0) {
    row.push(cell);
    rows.push(row);
  }
  //末尾空行不算数据行
  return rows.filter((line) => line.length > 1 || (line.length === 1 && line[0] !== ''));
}

// ===== 下载 =====

export function blobUrl(data, type) {
  const blob = data instanceof Blob ? data : new Blob([data], { type: type || 'text/csv;charset=utf-8' });
  return URL.createObjectURL(blob);
}

//下载按钮的首选形态：让用户点的就是一个真正的 <a download>。
//程序里 click() 出来的下载会被时序和浏览器策略卡住，用户亲手点的不会
export function downloadLink(filename, url, text, attrs) {
  return el('a', { href: url, download: filename, rel: 'noopener', text, ...attrs });
}

export function download(filename, data, type) {
  const url = blobUrl(data, type);
  const link = downloadLink(filename, url, '', { style: 'display:none' });
  document.body.appendChild(link);
  link.click();
  //点完不能立刻收摊。click() 只是把下载排进队列，浏览器真正去读这个 blob 是异步的：
  //锚点被摘掉、或者 blob 地址被撤销得太早，这一下就静默失败，表现正是「点了没反应」。
  //上一版只延后了撤销、没延后摘锚点，所以还是不下载
  setTimeout(() => {
    link.remove();
    URL.revokeObjectURL(url);
  }, 120000);
  return url;
}

// ===== 提示 =====

//message 可以是字符串，也可以是节点——下载完要在提示里挂一个手动兜底链接
export function toast(message, level, delay) {
  const container = query('#toast-container');
  const body = el('div', { class: 'toast-body' });
  appendChildren(body, [message instanceof Node ? message : document.createTextNode(String(message))]);
  const node = el('div', { class: `toast align-items-center text-bg-${level || 'primary'} border-0`, role: 'alert' }, [
    el('div', { class: 'd-flex' }, [
      body,
      el('button', { type: 'button', class: 'btn-close btn-close-white me-2 m-auto', 'data-bs-dismiss': 'toast' }),
    ]),
  ]);
  container.appendChild(node);
  const toastApi = new bootstrap.Toast(node, { delay: delay || (level === 'danger' ? 8000 : 3000) });
  node.addEventListener('hidden.bs.toast', () => node.remove());
  toastApi.show();
}

export function toastOk(message) {
  toast(message, 'success');
}

export function toastErr(err) {
  toast(err && err.message ? err.message : String(err), 'danger');
}

//二次确认：需要输入指定文案才放行的场景传 keyword
export function confirmModal(title, body, keyword) {
  return new Promise((resolve) => {
    const input = keyword
      ? el('input', { class: 'form-control mt-3', placeholder: `请输入「${keyword}」以确认` })
      : null;
    const okButton = el('button', { class: 'btn btn-danger', type: 'button', disabled: keyword ? true : null, text: '确认' });
    if (input) {
      input.addEventListener('input', () => {
        okButton.disabled = input.value.trim() !== keyword;
      });
    }
    const node = el('div', { class: 'modal fade', tabindex: '-1' }, [
      el('div', { class: 'modal-dialog modal-dialog-centered' }, [
        el('div', { class: 'modal-content' }, [
          el('div', { class: 'modal-header' }, [el('h5', { class: 'modal-title', text: title })]),
          el('div', { class: 'modal-body' }, [
            body instanceof Node ? body : el('div', { class: 'text-body', text: body }),
            input,
          ]),
          el('div', { class: 'modal-footer' }, [
            el('button', { class: 'btn btn-secondary', type: 'button', 'data-bs-dismiss': 'modal', text: '取消' }),
            okButton,
          ]),
        ]),
      ]),
    ]);
    document.body.appendChild(node);
    const modal = new bootstrap.Modal(node);
    let confirmed = false;
    okButton.addEventListener('click', () => {
      confirmed = true;
      modal.hide();
    });
    node.addEventListener('hidden.bs.modal', () => {
      node.remove();
      resolve(confirmed);
    });
    modal.show();
  });
}

//数组去空：筛选框留空的条件不能进请求体，否则 in () 会把结果筛没
export function compact(list) {
  return (list || []).map((item) => String(item).trim()).filter((item) => item !== '');
}

export function debounce(fn, wait) {
  let timer = 0;
  return function debounced(...args) {
    clearTimeout(timer);
    timer = setTimeout(() => fn.apply(this, args), wait);
  };
}
