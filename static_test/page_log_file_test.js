import test from 'node:test';
import './helper/browser.js';
import {
  answerModal,
  click,
  find,
  findAll,
  findByText,
  flush,
  lastBlobText,
  mockSession,
  modals,
  renderPage,
  setValue,
  takeToast,
} from './helper/fixture.js';
import { equal, excludes, includes, not, ok, same } from './helper/check.js';
import * as api from '../static/js/api.js';
import { CSV_FIELDS } from '../static/js/config.js';
import { render as renderFileMeta } from '../static/js/page_file_meta.js';
import { render as renderOperationLog } from '../static/js/page_operation_log.js';

mockSession();

const COLUMNS = CSV_FIELDS.map((field) => field.column);

//辅助函数：按标签文案取筛选格里的输入框
function filterInput(host, label) {
  const item = findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === label);
  return find(item, 'input');
}

//辅助函数：数表格里的数据行
function dataRows(host) {
  return findAll(host, 'tbody tr').filter((row) => row.childNodes[0].getAttribute('colspan') === null);
}

test('审计页：操作摘要是模糊搜索，要有小字告诉用户', async () => {
  const host = await renderPage(renderOperationLog, {});
  const item = findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === '操作摘要');
  includes('小字提示', item.textContent, '模糊匹配，输入片段即可');
});

test('审计页：按摘要模糊筛选，命中的行才留下', async () => {
  const host = await renderPage(renderOperationLog, {});
  const before = dataRows(host).length;
  ok('种子里有审计', before > 0);

  setValue(filterInput(host, '操作摘要'), '2609-招商');
  click(findByText(host, 'button', '查询'));
  await flush();
  const rows = dataRows(host);
  equal('只剩这一条', rows.length, 1);
  includes('命中的是入库那条', rows[0].textContent, '2609-招商.csv');
  includes('条数跟着变', host.textContent, '共 1 条');

  click(findByText(host, 'button', '重置'));
  await flush();
  equal('重置回到全量', dataRows(host).length, before);
});

test('审计页：变更内容能展开看全文，没有变更就不给入口', async () => {
  const seeded = (await api.selectExpense({ deleted: 0, sort: 'id asc' })).object[0];
  await api.updateExpense({ ...seeded, remark: '为了造一条变更内容' });
  const host = await renderPage(renderOperationLog, {});
  setValue(filterInput(host, '操作摘要'), `编辑明细 ${seeded.id}`);
  click(findByText(host, 'button', '查询'));
  await flush();

  click(findByText(host, 'button', '查看'));
  const modal = answerModal(false);
  includes('弹窗标题带审计ID', modal.textContent, '的变更内容');
  //后端记的是前后两份整快照，铺开就是两坨 21 字段 JSON，所以这一层自己比一遍只列变了的字段
  includes('列的是字段展示名', modal.textContent, '交易备注');
  includes('记了后值', modal.textContent, '为了造一条变更内容');
  excludes('没变的字段不占地方', modal.textContent, '摊销起始月');
});

test('审计页：只留痕不承载业务，关联入口是两条查询链接', async () => {
  const host = await renderPage(renderOperationLog, {});
  includes('页面写明了定位', host.textContent, '审计只留痕、不承载业务');
  const row = dataRows(host)[0];
  const links = findAll(row, 'a');
  same('两条关联', links.map((link) => link.textContent), ['本批明细', '本批文件']);
  ok('指向明细页', links[0].getAttribute('href').startsWith('#/expense?operation_id='));
  ok('指向文件页', links[1].getAttribute('href').startsWith('#/file-meta?operation_id='));
  ok('审计结果有徽标', find(row, 'span.badge'));
});

test('文件页：文件名是模糊搜索，要有小字告诉用户', async () => {
  const host = await renderPage(renderFileMeta, {});
  const item = findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === '文件名');
  includes('小字提示', item.textContent, '模糊匹配，输入片段即可');
});

test('文件页：按文件名模糊筛选，大小按单位进位展示', async () => {
  const host = await renderPage(renderFileMeta, {});
  const before = dataRows(host).length;
  ok('种子里有文件', before > 0);

  setValue(filterInput(host, '文件名'), '招商');
  click(findByText(host, 'button', '查询'));
  await flush();
  const rows = dataRows(host);
  equal('只剩这一条', rows.length, 1);
  includes('文件名', rows[0].textContent, '2609-招商.csv');
  ok('文件大小带单位', /\d (B|KB|MB|GB)/.test(rows[0].textContent));
  includes('页面写明了文件只留存不删除', host.textContent, '文件只留存不删除');

  click(findByText(host, 'button', '重置'));
  await flush();
  equal('重置回到全量', dataRows(host).length, before);
});

test('文件页：下载走的是入库时那份内容，失败只提示不崩页', async () => {
  const uploaded = await api.insertExpense('下载用例.csv', [
    '银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数',
    ',,2026-09-15,CNY,1,下载用例,,,,,',
  ].join('\r\n'));
  ok('先造一份有内容的文件', uploaded.count === 1);

  const host = await renderPage(renderFileMeta, {});
  setValue(filterInput(host, '文件名'), '下载用例');
  click(findByText(host, 'button', '查询'));
  await flush();
  click(findByText(dataRows(host)[0], 'button', '下载'));
  await flush();
  includes('下载成功提示', takeToast(), '已下载 下载用例.csv');
  includes('下的就是入库那份内容', await lastBlobText(), '下载用例');
});

//种子文件存的就是那一批明细导出的 CSV，所以预览出来该是一张表，而且表头就是落库契约那 11 列
test('文件页：预览种子文件，铺出来的是入库那份 CSV', async () => {
  const host = await renderPage(renderFileMeta, {});
  click(findByText(host, 'button', '重置'));
  await flush();
  setValue(filterInput(host, '文件名'), '招商');
  click(findByText(host, 'button', '查询'));
  await flush();

  click(findByText(dataRows(host)[0], 'button', '预览'));
  await flush();
  const modal = modals()[modals().length - 1].node;
  includes('弹窗标题带文件名', modal.textContent, '预览 2609-招商.csv');
  same('表头就是落库契约那 11 列', findAll(modal, 'thead th').map((cell) => cell.textContent), COLUMNS);
  ok('铺出了数据行', findAll(modal, 'tbody tr').length > 0);
  ok('页脚同时给了下载', findByText(modal, 'button', '下载'));
});

//预览与下载取的是同一份内容，走的也是同一个接口；这里盯的是「铺出来的确实是上传那一份」
test('文件页：预览上传上来的文件，铺的是上传那份内容', async () => {
  await api.insertExpense('预览用例.csv', [
    '银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数',
    ',,2026-09-15,CNY,1,预览用例,,,,,',
  ].join('\r\n'));
  const host = await renderPage(renderFileMeta, {});
  setValue(filterInput(host, '文件名'), '预览用例');
  click(findByText(host, 'button', '查询'));
  await flush();
  click(findByText(dataRows(host)[0], 'button', '预览'));
  await flush();
  const modal = modals()[modals().length - 1].node;
  ok('上传的就是 CSV，按表格铺', find(modal, 'table'));
  includes('铺的是上传那份内容', modal.textContent, '预览用例');
});

test('文件页：内容哈希只露前 16 位，全文挂在 title 上', async () => {
  const host = await renderPage(renderFileMeta, {});
  click(findByText(host, 'button', '重置'));
  await flush();
  const cells = findAll(dataRows(host)[0], 'td');
  const hashCell = cells[3];
  ok('展示的是截断值', hashCell.textContent.length <= 16);
  ok('全文在 title 上', hashCell.getAttribute('title').startsWith(hashCell.textContent));
  not('文件页没有删除入口', findByText(host, 'button', '删除'));
});
