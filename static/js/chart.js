import { el } from './util.js';

//支出类型、对手方这类分类维度按序号取色，颜色都够深，段内的白色标签才读得清
const PALETTE = [
  '#0d6efd', '#dc3545', '#198754', '#fd7e14', '#6f42c1', '#0aa2c0',
  '#d63384', '#6c757d', '#20603d', '#9c6f19', '#3d5a80', '#7a3b69',
];

export function colorOf(index) {
  return PALETTE[index % PALETTE.length];
}

//图例与坐标轴跟着 bootstrap 的正文色走，换主题时不用再改图表
Chart.defaults.color = getComputedStyle(document.body).getPropertyValue('--bs-body-color') || '#212529';
Chart.defaults.font.family = getComputedStyle(document.body).fontFamily;
Chart.defaults.font.size = 11;

//占比的分母是整根柱子的合计；合计为 0 时没有比例可言，退款这类负数会让比例是负的，那是真实口径
function percentText(value, total) {
  if (!total) return '—';
  return `${((value / total) * 100).toFixed(1)}%`;
}

function baseOptions() {
  return { responsive: true, maintainAspectRatio: false };
}

//柱顶的合计标签：datalabels 只认得到每一段，整根柱子的合计得自己画
const stackTotal = {
  id: 'stackTotal',
  afterDatasetsDraw(chart, args, options) {
    const ctx = chart.ctx;
    ctx.save();
    ctx.fillStyle = Chart.defaults.color;
    ctx.font = `600 11px ${Chart.defaults.font.family}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'bottom';
    chart.data.labels.forEach((label, index) => {
      let x = null;
      let top = null;
      for (const meta of chart.getSortedVisibleDatasetMetas()) {
        const element = meta.data[index];
        if (!element) continue;
        x = element.x;
        if (top === null || element.y < top) top = element.y;
      }
      if (x === null) return;
      ctx.fillText(options.format(options.totals[index]), x, top - 4);
    });
    ctx.restore();
  },
};

//堆叠柱：每段标金额与占该柱的比例，柱顶标该柱合计。段太薄放不下时 datalabels 自己藏，
//完整数据由调用方另外铺成表格兜底。
//totals 由调用方按它自己的精确类型算好传进来，这里不拿 number 再加一遍：
//0.01+0.14+0.15 用浮点加出来是 0.30000000000000004，柱顶就会印着这么一串
export function stackedBar({ labels, datasets, totals, format }) {
  return {
    type: 'bar',
    data: {
      labels,
      datasets: datasets.map((dataset, index) => ({ ...dataset, backgroundColor: colorOf(index) })),
    },
    options: {
      ...baseOptions(),
      //柱顶的合计标签画在绘图区外面，不留出上边距会被裁掉
      layout: { padding: { top: 22 } },
      interaction: { mode: 'index', intersect: false },
      scales: {
        x: { stacked: true },
        y: { stacked: true, ticks: { callback: (value) => format(value) } },
      },
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, padding: 8 } },
        tooltip: {
          callbacks: {
            label: (item) => `${item.dataset.label}：${format(item.parsed.y)}（${percentText(item.parsed.y, totals[item.dataIndex])}）`,
            footer: (items) => `合计 ${format(totals[items[0].dataIndex])}`,
          },
        },
        datalabels: {
          display: 'auto',
          color: '#fff',
          font: { size: 10 },
          formatter: (value, context) => (value ? `${format(value)}\n${percentText(value, totals[context.dataIndex])}` : null),
        },
        stackTotal: { totals, format },
      },
    },
    plugins: [ChartDataLabels, stackTotal],
  };
}

//环形：每块标占比，金额进 tooltip——占比与金额都塞进块里，块一小就全挤没了。
//total 同样由调用方给，理由与堆叠柱一致
export function doughnut({ labels, values, total, format }) {
  return {
    type: 'doughnut',
    data: { labels, datasets: [{ data: values, backgroundColor: labels.map((label, index) => colorOf(index)) }] },
    options: {
      ...baseOptions(),
      plugins: {
        legend: { position: 'right', labels: { boxWidth: 12, padding: 8 } },
        tooltip: { callbacks: { label: (item) => `${item.label}：${format(item.parsed)}（${percentText(item.parsed, total)}）` } },
        datalabels: {
          display: 'auto',
          color: '#fff',
          font: { size: 10, weight: '600' },
          formatter: (value) => percentText(value, total),
        },
      },
    },
    plugins: [ChartDataLabels],
  };
}

export function line({ labels, datasets, format }) {
  return {
    type: 'line',
    data: {
      labels,
      datasets: datasets.map((dataset, index) => ({
        ...dataset,
        borderColor: colorOf(index),
        backgroundColor: colorOf(index),
        tension: 0.25,
        pointRadius: 2,
      })),
    },
    options: {
      ...baseOptions(),
      interaction: { mode: 'index', intersect: false },
      scales: { y: { ticks: { callback: (value) => format(value) } } },
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, padding: 8 } },
        tooltip: { callbacks: { label: (item) => `${item.dataset.label}：${format(item.parsed.y)}` } },
      },
    },
  };
}

//横向条：分类名可能很长，横过来才不会被挤成竖排
export function horizontalBar({ labels, values, format }) {
  return {
    type: 'bar',
    data: { labels, datasets: [{ data: values, backgroundColor: labels.map((label, index) => colorOf(index)) }] },
    options: {
      ...baseOptions(),
      indexAxis: 'y',
      layout: { padding: { right: 56 } },
      scales: { x: { ticks: { callback: (value) => format(value) } } },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (item) => format(item.parsed.x) } },
        datalabels: { anchor: 'end', align: 'end', color: Chart.defaults.color, font: { size: 10 }, formatter: (value) => format(value) },
      },
    },
    plugins: [ChartDataLabels],
  };
}

// ===== 挂载 =====

//Chart.js 要量 canvas 所在容器的尺寸，容器还没进文档时量到的是 0。
//所以建节点与建实例分两步：页面先把节点挂进文档，再统一 mountChart
const pending = [];
const living = [];

export function chartCard({ title, description, config, header, height }) {
  const canvas = el('canvas');
  pending.push({ canvas, config });
  return el('div', { class: 'card mb-3' }, [
    el('div', { class: 'card-header py-2 d-flex flex-wrap align-items-center gap-2' }, [
      el('span', { class: 'small fw-semibold', text: title }),
      header ? el('div', { class: 'ms-auto' }, [header]) : null,
    ]),
    el('div', { class: 'card-body' }, [
      description ? el('p', { class: 'text-secondary small mb-2', text: description }) : null,
      el('div', { class: 'chart-box', style: height ? `height:${height}` : null }, [canvas]),
    ]),
  ]);
}

//先销毁再建：同一张 canvas 被两个实例占着，Chart.js 直接报错
export function mountChart() {
  for (const chart of living) chart.destroy();
  living.length = 0;
  for (const item of pending) living.push(new Chart(item.canvas, item.config));
  pending.length = 0;
}
