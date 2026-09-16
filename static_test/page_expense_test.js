import test from 'node:test';
import './helper/browser.js';
import {
  answerModal,
  check,
  click,
  dblclick,
  find,
  findAll,
  findByText,
  flush,
  lastBlobBytes,
  lastBlobText,
  location,
  mockSession,
  renderPage,
  setValue,
  takeToast,
} from './helper/fixture.js';
import { equal, excludes, includes, not, ok, rejects, same } from './helper/check.js';
import * as api from '../static/js/api.js';
import { CSV_FIELDS, EXPENSE_COLUMN_DEFAULT } from '../static/js/config.js';
import { defaultInquiry } from '../static/js/expense_inquiry.js';
import * as mock from '../static/js/mock.js';
import { render as renderExpense } from '../static/js/page_expense.js';
import { formatDate, parseCsv } from '../static/js/util.js';

mockSession();

const COLUMNS = CSV_FIELDS.map((field) => field.column);

//辅助函数：只数真正的明细行，编辑器行与占位行都是整行跨列的，按 colspan 排掉
function dataRows(host) {
  return findAll(host, 'tbody tr').filter((row) => row.childNodes[0] && row.childNodes[0].getAttribute('colspan') === null);
}

//辅助函数：当前筛选下后端一共有多少条，页面上的分页与导出都该以它为准。
//页面初值就是 defaultInquiry，用例也得按同一份条件问，否则两边数的不是同一批数据
async function totalCount() {
  return (await api.selectExpense(defaultInquiry())).count;
}

//辅助函数：筛选条件是跨次渲染保留的（切页面回来不用重填），要全集就先按一次重置
async function renderAll() {
  const host = await renderPage(renderExpense, {});
  click(findByText(host, 'button', '重置'));
  await flush();
  return host;
}

//辅助函数：按标签文案取筛选格里的输入框
function filterItemOf(host, label) {
  return findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === label);
}

function filterInput(host, label) {
  return find(filterItemOf(host, label), 'input');
}

//辅助函数：按标签文案取编辑器里的输入框。编辑器是「一格一标签一控件」，认这一格的头一个 label
function editorInput(editor, label) {
  const cell = findAll(editor, 'div').find((node) => {
    const first = node.childNodes[0];
    return first && first.tagName === 'LABEL' && first.textContent === label;
  });
  return find(cell, 'input');
}

test('明细页：一次拉全量，表格只渲染当前页的切片', async () => {
  const host = await renderAll();
  const total = await totalCount();
  ok('种子数据够翻页', total > 20);
  equal('首屏只渲染一页', dataRows(host).length, 20);
  includes('分页条按全量算', host.textContent, `共 ${total} 条 · 第 1/${Math.ceil(total / 20)} 页`);
  includes('导出按钮带的是全量条数', findByText(host, 'button', '导出查询结果').textContent, `（${total}）`);
});

//翻页只对已经拿到手的全量切片，不再回接口——所以点完当场就该换好，中间不会闪「加载中」
test('明细页：翻页是同步切片，不重新请求', async () => {
  const host = await renderAll();
  const firstPage = dataRows(host).map((row) => row.textContent);
  click(findByText(host, 'button', '下一页'));
  const secondPage = dataRows(host).map((row) => row.textContent);
  not('数据当场就换了', firstPage[0] === secondPage[0]);
  excludes('全程没有加载态', host.textContent, '加载中');
  includes('页码跟着走', host.textContent, '第 2/');

  click(findByText(host, 'button', '末页'));
  const total = await totalCount();
  equal('末页只剩余数那几行', dataRows(host).length, total % 20 || 20);
  click(findByText(host, 'button', '首页'));
  same('回到首页还是原来那一屏', dataRows(host).map((row) => row.textContent), firstPage);
});

test('明细页：每页条数改成 100 之后一屏装得下全量', async () => {
  const host = await renderAll();
  const sizeMenu = findAll(host, 'ul').find((node) => node.textContent.includes('条/页'));
  click(findByText(sizeMenu, 'button', '100 条/页'));
  const total = await totalCount();
  equal('一屏铺满', dataRows(host).length, Math.min(total, 100));
  includes('回到第 1 页', host.textContent, '第 1/1 页');
});

//疑似重复原先只在当前页里找，跨页的那一组标不出来；改成在全量上分组之后，第一页就能看见
test('明细页：疑似重复在全量上分组', async () => {
  const host = await renderAll();
  const marked = dataRows(host).filter((row) => row.className.includes('duplicate-'));
  ok('第一页就能看到重复组', marked.length > 0);
  ok('同组至少两行或跨页只露一行', marked.length >= 1);
  includes('页面写明了只标记不阻断', host.textContent, '只标记不阻断');
});

test('明细页：默认展示 10 列，表头设置改完即生效并留在本地', async () => {
  const host = await renderAll();
  equal('默认列数 + 勾选列 + 操作列', findAll(host, 'thead th').length, EXPENSE_COLUMN_DEFAULT.length + 2);

  click(findByText(host, 'button', '表头设置'));
  const modal = answerModal(false);
  ok('弹窗里能全字段平铺', findByText(modal, 'button', '全字段平铺'));

  click(findByText(host, 'button', '表头设置'));
  const boxes = findAll(answerModalKeepOpen(), 'input');
  for (const box of boxes) box.checked = false;
  boxes[0].checked = true;
  answerModal(true);
  await flush();
  equal('只剩一列', findAll(host, 'thead th').length, 3);
  same('偏好写进了 localStorage', JSON.parse(localStorage.getItem('jotcash.columns')).length, 1);

  localStorage.clear();
});

//辅助函数：拿到当前打开的弹窗但不关它
function answerModalKeepOpen() {
  const opened = findAll(document.body, 'div.modal');
  return opened[opened.length - 1];
}

test('明细页：术语只说摊销，导入说明给的是契约列名', async () => {
  const host = await renderAll();
  click(findByText(host, 'button', '上传账单文件'));
  const modal = answerModalKeepOpen();
  includes('表头契约逐字给出', modal.textContent, COLUMNS.join(','));
  includes('摊销月数留空按 1 处理', modal.textContent, '摊销月数留空按 1 处理');
  excludes('弹窗里不再出现摊分', modal.textContent, '摊分');
  answerModal(false);
  await flush();
  excludes('整页不出现摊分', host.textContent, '摊分');
});

//全量拉取之后条件全空就等于把整个库拖到浏览器里，所以支出日期区间必填，默认给最近一年
test('明细页：支出日期区间默认最近一年，且必填', async () => {
  const host = await renderAll();
  const from = filterInput(host, '支出日期起');
  const to = filterInput(host, '支出日期止');
  equal('默认起日', from.value, formatDate(defaultInquiry().expense_date_start));
  equal('默认止日', to.value, formatDate(defaultInquiry().expense_date_end));
  includes('小字写明必填', filterItemOf(host, '支出日期起').textContent, '必填');

  const before = dataRows(host).map((row) => row.textContent);
  setValue(from, '');
  click(findByText(host, 'button', '查询'));
  await flush();
  includes('留空就拦下来', takeToast(), '支出日期起与支出日期止必填');
  same('结果没被换掉', dataRows(host).map((row) => row.textContent), before);

  setValue(from, formatDate(defaultInquiry().expense_date_start));
  setValue(to, '');
  click(findByText(host, 'button', '查询'));
  await flush();
  includes('只缺止日一样拦', takeToast(), '支出日期起与支出日期止必填');
});

test('明细页：编辑器的校验文案逐条对得上', async () => {
  const host = await renderAll();
  click(findByText(host, 'button', '新增一行'));
  const editor = find(host, '.expense-editor');
  ok('编辑器出来了', editor);
  const field = (label) => editorInput(editor, label);

  setValue(field('支出日期'), '');
  click(findByText(editor, 'button', '入库'));
  await flush();
  includes('支出日期必填', takeToast(), '支出日期必填');

  setValue(field('支出日期'), '2026-09-15');
  setValue(field('支出币种'), 'CN');
  click(findByText(editor, 'button', '入库'));
  await flush();
  includes('币种三位', takeToast(), '支出币种必须是三位币种代码');

  setValue(field('支出币种'), 'CNY');
  setValue(field('支出金额'), '一百');
  click(findByText(editor, 'button', '入库'));
  await flush();
  includes('金额非法', takeToast(), '支出金额非法');

  setValue(field('支出金额'), '128.50');
  setValue(field('摊销月数'), '-2');
  click(findByText(editor, 'button', '入库'));
  await flush();
  includes('摊销月数不得小于 1', takeToast(), '摊销月数不得小于 1');

  //输入框自己会把 0 与空值收成 1，落到校验那一步的只可能是负数
  setValue(field('摊销月数'), '0');
  includes('0 被收成 1 个月', editor.textContent, '摊销 2026-09 至 2026-09');
});

//支出币种与记账币种相同时汇率恒为 1 且不给改，派生预览要当场把摊销区间算出来
test('明细页：编辑器的联动与派生预览', async () => {
  const host = await renderAll();
  click(findByText(host, 'button', '新增一行'));
  const editor = find(host, '.expense-editor');
  const field = (label) => editorInput(editor, label);

  setValue(field('支出币种'), 'CNY');
  ok('同币种时汇率锁死', field('折算汇率').disabled);
  equal('汇率恒为 1', field('折算汇率').value, '1');

  setValue(field('支出币种'), 'USD');
  not('异币种可以手填汇率', field('折算汇率').disabled);
  equal('换币种会清掉汇率交给系统取', field('折算汇率').value, '');

  setValue(field('支出日期'), '2026-09-15');
  setValue(field('摊销月数'), '3');
  includes('派生预览给出摊销区间', editor.textContent, '摊销 2026-09 至 2026-11');
  setValue(field('支出金额'), '100');
  includes('汇率留空时说明会自动获取', editor.textContent, '汇率留空，落库时自动获取');
  setValue(field('折算汇率'), '7.12');
  includes('填了汇率就当场折算出记账金额', editor.textContent, '记账金额 712');
});

test('明细页：编辑保存走乐观锁，版本号自增', async () => {
  const host = await renderAll();
  const before = (await api.selectExpense({ deleted: 0, sort: 'expense_date desc' })).object[0];
  click(findByText(dataRows(host)[0], 'button', '编辑'));
  await flush();
  const editor = find(host, '.expense-editor');
  const typeField = editorInput(editor, '支出类型');
  setValue(typeField, '改过的类型');
  click(findByText(editor, 'button', '保存'));
  await flush();
  includes('保存成功的提示', takeToast(), '已保存');

  const after = (await api.selectExpense({ id: [before.id] })).object[0];
  equal('支出类型已改', after.expense_type, '改过的类型');
  equal('版本号自增', after.version, before.version + 1);
  await rejects('旧版本再存会被乐观锁挡住', mock.updateExpense({ ...before, expense_type: '再改一次' }), '数据已落后');
});

test('明细页：表格展示对齐与表头一致，可编辑列带提示', async () => {
  const host = await renderAll();
  const ths = findAll(host, 'thead th');
  // 默认 10 列：0:checkbox, 1:expense_date, 2:expense_currency, 3:expense_amount, 4:exchange_rate, 5:accounting_currency, 6:accounting_amount, 7:counterparty, 8:remark, 9:expense_type, 10:amortization_months, 11:actions
  ok('支出金额表头右对齐', ths[3].className.includes('text-end'));
  ok('折算汇率表头右对齐', ths[4].className.includes('text-end'));
  ok('记账金额表头右对齐', ths[6].className.includes('text-end'));
  ok('摊销月数表头右对齐', ths[10].className.includes('text-end'));
  ok('支出日期表头左对齐', ths[1].className.includes('text-start'));
  ok('对手方表头左对齐', ths[7].className.includes('text-start'));

  const row = dataRows(host)[0];
  ok('支出金额数据格右对齐', row.childNodes[3].className.includes('text-end'));
  ok('记账金额数据格右对齐', row.childNodes[6].className.includes('text-end'));
  ok('对手方数据格左对齐', row.childNodes[7].className.includes('text-start'));

  ok('对手方单元格可双击编辑', row.childNodes[7].className.includes('cell-editable'));
  equal('对手方单元格提示文案', row.childNodes[7].getAttribute('title'), '双击编辑');
  not('记账金额单元格不可编辑', row.childNodes[6].className.includes('cell-editable'));
});

test('明细页：双击单元格进入行内编辑，点击保存生效', async () => {
  const host = await renderAll();
  const row = dataRows(host)[0];
  const targetCell = row.childNodes[7]; // 交易对手方
  dblclick(targetCell);
  await flush();

  const freshRow = dataRows(host)[0];
  const input = find(freshRow.childNodes[7], 'input');
  ok('双击后单元格内出现输入框', input);

  const saveBtn = findByText(freshRow, 'button', '保存');
  const cancelBtn = findByText(freshRow, 'button', '取消');
  ok('右侧动作列切换为保存按钮', saveBtn);
  ok('右侧动作列切换为取消按钮', cancelBtn);

  setValue(input, '瑞幸咖啡双击改');
  click(saveBtn);
  await flush();

  includes('提示已保存', takeToast(), '已保存');
  equal('页面数据已更新为新值', dataRows(host)[0].childNodes[7].textContent, '瑞幸咖啡双击改');
  not('保存后退出编辑态', find(dataRows(host)[0].childNodes[7], 'input'));
});

test('明细页：同双击多列可同时行内编辑并一同保存', async () => {
  const host = await renderAll();
  let row = dataRows(host)[0];
  dblclick(row.childNodes[7]); // 对手方
  await flush();
  row = dataRows(host)[0];
  dblclick(row.childNodes[8]); // 备注
  await flush();
  row = dataRows(host)[0];

  const counterpartyInput = find(row.childNodes[7], 'input');
  const remarkInput = find(row.childNodes[8], 'input');
  ok('两列均有输入框', counterpartyInput && remarkInput);

  setValue(counterpartyInput, '全家便利店');
  setValue(remarkInput, '早餐豆浆');
  click(findByText(row, 'button', '保存'));
  await flush();

  includes('保存成功', takeToast(), '已保存');
  equal('对手方已落盘', dataRows(host)[0].childNodes[7].textContent, '全家便利店');
  equal('备注已落盘', dataRows(host)[0].childNodes[8].textContent, '早餐豆浆');
});

test('明细页：双击编辑后点击取消，修改丢弃且恢复展示', async () => {
  const host = await renderAll();
  let row = dataRows(host)[0];
  const original = row.childNodes[7].textContent;
  dblclick(row.childNodes[7]);
  await flush();
  row = dataRows(host)[0];

  const input = find(row.childNodes[7], 'input');
  setValue(input, '未保存的内容');
  click(findByText(row, 'button', '取消'));
  await flush();

  not('取消后编辑框消失', find(dataRows(host)[0].childNodes[7], 'input'));
  equal('内容未改变', dataRows(host)[0].childNodes[7].textContent, original);
});

test('明细页：支出币种与支出类型行内编辑采用组合框，支持下拉与录入并保存', async () => {
  const host = await renderAll();
  let row = dataRows(host)[0];
  const currencyCell = row.childNodes[2]; // 支出币种
  dblclick(currencyCell);
  await flush();

  row = dataRows(host)[0];
  const currencyEditCell = row.childNodes[2];
  const currencyInput = find(currencyEditCell, 'input');
  const currencyToggle = find(currencyEditCell, 'button.dropdown-toggle');
  ok('支出币种编辑框出现', currencyInput);
  ok('支出币种包含下拉切换按钮', currencyToggle);

  const typeCell = row.childNodes[9]; // 支出类型
  dblclick(typeCell);
  await flush();

  row = dataRows(host)[0];
  const typeEditCell = row.childNodes[9];
  const typeInput = find(typeEditCell, 'input');
  const typeToggle = find(typeEditCell, 'button.dropdown-toggle');
  ok('支出类型编辑框出现', typeInput);
  ok('支出类型包含下拉切换按钮', typeToggle);

  setValue(currencyInput, 'EUR');
  setValue(typeInput, '商务宴请');
  click(findByText(row, 'button', '保存'));
  await flush();

  includes('保存成功提示', takeToast(), '已保存');
  equal('支出币种已更新为EUR', dataRows(host)[0].childNodes[2].textContent, 'EUR');
  equal('支出类型已更新为商务宴请', dataRows(host)[0].childNodes[9].textContent, '商务宴请');
});

test('明细页：双击摊销月数进入数字编辑框，定宽无跳变且保存生效', async () => {
  const host = await renderAll();
  let row = dataRows(host)[0];
  const monthsCell = row.childNodes[10]; // 摊销月数
  dblclick(monthsCell);
  await flush();

  row = dataRows(host)[0];
  const monthsEditCell = row.childNodes[10];
  const input = find(monthsEditCell, 'input');
  ok('摊销月数编辑框出现', input);
  equal('摊销月数恢复为原生数字输入框', input.type, 'number');
  equal('数值步进为1', input.getAttribute('step'), '1');
  equal('数值最小值为1', input.getAttribute('min'), '1');
  ok('文本靠右对齐', input.className.includes('text-end'));

  setValue(input, '6');
  click(findByText(row, 'button', '保存'));
  await flush();

  includes('保存成功提示', takeToast(), '已保存');
  equal('摊销月数已更新为6', dataRows(host)[0].childNodes[10].textContent, '6');
});

test('明细页：表头所有列均预设宽度与最小宽度，防止编辑态撑开表格抖动', async () => {
  const host = await renderAll();
  const ths = findAll(host, 'thead th');
  for (const th of ths) {
    const style = th.getAttribute('style') || '';
    includes('带有宽度样式', style, 'width:');
    includes('带有最小宽度样式', style, 'min-width:');
  }
  const actionTh = ths[ths.length - 1];
  includes('操作列表头固定宽度11.5rem', actionTh.getAttribute('style'), 'width:11.5rem');
  includes('操作列表头最小宽度11.5rem', actionTh.getAttribute('style'), 'min-width:11.5rem');
  includes('摊销月数表头宽度7.5rem', ths[10].getAttribute('style'), 'width:7.5rem');

  // 验证操作列在正常态与行内编辑态（出现保存+取消按钮）下，单元格定宽不发生抖动跳变
  const firstRow = dataRows(host)[0];
  const normalActionsTd = firstRow.childNodes[firstRow.childNodes.length - 1];
  includes('操作列单元格样式包含宽度11.5rem', normalActionsTd.getAttribute('style'), 'width:11.5rem');
  includes('操作列单元格样式包含最小宽度11.5rem', normalActionsTd.getAttribute('style'), 'min-width:11.5rem');
  ok('未编辑态展示编辑与来源', findByText(normalActionsTd, 'button', '编辑') && findByText(normalActionsTd, 'a', '来源'));

  dblclick(firstRow.childNodes[10]);
  await flush();
  const editingRow = dataRows(host)[0];
  const editingActionsTd = editingRow.childNodes[editingRow.childNodes.length - 1];
  includes('编辑态操作列单元格样式保持宽度11.5rem', editingActionsTd.getAttribute('style'), 'width:11.5rem');
  includes('编辑态操作列单元格样式保持最小宽度11.5rem', editingActionsTd.getAttribute('style'), 'min-width:11.5rem');
  ok('编辑态展示保存与取消', findByText(editingActionsTd, 'button', '保存') && findByText(editingActionsTd, 'button', '取消'));
});

test('明细页：核实视图只列本批次与命中的行，没有分页条', async () => {
  const seedRow = (await api.selectExpense({ deleted: 0, sort: 'id asc' })).object[0];
  const host = await renderPage(renderExpense, { operation_id: String(seedRow.operation_id), verify: '1' });
  includes('核实提示', host.textContent, `审计ID ${seedRow.operation_id} 这一批次`);
  ok('有返回全部明细的入口', findByText(host, 'a', '返回全部明细'));
  equal('核实视图不分页', findAll(host, 'button').filter((button) => button.textContent === '下一页').length, 0);
  ok('全选筛选结果并删除在核实视图里禁用', findByText(host, 'button', '全选筛选结果并删除').disabled);
});

test('明细页：按来源审计ID 进来时筛选条件被 URL 覆盖', async () => {
  const seedRow = (await api.selectExpense({ deleted: 0, sort: 'id asc' })).object[0];
  const host = await renderPage(renderExpense, { operation_id: String(seedRow.operation_id) });
  const expect = await api.selectExpense({ operation_id: [seedRow.operation_id], deleted: 0, sort: 'expense_date desc' });
  includes('条数按这一批算', host.textContent, `共 ${expect.count} 条`);
  //这条路本来就被审计ID 圈死了，再叠一个最近一年的窗口，老批次点进来就会是一张空表
  equal('不叠默认的日期窗口', filterInput(host, '支出日期起').value, '');
});

//导出的就是入库契约那 11 列，所以导出的文件必须能原样再传回去——这是一对镜像，只有往返恒等才算测到位
test('明细页：导出全量，且导出的 CSV 能被入库链路吃回去', async () => {
  const host = await renderAll();
  const total = await totalCount();
  click(findByText(host, 'button', '导出查询结果'));
  await flush();
  includes('导出提示带条数', takeToast(), `已导出 ${total} 笔`);

  const bytes = await lastBlobBytes();
  same('带 BOM，Excel 打开不乱码', [...bytes.slice(0, 3)], [0xef, 0xbb, 0xbf]);
  const text = await lastBlobText();
  const lines = parseCsv(text);
  same('表头就是契约列名', lines[0], COLUMNS);
  equal('导出的是全量不是当前页', lines.length - 1, total);

  const back = await mock.insertExpense('回灌.csv', text, 'CNY');
  equal('原样传回去能全部入库', back.count, total);
});

test('明细页：新增一行等价于一份只有 1 行的 CSV，入库后进核实视图', async () => {
  const host = await renderAll();
  click(findByText(host, 'button', '新增一行'));
  const editor = find(host, '.expense-editor');
  const field = (label) => editorInput(editor, label);
  setValue(field('支出日期'), '2026-09-15');
  setValue(field('支出币种'), 'CNY');
  setValue(field('支出金额'), '66.60');
  setValue(field('交易对手方'), '单行新增用例');
  setValue(field('摊销月数'), '2');
  click(findByText(editor, 'button', '入库'));
  await flush();
  includes('入库提示', takeToast(), '入库 1 笔');

  const inserted = await api.selectExpense({ counterparty_like: '单行新增用例', deleted: 0, sort: 'id desc' });
  equal('确实入了一笔', inserted.count, 1);
  equal('金额按录入的来', inserted.object[0].expense_amount, '66.60');
  equal('摊销月数按录入的来', inserted.object[0].amortization_months, 2);
  includes('跳到了核实视图', location.hash, `#/expense?operation_id=${inserted.object[0].operation_id}&verify=1`);
});

test('明细页：批量删除要二次确认，取消了就一行都不动', async () => {
  const host = await renderAll();
  const target = dataRows(host)[0];
  check(find(target, 'input'), true);
  const before = await totalCount();

  click(findByText(host, 'button', '批量删除'));
  await flush();
  answerModal(false);
  await flush();
  equal('取消之后条数不变', await totalCount(), before);

  click(findByText(host, 'button', '批量删除'));
  await flush();
  answerModal(true);
  await flush();
  includes('删除成功提示', takeToast(), '已删除 1 笔');
  equal('未删除视图少了一条', await totalCount(), before - 1);
});

test('明细页：全选筛选结果并删除，要把关键字敲对才放行', async () => {
  const host = await renderAll();
  const before = await totalCount();
  click(findByText(host, 'button', '全选筛选结果并删除'));
  await flush();
  const modal = answerModalKeepOpen();
  const okButton = findByText(modal, 'button', '确认');
  ok('没敲关键字时按钮是禁用的', okButton.disabled);
  click(okButton);
  await flush();
  equal('禁用的按钮点不动，一行没删', await totalCount(), before);

  const input = find(modal, 'input');
  setValue(input, '确认删除');
  not('敲对了就放行', okButton.disabled);
  click(okButton);
  await flush();
  includes('删除提示', takeToast(), `已删除 ${before} 笔`);
  equal('筛选全集都没了', await totalCount(), 0);
});

