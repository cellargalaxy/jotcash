import test from 'node:test';
import './helper/browser.js';
import { charts, click, find, findAll, findByText, flush, renderPage, setValue } from './helper/fixture.js';
import { equal, excludes, includes, near, not, ok, same } from './helper/check.js';
import * as api from '../static/js/api.js';
import { render as renderStatistic } from '../static/js/page_statistic.js';

api.seedMock();

//辅助函数：按标签文案取筛选格里的输入框
function filterInput(host, label) {
  const item = findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === label);
  return find(item, 'input');
}

//辅助函数：跑一次统计页，返回页面节点与这一屏画出来的图
async function renderStatisticPage() {
  const host = await renderPage(renderStatistic);
  return { host, drawn: charts() };
}

test('统计页：四张图一次画齐，堆叠柱是双向堆叠', async () => {
  const { drawn } = await renderStatisticPage();
  same('图的类型与顺序', drawn.map((chart) => chart.config.type), ['bar', 'doughnut', 'line', 'bar']);
  const stacked = drawn[0].config;
  ok('x 轴堆叠', stacked.options.scales.x.stacked);
  ok('y 轴堆叠', stacked.options.scales.y.stacked);
  same('挂了 datalabels 与柱顶合计两个插件', stacked.plugins.map((plugin) => plugin.id), ['datalabels', 'stackTotal']);
  ok('有月份', stacked.data.labels.length > 0);
  ok('有支出类型', stacked.data.datasets.length > 0);
  for (const dataset of stacked.data.datasets) {
    equal(`数据点数与月份数对齐 ${dataset.label}`, dataset.data.length, stacked.data.labels.length);
    ok(`每段都有颜色 ${dataset.label}`, Boolean(dataset.backgroundColor));
  }
});

//柱顶合计由统计层用 Decimal 算好传进来，图表不再拿 number 把各段加一遍
test('统计页：柱顶合计等于各段之和，且不是图表自己加出来的', async () => {
  const { drawn } = await renderStatisticPage();
  const config = drawn[0].config;
  const totals = config.options.plugins.stackTotal.totals;
  equal('合计数与月份数一致', totals.length, config.data.labels.length);
  config.data.labels.forEach((month, index) => {
    const sum = config.data.datasets.reduce((acc, dataset) => acc + dataset.data[index], 0);
    near(`${month} 的柱顶合计`, totals[index], sum);
  });
  ok('合计是调用方传进来的，不在 data 里', Array.isArray(config.options.plugins.stackTotal.totals));
});

test('统计页：柱高默认摊销口径，切成记账口径后整屏重绘', async () => {
  const { host, drawn } = await renderStatisticPage();
  includes('图 1 标题', findByText(host, 'span', '每月支出 × 支出类型').textContent, '（摊销金额）');
  const control = find(findByText(host, 'div.card-header', '柱高口径'), 'select');
  equal('口径下拉默认选中摊销', control.value, 'amortization');
  const amortizationMonths = drawn[0].config.data.labels.length;

  setValue(control, 'accounting');
  await flush();
  const redrawn = charts();
  equal('仍是四张图', redrawn.length, 4);
  includes('标题跟着口径走', findByText(host, 'span', '每月支出 × 支出类型').textContent, '（记账金额）');
  ok('记账口径覆盖的月份不会比摊销口径多', redrawn[0].config.data.labels.length <= amortizationMonths);
  includes('逐月逐类型表的标题也跟着走', findByText(host, 'span', '逐月逐类型明细').textContent, '（记账金额）');

  setValue(control, 'amortization');
  await flush();
});

//一笔明细的各月份额之和恒等于它的记账金额，所以两条线的总额必须相等，差的只是月份分布
test('统计页：两种口径的月度合计线，总额相等', async () => {
  const { drawn } = await renderStatisticPage();
  const line = drawn[2].config;
  same('两条线', line.data.datasets.map((dataset) => dataset.label), ['记账金额', '摊销金额']);
  const sum = (dataset) => dataset.data.reduce((acc, value) => acc + value, 0);
  near('总额相等', sum(line.data.datasets[0]), sum(line.data.datasets[1]), 0.01);
  equal('两条线共用同一条月轴', line.data.datasets[0].data.length, line.data.datasets[1].data.length);
});

test('统计页：对手方榜横过来画且不超过 Top 10', async () => {
  const { drawn } = await renderStatisticPage();
  const ranked = drawn[3].config;
  equal('横向', ranked.options.indexAxis, 'y');
  ok('不超过 10 条', ranked.data.labels.length <= 10);
  const values = ranked.data.datasets[0].data;
  for (let index = 1; index < values.length; index += 1) {
    ok(`按金额从大到小 ${index}`, values[index - 1] >= values[index]);
  }
});

test('统计页：概览与逐月逐类型表跟聚合结果对得上', async () => {
  const { host, drawn } = await renderStatisticPage();
  const all = await api.selectExpense({ deleted: 0, sort: 'expense_date desc' });
  const overview = findAll(host, '.card-body.py-2.px-3').map((node) => node.textContent);
  ok('明细笔数是全量条数', overview.some((text) => text.includes('明细笔数') && text.includes(String(all.object.length))));
  ok('覆盖月份按当前口径', overview.some((text) => text.includes('覆盖月份') && text.includes('摊销金额')));

  const table = findByText(host, 'div.card', '逐月逐类型明细');
  equal('表头是月份 + 各类型 + 合计', findAll(table, 'thead th').length, drawn[0].config.data.datasets.length + 2);
  equal('表格行数等于月份数', findAll(table, 'tbody tr').length, drawn[0].config.data.labels.length);
});

//记账金额按各自的记账币种存，混着相加就不是钱了，这条提示比任何一张图都重要
test('统计页：范围内有多种记账币种时必须告警', async () => {
  const { host } = await renderStatisticPage();
  const alert = find(host, '.alert-warning');
  ok('告警在', alert);
  includes('说清了失真', alert.textContent, '金额已经失真');
});

test('统计页：术语只说摊销，不再出现摊分', async () => {
  const { host } = await renderStatisticPage();
  includes('出现摊销', host.textContent, '摊销');
  excludes('不出现摊分', host.textContent, '摊分');
});

test('统计页：筛完没有数据时给空态，不留上一屏的图', async () => {
  const { host } = await renderStatisticPage();
  setValue(filterInput(host, '交易对手方'), '不存在的对手方');
  click(findByText(host, 'button', '查询'));
  await flush();
  includes('空态文案', host.textContent, '当前筛选没有可统计的数据');
  equal('没有活着的图', charts().length, 0);

  click(findByText(host, 'button', '重置'));
  await flush();
  ok('重置之后图又回来了', charts().length === 4);
  not('筛选框已清空', filterInput(host, '交易对手方').value);
});
