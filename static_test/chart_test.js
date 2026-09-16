import test from 'node:test';
import './helper/browser.js';
import { charts, find, findAll } from './helper/fixture.js';
import { equal, includes, not, ok, same } from './helper/check.js';
import { chartCard, colorOf, doughnut, horizontalBar, line, mountChart, stackedBar } from '../static/js/chart.js';
import { formatAmount } from '../static/js/util.js';

//辅助函数：把画布操作录下来，柱顶合计那段是直接往 canvas 上写字的，只能这么验
function newCanvasRecorder() {
  const written = [];
  return {
    written,
    ctx: {
      save: () => {},
      restore: () => {},
      fillText: (text, x, y) => written.push({ text, x, y }),
    },
  };
}

test('调色板：按序号取色且循环，不会取到空色', () => {
  equal('同一序号取同一个色', colorOf(0), colorOf(0));
  not('相邻序号不同色', colorOf(0) === colorOf(1));
  equal('超出长度就循环', colorOf(0), colorOf(12));
  for (let index = 0; index < 30; index += 1) ok(`第 ${index} 个颜色有值`, /^#[0-9a-f]{6}$/i.test(colorOf(index)));
});

test('堆叠柱：双向堆叠、逐段配色、坐标轴按金额格式化', () => {
  const config = stackedBar({
    labels: ['2026-09', '2026-10'],
    datasets: [{ label: '餐饮', data: [30, 10] }, { label: '数码', data: [70, 90] }],
    totals: [100, 100],
    format: formatAmount,
  });
  ok('x 轴堆叠', config.options.scales.x.stacked);
  ok('y 轴堆叠', config.options.scales.y.stacked);
  equal('刻度按金额格式化', config.options.scales.y.ticks.callback(1234.5), '1234.50');
  not('两段同色', config.data.datasets[0].backgroundColor === config.data.datasets[1].backgroundColor);
  ok('给柱顶标签留了上边距', config.options.layout.padding.top > 0);
  same('插件按顺序挂好', config.plugins.map((plugin) => plugin.id), ['datalabels', 'stackTotal']);
});

//段上标的是「金额 + 占该月的比例」，占比的分母必须是调用方给的合计，不能是图表自己加的
test('堆叠柱：段标签与提示里的占比都按传进来的合计算', () => {
  const config = stackedBar({
    labels: ['2026-09'],
    datasets: [{ label: '餐饮', data: [30] }],
    totals: [120],
    format: formatAmount,
  });
  const labels = config.options.plugins.datalabels;
  equal('段标签是金额换行占比', labels.formatter(30, { dataIndex: 0 }), '30.00\n25.0%');
  equal('零值不标', labels.formatter(0, { dataIndex: 0 }), null);
  equal('放不下就自己藏', labels.display, 'auto');

  const tooltip = config.options.plugins.tooltip.callbacks;
  equal('提示行', tooltip.label({ dataset: { label: '餐饮' }, parsed: { y: 30 }, dataIndex: 0 }), '餐饮：30.00（25.0%）');
  equal('提示脚注是合计', tooltip.footer([{ dataIndex: 0 }]), '合计 120.00');
});

test('堆叠柱：合计为 0 与负数占比的边界', () => {
  const config = stackedBar({ labels: ['2026-09'], datasets: [{ label: '退款', data: [-20] }], totals: [0], format: formatAmount });
  includes('零分母给破折号', config.options.plugins.datalabels.formatter(-20, { dataIndex: 0 }), '—');

  const negative = stackedBar({ labels: ['2026-09'], datasets: [{ label: '退款', data: [-20] }], totals: [100], format: formatAmount });
  includes('负数占比照实给', negative.options.plugins.datalabels.formatter(-20, { dataIndex: 0 }), '-20.0%');
});

//柱顶合计画在最高那一段的上沿，取的是调用方给的精确值
test('堆叠柱：柱顶合计画在最高段的上沿，印的是传进来的值', () => {
  const config = stackedBar({
    labels: ['2026-09', '2026-10'],
    datasets: [{ label: '餐饮', data: [30, 10] }],
    totals: [0.3, 100],
    format: formatAmount,
  });
  const recorder = newCanvasRecorder();
  const plugin = config.plugins.find((item) => item.id === 'stackTotal');
  plugin.afterDatasetsDraw({
    ctx: recorder.ctx,
    data: { labels: ['2026-09', '2026-10'] },
    getSortedVisibleDatasetMetas: () => [
      { data: [{ x: 10, y: 80 }, { x: 50, y: 60 }] },
      { data: [{ x: 10, y: 40 }, { x: 50, y: 70 }] },
    ],
  }, {}, config.options.plugins.stackTotal);

  equal('每根柱子一个合计', recorder.written.length, 2);
  equal('印的是精确值不是浮点和', recorder.written[0].text, '0.30');
  equal('横坐标取这根柱子的', recorder.written[0].x, 10);
  equal('纵坐标取最高那一段再抬 4 像素', recorder.written[0].y, 36);
  equal('第二根取另一段', recorder.written[1].y, 56);
});

test('环形图：块上标占比，金额进提示', () => {
  const config = doughnut({ labels: ['餐饮', '数码'], values: [25, 75], total: 100, format: formatAmount });
  equal('类型', config.type, 'doughnut');
  //图表这一侧的占比固定一位小数，与逐月逐类型表那一侧的 Decimal 口径不同，是两套写法
  equal('块上只标占比', config.options.plugins.datalabels.formatter(25), '25.0%');
  equal('提示带金额与占比', config.options.plugins.tooltip.callbacks.label({ label: '餐饮', parsed: 25 }), '餐饮：25.00（25.0%）');
  equal('每块一个颜色', config.data.datasets[0].backgroundColor.length, 2);
});

test('折线图：两条线各自配色，提示按金额格式化', () => {
  const config = line({
    labels: ['2026-09'],
    datasets: [{ label: '记账金额', data: [100] }, { label: '摊销金额', data: [50] }],
    format: formatAmount,
  });
  equal('类型', config.type, 'line');
  not('两条线不同色', config.data.datasets[0].borderColor === config.data.datasets[1].borderColor);
  equal('提示', config.options.plugins.tooltip.callbacks.label({ dataset: { label: '记账金额' }, parsed: { y: 100 } }), '记账金额：100.00');
  equal('刻度', config.options.scales.y.ticks.callback(0.1 + 0.2), '0.30');
});

test('横向条：分类名横过来放，条末标金额', () => {
  const config = horizontalBar({ labels: ['盒马鲜生'], values: [128.5], format: formatAmount });
  equal('横向', config.options.indexAxis, 'y');
  not('不要图例', config.options.plugins.legend.display);
  equal('条末标金额', config.options.plugins.datalabels.formatter(128.5), '128.50');
  ok('右侧留了标签的位置', config.options.layout.padding.right > 0);
});

test('图表卡片：标题与说明都在，实例等进了文档才建', () => {
  const card = chartCard({
    title: '每月支出 × 支出类型（摊销金额）',
    description: '每根柱子是一个月',
    height: '26rem',
    config: doughnut({ labels: ['甲'], values: [1], total: 1, format: formatAmount }),
  });
  includes('标题', card.textContent, '每月支出 × 支出类型（摊销金额）');
  includes('说明', card.textContent, '每根柱子是一个月');
  includes('高度按调用方给的', find(card, '.chart-box').getAttribute('style'), '26rem');
  equal('画布已经建好', findAll(card, 'canvas').length, 1);
  equal('挂载前不建实例', charts().length, 0);

  mountChart();
  equal('挂载后建实例', charts().length, 1);
});

//同一张 canvas 被两个实例占着，Chart.js 直接报错，所以必须先销毁再建
test('图表卡片：重挂载先销毁旧实例', () => {
  chartCard({ title: '甲', config: doughnut({ labels: ['甲'], values: [1], total: 1, format: formatAmount }) });
  mountChart();
  const first = charts();
  equal('只剩这一张活着', first.length, 1);

  chartCard({ title: '乙', config: doughnut({ labels: ['乙'], values: [2], total: 2, format: formatAmount }) });
  mountChart();
  ok('旧实例已销毁', first[0].destroyed);
  equal('新实例只有一张', charts().length, 1);
});
