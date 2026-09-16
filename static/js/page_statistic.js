import * as api from './api.js';
import { AMOUNT_SCALE } from './config.js';
import { chartCard, doughnut, horizontalBar, line, mountChart, stackedBar } from './chart.js';
import { emptyRow, select } from './component.js';
import { expenseFilter, loadCandidate, newInquiry } from './expense_inquiry.js';
import { addMonth, appendChildren, clear, el, formatAmount, formatDate, formatMonth, monthOf, toastErr } from './util.js';

const MEASURE_ACCOUNTING = 'accounting';
const MEASURE_AMORTIZATION = 'amortization';

const MEASURES = [
  { value: MEASURE_ACCOUNTING, name: '记账金额' },
  { value: MEASURE_AMORTIZATION, name: '摊销金额' },
];

//支出类型、交易对手方留空的行不能丢掉，归到这一档里
const UNFILLED = '未填写';
const TOP_COUNT = 10;
//连续月轴的长度上限。摊销月数没有上限，真填了个离谱的值月轴会长到画不出来，超了就退回只画有数据的月份
const MONTH_AXIS_MAX = 120;

//跨次渲染保留筛选条件与口径，切页面回来不用重新填一遍。
//与明细页各持一份：共享一份会让在统计页缩小范围把明细页的筛选也改掉
const state = {
  inquiry: newInquiry(),
  rows: [],
  //默认摊销口径：大额分期的钱本来就不是当月花掉的，记账口径会让那一个月凭空拱起一根柱子
  measure: MEASURE_AMORTIZATION,
};

let host = null;
let bodyHost = null;

// ===== 统计 =====

//一笔明细在各月的份额。记账口径整笔落在支出日期所属月；摊销口径按摊销月数均摊到摊销起止月，
//尾差落在最后一个月——除不尽时逐月四舍五入，各月之和会对不上原始记账金额
function monthShares(row, measure) {
  const amount = new Decimal(row.accounting_amount || 0);
  if (measure === MEASURE_ACCOUNTING) {
    return [{ month: monthOf(formatDate(row.expense_date)), amount }];
  }
  const months = Number(row.amortization_months) || 1;
  const startMonth = formatMonth(row.amortization_start_month) || monthOf(formatDate(row.expense_date));
  const share = amount.div(months).toDecimalPlaces(AMOUNT_SCALE);
  const shares = [];
  for (let index = 0; index < months; index += 1) {
    shares.push({
      month: addMonth(startMonth, index),
      amount: index === months - 1 ? amount.minus(share.mul(months - 1)) : share,
    });
  }
  return shares;
}

function addAmount(counter, key, amount) {
  counter.set(key, (counter.get(key) || new Decimal(0)).plus(amount));
}

function monthTotalOf(rows, measure) {
  const totals = new Map();
  for (const row of rows) {
    for (const share of monthShares(row, measure)) {
      if (share.month) addAmount(totals, share.month, share.amount);
    }
  }
  return totals;
}

//月轴填满区间内的空月：那个月真的一笔没有，画成 0 才是实话，跳过去会把相隔半年的两根柱子并在一起
function monthAxis(months) {
  const sorted = [...months].sort();
  if (sorted.length === 0) return [];
  const last = sorted[sorted.length - 1];
  const axis = [];
  for (let month = sorted[0]; month <= last; month = addMonth(month, 1)) {
    axis.push(month);
    if (axis.length > MONTH_AXIS_MAX) return sorted;
  }
  return axis;
}

//支出类型与交易对手方的合计与口径无关：一笔明细各月份额之和恒等于它的记账金额
function aggregate(rows, measure) {
  const monthType = new Map();
  const monthTotal = new Map();
  const typeTotal = new Map();
  const counterpartyTotal = new Map();
  let total = new Decimal(0);
  let largest = new Decimal(0);
  for (const row of rows) {
    const amount = new Decimal(row.accounting_amount || 0);
    total = total.plus(amount);
    if (amount.abs().gt(largest.abs())) largest = amount;
    addAmount(typeTotal, row.expense_type || UNFILLED, amount);
    addAmount(counterpartyTotal, row.counterparty || UNFILLED, amount);
    for (const share of monthShares(row, measure)) {
      if (!share.month) continue;
      if (!monthType.has(share.month)) monthType.set(share.month, new Map());
      addAmount(monthType.get(share.month), row.expense_type || UNFILLED, share.amount);
      addAmount(monthTotal, share.month, share.amount);
    }
  }
  //类型按合计从大到小排，堆叠柱里大头永远在同一层，几张图之间颜色也对得上
  const types = [...typeTotal.keys()].sort((left, right) => typeTotal.get(right).cmp(typeTotal.get(left)));
  return {
    months: monthAxis(monthType.keys()),
    types,
    monthType,
    monthTotal,
    typeTotal,
    counterpartyTotal,
    total,
    largest,
  };
}

function cellAmount(summary, month, type) {
  const types = summary.monthType.get(month);
  return (types && types.get(type)) || new Decimal(0);
}

function monthTotal(summary, month) {
  return summary.monthTotal.get(month) || new Decimal(0);
}

function percentText(value, total) {
  if (total.isZero()) return '—';
  return `${value.div(total).mul(100).toDecimalPlaces(1).toString()}%`;
}

// ===== 渲染 =====

function measureName() {
  return (MEASURES.find((measure) => measure.value === state.measure) || MEASURES[0]).name;
}

function buildFilter() {
  return expenseFilter(
    state.inquiry,
    (inquiry) => {
      state.inquiry = inquiry;
      reload();
    },
    () => {
      state.inquiry = newInquiry();
      render(host, {});
    },
  );
}

//记账金额是按各自的记账币种存的，混着相加就不是钱了，这条提示比任何一张图都重要
function buildCurrencyHint() {
  const codes = [...new Set(state.rows.map((row) => row.accounting_currency).filter(Boolean))].sort();
  if (codes.length <= 1) return null;
  return el('div', { class: 'alert alert-warning py-2 small mb-3' }, [
    `当前统计范围内有 ${codes.length} 种记账币种：${codes.join('、')}。图表把它们直接相加，金额已经失真。请在筛选里限定记账币种，或先去明细页做一次记账币种切换。`,
  ]);
}

function buildOverview(summary) {
  const monthCount = summary.months.length;
  const tiles = [
    { name: '明细笔数', value: `${state.rows.length}` },
    { name: '合计记账金额', value: formatAmount(summary.total) },
    { name: `覆盖月份（${measureName()}口径）`, value: `${monthCount}` },
    { name: '月均', value: monthCount === 0 ? '—' : formatAmount(summary.total.div(monthCount).toDecimalPlaces(AMOUNT_SCALE)) },
    { name: '最大单笔', value: formatAmount(summary.largest) },
  ];
  return el('div', { class: 'row g-2 mb-3' }, tiles.map((tile) => el('div', { class: 'col-6 col-lg' }, [
    el('div', { class: 'card h-100' }, [
      el('div', { class: 'card-body py-2 px-3' }, [
        el('div', { class: 'text-secondary small', text: tile.name }),
        el('div', { class: 'fs-6 fw-semibold text-nowrap', text: tile.value }),
      ]),
    ]),
  ])));
}

function measureSelect() {
  const control = select(MEASURES, state.measure);
  control.addEventListener('change', () => {
    state.measure = control.value;
    renderBody();
  });
  return el('div', { class: 'd-flex align-items-center gap-2' }, [
    el('span', { class: 'small text-secondary text-nowrap', text: '柱高口径' }),
    control,
  ]);
}

function buildMonthTypeChart(summary) {
  return chartCard({
    title: `每月支出 × 支出类型（${measureName()}）`,
    description: '每根柱子是一个月，按支出类型分段。段上标着该段金额与它占当月合计的比例，柱顶是当月合计；段太薄放不下时标签会自动隐去，完整数据看下面那张表。',
    header: measureSelect(),
    height: '26rem',
    config: stackedBar({
      labels: summary.months,
      datasets: summary.types.map((type) => ({
        label: type,
        data: summary.months.map((month) => cellAmount(summary, month, type).toNumber()),
      })),
      //柱顶合计用聚合时就算准的 Decimal，不让图表再拿浮点把各段加一遍
      totals: summary.months.map((month) => monthTotal(summary, month).toNumber()),
      format: formatAmount,
    }),
  });
}

//堆叠柱上放不下的标签在这里全都读得到，默认收起，免得把图表挤到屏幕外
function buildMonthTypeTable(summary) {
  const id = `statistic-detail-${Math.random().toString(36).slice(2, 8)}`;
  const head = el('thead', {}, [
    el('tr', {}, [
      el('th', { class: 'text-nowrap', text: '月份' }),
      ...summary.types.map((type) => el('th', { class: 'text-nowrap text-end', text: type })),
      el('th', { class: 'text-nowrap text-end', text: '合计' }),
    ]),
  ]);
  const body = el('tbody');
  if (summary.months.length === 0) {
    body.appendChild(emptyRow(summary.types.length + 2));
  }
  for (const month of summary.months) {
    const total = monthTotal(summary, month);
    body.appendChild(el('tr', {}, [
      el('td', { class: 'text-nowrap', text: month }),
      ...summary.types.map((type) => {
        const amount = cellAmount(summary, month, type);
        return el('td', { class: 'text-end text-nowrap' }, [
          amount.isZero()
            ? el('span', { class: 'text-secondary', text: '—' })
            : el('span', {}, [formatAmount(amount), el('span', { class: 'text-secondary ms-1', text: percentText(amount, total) })]),
        ]);
      }),
      el('td', { class: 'text-end text-nowrap fw-semibold', text: formatAmount(total) }),
    ]));
  }
  return el('div', { class: 'card mb-3' }, [
    el('div', { class: 'card-header py-2 d-flex align-items-center' }, [
      el('span', { class: 'small fw-semibold', text: `逐月逐类型明细（${measureName()}）` }),
      el('button', {
        class: 'btn btn-sm btn-link ms-auto p-0 text-decoration-none',
        type: 'button',
        'data-bs-toggle': 'collapse',
        'data-bs-target': `#${id}`,
        text: '展开 / 收起',
      }),
    ]),
    el('div', { class: 'collapse', id }, [
      el('div', { class: 'card-body py-2' }, [
        el('div', { class: 'table-responsive' }, [
          el('table', { class: 'table table-sm table-hover align-middle expense-table mb-0' }, [head, body]),
        ]),
      ]),
    ]),
  ]);
}

function buildTypeShareChart(summary) {
  return chartCard({
    title: '支出类型占比',
    description: '筛选范围内按支出类型汇总的记账金额。与口径无关：一笔明细各月份额之和就是它的记账金额。',
    config: doughnut({
      labels: summary.types,
      values: summary.types.map((type) => summary.typeTotal.get(type).toNumber()),
      total: summary.total.toNumber(),
      format: formatAmount,
    }),
  });
}

function buildMeasureCompareChart() {
  const accounting = monthTotalOf(state.rows, MEASURE_ACCOUNTING);
  const amortization = monthTotalOf(state.rows, MEASURE_AMORTIZATION);
  const months = monthAxis([...accounting.keys(), ...amortization.keys()]);
  const pick = (totals, month) => (totals.get(month) || new Decimal(0)).toNumber();
  return chartCard({
    title: '记账口径 vs 摊销口径',
    description: '同一批明细两种口径的月度合计。记账口径把整笔算在支出当月，摊销口径把它摊到摊销起止月，两条线的差就是摊销削平的那部分。',
    config: line({
      labels: months,
      datasets: [
        { label: '记账金额', data: months.map((month) => pick(accounting, month)) },
        { label: '摊销金额', data: months.map((month) => pick(amortization, month)) },
      ],
      format: formatAmount,
    }),
  });
}

function buildCounterpartyChart(summary) {
  const ranked = [...summary.counterpartyTotal.entries()]
    .sort((left, right) => right[1].cmp(left[1]))
    .slice(0, TOP_COUNT);
  return chartCard({
    title: `交易对手方 Top ${TOP_COUNT}`,
    description: '筛选范围内记账金额最高的交易对手方。与口径无关。',
    height: '24rem',
    config: horizontalBar({
      labels: ranked.map((item) => item[0]),
      values: ranked.map((item) => item[1].toNumber()),
      format: formatAmount,
    }),
  });
}

function renderBody() {
  if (!bodyHost) return;
  clear(bodyHost);
  if (state.rows.length === 0) {
    bodyHost.appendChild(el('div', { class: 'text-center text-secondary py-5', text: '当前筛选没有可统计的数据' }));
    mountChart();
    return;
  }
  const summary = aggregate(state.rows, state.measure);
  appendChildren(bodyHost, [
    buildCurrencyHint(),
    buildOverview(summary),
    buildMonthTypeChart(summary),
    buildMonthTypeTable(summary),
    el('div', { class: 'row g-3' }, [
      el('div', { class: 'col-12 col-xl-6' }, [buildTypeShareChart(summary)]),
      el('div', { class: 'col-12 col-xl-6' }, [buildMeasureCompareChart()]),
    ]),
    buildCounterpartyChart(summary),
    el('p', { class: 'small text-secondary' }, [
      '统计全部在前端算，后端只按筛选条件返回原始明细。已删除的明细是否计入，跟着筛选里的「已删除」走。',
    ]),
  ]);
  //节点进了文档才量得到容器尺寸，图表实例统一在这一步建
  mountChart();
}

async function reload() {
  if (!bodyHost) return;
  clear(bodyHost).appendChild(el('div', { class: 'text-center text-secondary py-5' }, [
    el('span', { class: 'spinner-border spinner-border-sm me-2' }),
    '加载中',
  ]));
  try {
    state.rows = await api.selectExpense(state.inquiry).then((result) => result.object || []);
    await loadCandidate();
  } catch (err) {
    state.rows = [];
    toastErr(err);
  }
  renderBody();
}

export function render(container, query) {
  host = container;
  bodyHost = el('div');
  clear(container).appendChild(el('div', {}, [
    el('h5', { class: 'mb-3', text: '金额统计' }),
    buildFilter(),
    bodyHost,
  ]));
  reload();
}
