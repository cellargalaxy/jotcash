import * as api from './api.js';
import { DELETED_ALL, DELETED_NO, DELETED_ONLY, EXPENSE_SORTS } from './config.js';
import {
  comboFilterInput,
  comboInput,
  currencyOptions,
  dateInput,
  filterCard,
  filterItem,
  select,
  textInput,
} from './component.js';
import { compact, dateToRfc3339, el, formatDate } from './util.js';

//明细的筛选条件。没有 page/page_size：条件只描述「筛什么」，后端不带分页参数即返回全集，
//明细表格的分页、图表统计、导出都从这一份全量上再加工
export function newInquiry() {
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
  };
}

// ===== 候选取值 =====

export const CANDIDATE_FIELDS = ['expense_type', 'bank_name', 'card_last_4', 'expense_currency'];

//候选下拉的取值：接口的 distinct 结果与用户现场录入的新值都往这里合并
const candidates = { expense_type: [], bank_name: [], card_last_4: [], expense_currency: [] };

let currencySet = [];

//用户录入的新值立刻进候选，不必等它入库后 distinct 才认
export function addCandidate(field, value) {
  const text = String(value || '').trim();
  if (!text || !CANDIDATE_FIELDS.includes(field)) return;
  const list = candidates[field];
  if (!list.includes(text)) {
    list.push(text);
    list.sort();
  }
}

export function candidateOf(field) {
  return candidates[field] || [];
}

//全库真实存在的记账币种，与候选分开取：候选会合并用户现场录入的新值，混进来这个集合就不准了
export function accountingCurrencySet() {
  return currencySet;
}

export async function loadCandidate() {
  try {
    const [currencies, ...distincts] = await Promise.all([
      api.selectDistinct('accounting_currency'),
      ...CANDIDATE_FIELDS.map((field) => api.selectDistinct(field)),
    ]);
    currencySet = currencies.object || [];
    CANDIDATE_FIELDS.forEach((field, index) => {
      for (const value of distincts[index].object || []) addCandidate(field, value);
    });
  } catch (err) {
    //候选与币种提示是锦上添花，取不到不该拦住列表与图表
    currencySet = [];
  }
}

// ===== 筛选卡片：明细页与统计页共用同一套条件、同一套控件 =====

export function expenseFilter(inquiry, onApply, onReset) {
  const controls = {};
  //组合框返回的是 {node,input}，控件登记的得是里面那个 input，栅格里放的是外层 node
  const comboField = (key, getOptions, value, attrs) => {
    const combo = comboFilterInput(getOptions, value, attrs);
    controls[key] = combo.input;
    return combo.node;
  };
  const singleComboField = (key, getOptions, value, attrs) => {
    const combo = comboInput(getOptions, value, attrs);
    controls[key] = combo.input;
    return combo.node;
  };
  const items = [
    filterItem('支出日期起', (controls.expense_date_start = dateInput({ value: inquiry.expense_date_start ? formatDate(inquiry.expense_date_start) : '' })), 2),
    filterItem('支出日期止', (controls.expense_date_end = dateInput({ value: inquiry.expense_date_end ? formatDate(inquiry.expense_date_end) : '' })), 2),
    filterItem('支出金额下限', (controls.expense_amount_min = textInput({ value: inquiry.expense_amount_min || '' })), 2),
    filterItem('支出金额上限', (controls.expense_amount_max = textInput({ value: inquiry.expense_amount_max || '' })), 2),
    filterItem('支出币种', comboField('expense_currency', () => currencyOptions(candidateOf('expense_currency')), inquiry.expense_currency.join(','), { class: 'form-control form-control-sm text-uppercase' }), 2, '可多选，逗号分隔'),
    filterItem('记账币种', comboField('accounting_currency', () => currencyOptions(candidateOf('expense_currency')), inquiry.accounting_currency.join(','), { class: 'form-control form-control-sm text-uppercase' }), 2, '可多选，逗号分隔'),
    filterItem('交易对手方', (controls.counterparty_like = textInput({ value: inquiry.counterparty_like })), 3, '模糊匹配，输入片段即可'),
    filterItem('交易备注', (controls.remark_like = textInput({ value: inquiry.remark_like })), 3, '模糊匹配，输入片段即可'),
    filterItem('支出类型', singleComboField('expense_type_like', () => candidateOf('expense_type'), inquiry.expense_type_like, {}), 2, '模糊匹配，也可下拉选已有'),
    filterItem('银行名称', comboField('bank_name', () => candidateOf('bank_name'), inquiry.bank_name.join(','), {}), 2, '可多选，逗号分隔'),
    filterItem('卡号后四位', comboField('card_last_4', () => candidateOf('card_last_4'), inquiry.card_last_4.join(','), {}), 2, '可多选，逗号分隔'),
    filterItem('来源审计ID', (controls.operation_id = textInput({ value: inquiry.operation_id.join(',') })), 2),
    filterItem('文件ID', (controls.file_id = textInput({ value: inquiry.file_id.join(',') })), 2),
    filterItem('已删除', (controls.deleted = select([
      { value: DELETED_NO, name: '不显示已删除' },
      { value: DELETED_ALL, name: '全部' },
      { value: DELETED_ONLY, name: '只看已删除' },
    ], inquiry.deleted)), 2),
    filterItem('排序', (controls.sort = select(EXPENSE_SORTS, inquiry.sort)), 3),
  ];

  const form = el('form', { class: 'row g-2 align-items-start filter-form' }, [
    ...items,
    el('div', { class: 'col-12 d-flex gap-2 pt-2' }, [
      el('button', { class: 'btn btn-sm btn-primary', type: 'submit', text: '查询' }),
      el('button', { class: 'btn btn-sm btn-outline-secondary', type: 'button', text: '重置', onclick: onReset }),
    ]),
  ]);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    onApply({
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
    });
  });

  return filterCard(form);
}
