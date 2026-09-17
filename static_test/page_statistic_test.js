import test from 'node:test';
import './helper/browser.js';
import { charts, click, find, findAll, findByText, flush, mockSession, renderPage, setValue } from './helper/fixture.js';
import { equal, excludes, includes, near, not, ok, same } from './helper/check.js';
import * as api from '../static/js/api.js';
import { defaultInquiry } from '../static/js/expense_inquiry.js';
import { render as renderStatistic } from '../static/js/page_statistic.js';
import { formatDate, monthOf } from '../static/js/util.js';

mockSession();

//辅助函数：按标签文案取筛选格里的输入框
function filterInput(host, label) {
  const item = findAll(host, '.filter-item').find((node) => node.childNodes[0].textContent === label);
  return find(item, 'input');
}

//辅助函数：筛选区间对应的月窗，统计页画出来的月份一个都不该越出去
function monthWindow() {
  const inquiry = defaultInquiry();
  return { start: monthOf(formatDate(inquiry.expense_date_start)), end: monthOf(formatDate(inquiry.expense_date_end)) };
}

//辅助函数：跑一次统计页，返回页面节点与这一屏画出来的图
async function renderStatisticPage() {
  const host = await renderPage(renderStatistic);
  return { host, drawn: charts() };
}

//统计页与明细页共用同一套筛选条件，统计也是一次拉全量再前端算，所以同样得被日期窗口圈住
test('统计页：支出日期区间默认最近一年', async () => {
  const { host } = await renderStatisticPage();
  equal('默认起日', filterInput(host, '支出日期起').value, formatDate(defaultInquiry().expense_date_start));
  equal('默认止日', filterInput(host, '支出日期止').value, formatDate(defaultInquiry().expense_date_end));
});

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

//两条线都只画筛选区间内的月份：摊销口径会把跨出区间的那截尾巴剪掉，所以两条线的总额本来就不该相等；
//真正该对上的是「对比线里那条摊销线」与「主图的柱顶合计」，它们走的是两条不同的聚合路径
test('统计页：两种口径的月度合计线只画区间内的月份，摊销线与主图逐月对得上', async () => {
  const { drawn } = await renderStatisticPage();
  const line = drawn[2].config;
  same('两条线', line.data.datasets.map((dataset) => dataset.label), ['记账金额', '摊销金额']);
  equal('两条线共用同一条月轴', line.data.datasets[0].data.length, line.data.datasets[1].data.length);
  for (const month of line.data.labels) {
    ok(`${month} 不越出筛选区间`, month >= monthWindow().start && month <= monthWindow().end);
  }
  same('与主图共用同一条月轴', line.data.labels, drawn[0].config.data.labels);
  const totals = drawn[0].config.options.plugins.stackTotal.totals;
  line.data.labels.forEach((month, index) => {
    near(`${month} 摊销线与柱顶合计一致`, line.data.datasets[1].data[index], totals[index], 0.01);
  });
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

//摊销口径要的不是「支出日期落在区间内」，而是「摊销区间与区间有交集」：
//支出日期早于区间、但摊销跨进区间的那几笔，记账口径下不该出现，摊销口径下必须出现，且只算摊进来的那部分
test('统计页：摊销口径把摊进区间的旧明细一并取回，记账口径不取', async () => {
  //2026-01 一笔 1200 摊 12 个月，每月 100，摊到 2026-12；区间卡在 2026-08~2026-09，它的支出日期在区间之外
  const inserted = await api.insertExpense('跨期分期.csv', [
    '银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数',
    ',,2026-01-20,CNY,1200,跨期分期用例,,,,跨期分期,12',
  ].join('\r\n'));
  equal('先造出这笔跨期分期', inserted.count, 1);

  const { host } = await renderStatisticPage();
  setValue(filterInput(host, '支出日期起'), '2026-08-01');
  setValue(filterInput(host, '支出日期止'), '2026-09-30');
  click(findByText(host, 'button', '查询'));
  await flush();

  const control = find(findByText(host, 'div.card-header', '柱高口径'), 'select');
  equal('当前是摊销口径', control.value, 'amortization');
  const typeOf = () => charts()[0].config.data.datasets.find((dataset) => dataset.label === '跨期分期');
  const amortized = typeOf();
  ok('摊销口径下这笔露面了', amortized);
  same('月轴正好是区间内那两个月', charts()[0].config.data.labels, ['2026-08', '2026-09']);
  same('每月只算摊进来的 100，不是整笔 1200', amortized.data, [100, 100]);

  setValue(control, 'accounting');
  await flush();
  not('记账口径下它整个不出现', typeOf());

  setValue(control, 'amortization');
  await flush();
  click(findByText(host, 'button', '重置'));
  await flush();
});
