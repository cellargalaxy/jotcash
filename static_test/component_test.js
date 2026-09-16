import test from 'node:test';
import { ShimEvent, mountHost } from './helper/browser.js';
import { click, find, findAll, findByText, texts } from './helper/fixture.js';
import { equal, excludes, includes, not, ok, same } from './helper/check.js';
import {
  badge,
  checkGroup,
  comboFilterInput,
  comboInput,
  currencyOptions,
  emptyRow,
  fieldText,
  filterItem,
  idText,
  loadingRow,
  pager,
  resultBadge,
  select,
} from '../static/js/component.js';
import { EXPENSE_FIELDS, PAGE_SIZES } from '../static/js/config.js';

//辅助函数：按字段键取字段声明，展示口径全仓只有 fieldText 一处，测的就是它
function fieldOf(key) {
  return EXPENSE_FIELDS.find((field) => field.key === key);
}

test('分页条：总页数按每页条数算，首末页的按钮该禁就禁', () => {
  const changes = [];
  const bar = pager({ page: 1, page_size: 20 }, 59, (change) => changes.push(change));
  includes('条数与页码', bar.textContent, '共 59 条 · 第 1/3 页');
  ok('首页禁用', findByText(bar, 'button', '首页').disabled);
  ok('上一页禁用', findByText(bar, 'button', '上一页').disabled);
  not('下一页可点', findByText(bar, 'button', '下一页').disabled);

  click(findByText(bar, 'button', '下一页'));
  same('翻到第 2 页', changes[0], { page: 2 });
  click(findByText(bar, 'button', '末页'));
  same('末页是第 3 页', changes[1], { page: 3 });
});

test('分页条：末页与空结果的边界', () => {
  const last = pager({ page: 3, page_size: 20 }, 59, () => {});
  ok('末页的下一页禁用', findByText(last, 'button', '下一页').disabled);
  ok('末页的末页禁用', findByText(last, 'button', '末页').disabled);

  const empty = pager({ page: 1, page_size: 20 }, 0, () => {});
  includes('零条也是第 1/1 页', empty.textContent, '共 0 条 · 第 1/1 页');
  const exact = pager({ page: 1, page_size: 20 }, 40, () => {});
  includes('整除不多出一页', exact.textContent, '第 1/2 页');
});

test('分页条：换每页条数要退回第 1 页，否则会停在一张空表上', () => {
  const changes = [];
  const bar = pager({ page: 3, page_size: 20 }, 59, (change) => changes.push(change));
  const sizes = findAll(bar, 'button').filter((button) => button.textContent.includes('条/页'));
  equal('候选项数 + 下拉按钮', sizes.length, PAGE_SIZES.length + 1);
  click(findByText(find(bar, 'ul'), 'button', '50 条/页'));
  same('换条数同时回第一页', changes[0], { page: 1, page_size: 50 });
});

test('只读单元格：按字段类型走各自的展示口径', () => {
  const row = {
    expense_date: '2026-09-15T10:00:00+08:00',
    amortization_start_month: '2026-09-01T00:00:00+08:00',
    created_at: '2026-09-15T10:00:00+08:00',
    accounting_amount: '68',
    exchange_rate: '7.123456',
    id: 2609151012330001,
    amortization_months: 3,
    remark: null,
    deleted_at: null,
  };
  equal('日期', fieldText(fieldOf('expense_date'), row), '2026-09-15');
  equal('月份', fieldText(fieldOf('amortization_start_month'), row), '2026-09');
  includes('时间', fieldText(fieldOf('created_at'), row), '2026-09-15 ');
  equal('金额补到金额精度', fieldText(fieldOf('accounting_amount'), row), '68.00');
  equal('汇率保留自己的位数', fieldText(fieldOf('exchange_rate'), row), '7.123456');
  equal('ID 当字符串展示', fieldText(fieldOf('id'), row), '2609151012330001');
  equal('整数', fieldText(fieldOf('amortization_months'), row), '3');
  equal('空值不印 null', fieldText(fieldOf('remark'), row), '');
  equal('零值时间', fieldText(fieldOf('deleted_at'), row), '');
  equal('ID 为 0 时留空', idText(0), '');
});

test('币种下拉：表外的币种追加在后面，不与表内重复', () => {
  const options = currencyOptions(['CNY', 'MOP', 'XBT']);
  const codes = options.map((option) => option.value);
  equal('已知币种只出现一次', codes.filter((code) => code === 'CNY').length, 1);
  equal('表内的澳门元也只有一条', codes.filter((code) => code === 'MOP').length, 1);
  ok('表外币种被追加', codes.includes('XBT'));
  equal('表外币种排在最后', codes[codes.length - 1], 'XBT');
  ok('带中文名', options[0].name.includes('人民币'));
});

test('下拉控件：选中项按取值匹配，数字取值也认', () => {
  const node = select([{ value: 0, name: '不显示已删除' }, { value: 1, name: '全部' }], 1);
  equal('选中的是第二项', node.value, '1');
  same('选项文案', texts(node, 'option'), ['不显示已删除', '全部']);
});

//多选筛选点下拉是往后面追加，单选是替换；这两条行为一旦调反，筛选条件会被悄悄顶掉
test('组合框：多选追加、单选替换，追加时不重复', () => {
  const multi = comboFilterInput(() => ['餐饮', '数码'], '餐饮', {});
  multi.node.dispatchEvent(new ShimEvent('show.bs.dropdown'));
  click(findByText(multi.node, 'button', '数码'));
  equal('追加在后面', multi.input.value, '餐饮,数码');
  multi.node.dispatchEvent(new ShimEvent('show.bs.dropdown'));
  click(findByText(multi.node, 'button', '数码'));
  equal('已有的不再追加', multi.input.value, '餐饮,数码');

  const single = comboInput(() => ['餐饮', '数码'], '餐饮', {});
  single.node.dispatchEvent(new ShimEvent('show.bs.dropdown'));
  click(findByText(single.node, 'button', '数码'));
  equal('单选直接替换', single.input.value, '数码');
});

test('组合框：候选是取值函数不是快照，展开时才建菜单', () => {
  let options = [];
  const combo = comboInput(() => options, '', {});
  combo.node.dispatchEvent(new ShimEvent('show.bs.dropdown'));
  includes('候选为空时给提示', combo.node.textContent, '暂无候选');

  options = ['后来才有的候选'];
  combo.node.dispatchEvent(new ShimEvent('show.bs.dropdown'));
  ok('第二次展开能看到新候选', findByText(combo.node, 'button', '后来才有的候选'));
  excludes('菜单已重建', combo.node.textContent, '暂无候选');
});

test('复选框组：勾选与取消都回调当前全集', () => {
  const picked = [];
  const group = checkGroup(['成功', '失败', '部分成功'], ['成功'], (values) => picked.push(values.slice()));
  const boxes = findAll(group, 'input');
  ok('初值已勾上', boxes[0].checked);
  not('未选中的不勾', boxes[1].checked);

  boxes[1].checked = true;
  boxes[1].dispatchEvent(new ShimEvent('change', { bubbles: true }));
  same('勾上第二项', picked[0], ['成功', '失败']);
  boxes[0].checked = false;
  boxes[0].dispatchEvent(new ShimEvent('change', { bubbles: true }));
  same('取消第一项', picked[1], ['失败']);
});

test('筛选格与占位行：小字提示与跨列数都按调用方给的来', () => {
  mountHost();
  const item = filterItem('交易对手方', select([{ value: 'a', name: 'a' }], 'a'), 3, '模糊匹配，输入片段即可');
  includes('标签', item.textContent, '交易对手方');
  includes('小字提示', item.textContent, '模糊匹配，输入片段即可');
  includes('栅格宽度', item.className, 'col-lg-3');
  const plain = filterItem('文件ID', select([], ''), 2);
  equal('没给提示就不长出这一格', findAll(plain, 'div').length, 0);

  equal('空行跨列', emptyRow(12).querySelector('td').getAttribute('colspan'), '12');
  includes('空行文案', emptyRow(12).textContent, '没有匹配的数据');
  includes('自定义空行文案', emptyRow(3, '这一批没有明细').textContent, '这一批没有明细');
  includes('加载行文案', loadingRow(12).textContent, '加载中');
});

test('审计结果徽标：三种结果各有自己的颜色', () => {
  includes('成功', resultBadge('成功').className, 'text-bg-success');
  includes('失败', resultBadge('失败').className, 'text-bg-danger');
  includes('部分成功', resultBadge('部分成功').className, 'text-bg-warning');
  includes('默认色', badge('其它').className, 'text-bg-secondary');
});
