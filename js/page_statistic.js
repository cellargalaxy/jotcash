import * as api from './api.js';
import { AMOUNT_SCALE } from './config.js';
import { chartCard, doughnut, horizontalBar, line, mountChart, stackedBar } from './chart.js';
import { emptyRow, select } from './component.js';
import { defaultInquiry, expenseFilter, loadCandidate } from './expense_inquiry.js';
import {
  MEASURES,
  MEASURE_ACCOUNTING,
  MEASURE_AMORTIZATION,
  aggregate,
  cellAmount,
  monthAxis,
  monthTotal,
  monthTotalOf,
  percentText,
} from './expense_statistic.js';
import { t } from './i18n.js';
import { appendChildren, clear, el, dateToRfc3339, formatAmount, formatDate, monthOf, toastErr } from './util.js';

const TOP_COUNT = 10;

//跨次渲染保留筛选条件与口径，切页面回来不用重新填一遍。
//与明细页各持一份：共享一份会让在统计页缩小范围把明细页的筛选也改掉
const state = {
  inquiry: defaultInquiry(),
  rows: [],
  //默认摊销口径：大额分期的钱本来就不是当月花掉的，记账口径会让那一个月凭空拱起一根柱子
  measure: MEASURE_AMORTIZATION,
};

let host = null;
let bodyHost = null;

// ===== 渲染 =====

//统计只看落在这个月窗里的钱，窗口边界取筛选里那对支出日期所在的月
function monthWindow() {
  return {
    start: monthOf(formatDate(state.inquiry.expense_date_start)),
    end: monthOf(formatDate(state.inquiry.expense_date_end)),
  };
}

//摊销口径下要的不是「支出日期落在窗口内」，而是「摊销区间与窗口有交集」：
//跨期分期的支出日期在窗口之前，它摊到窗口内那几个月的钱同样得算进来
function requestInquiry() {
  const window = monthWindow();
  if (state.measure !== MEASURE_AMORTIZATION || !window.start || !window.end) return state.inquiry;
  return {
    ...state.inquiry,
    expense_date_start: '',
    expense_date_end: '',
    amortization_month_start: dateToRfc3339(`${window.start}-01`),
    amortization_month_end: dateToRfc3339(`${window.end}-01`),
  };
}

function measureName() {
  return t((MEASURES.find((measure) => measure.value === state.measure) || MEASURES[0]).name);
}

function buildFilter() {
  return expenseFilter(
    state.inquiry,
    (inquiry) => {
      state.inquiry = inquiry;
      reload();
    },
    () => {
      state.inquiry = defaultInquiry();
      render(host, {});
    },
  );
}

//记账金额是按各自的记账币种存的，混着相加就不是钱了，这条提示比任何一张图都重要
function buildCurrencyHint() {
  const codes = [...new Set(state.rows.map((row) => row.accounting_currency).filter(Boolean))].sort();
  if (codes.length <= 1) return null;
  return el('div', { class: 'alert alert-warning py-2 small mb-3' }, [
    t('当前统计范围内有 {count} 种记账币种：{codes}。图表把它们直接相加，金额已经失真。请在筛选里限定记账币种，或先去明细页做一次记账币种切换。', { count: codes.length, codes: codes.join(t('、')) }),
  ]);
}

function buildOverview(summary) {
  const monthCount = summary.months.length;
  const tiles = [
    { name: t('明细笔数'), value: `${state.rows.length}` },
    { name: t('合计金额'), value: formatAmount(summary.total) },
    { name: t('覆盖月份（{measure}口径）', { measure: measureName() }), value: `${monthCount}` },
    { name: t('月均'), value: monthCount === 0 ? '—' : formatAmount(summary.total.div(monthCount).toDecimalPlaces(AMOUNT_SCALE)) },
    { name: t('最大单笔'), value: formatAmount(summary.largest) },
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
    //两种口径拉的不是同一批明细，换口径得重新回后端取
    reload();
  });
  return el('div', { class: 'd-flex align-items-center gap-2' }, [
    el('span', { class: 'small text-secondary text-nowrap', text: t('柱高口径') }),
    control,
  ]);
}

function buildMonthTypeChart(summary) {
  return chartCard({
    title: t('每月支出 × 支出类型（{measure}）', { measure: measureName() }),
    description: t('每根柱子是一个月，按支出类型分段。段上标着该段金额与它占当月合计的比例，柱顶是当月合计；段太薄放不下时标签会自动隐去，完整数据看下面那张表。'),
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
      el('th', { class: 'text-nowrap', text: t('月份') }),
      ...summary.types.map((type) => el('th', { class: 'text-nowrap text-end', text: type })),
      el('th', { class: 'text-nowrap text-end', text: t('合计') }),
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
      el('span', { class: 'small fw-semibold', text: t('逐月逐类型明细（{measure}）', { measure: measureName() }) }),
      el('button', {
        class: 'btn btn-sm btn-link ms-auto p-0 text-decoration-none',
        type: 'button',
        'data-bs-toggle': 'collapse',
        'data-bs-target': `#${id}`,
        text: t('展开 / 收起'),
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
    title: t('支出类型占比'),
    description: t('筛选范围内按支出类型汇总的金额，跟着柱高口径走：摊销口径下算的是摊进区间那几个月的份额，不是整笔记账金额。'),
    config: doughnut({
      labels: summary.types,
      values: summary.types.map((type) => summary.typeTotal.get(type).toNumber()),
      total: summary.total.toNumber(),
      format: formatAmount,
    }),
  });
}

function buildMeasureCompareChart() {
  const window = monthWindow();
  const accounting = monthTotalOf(state.rows, MEASURE_ACCOUNTING, window);
  const amortization = monthTotalOf(state.rows, MEASURE_AMORTIZATION, window);
  const months = monthAxis([...accounting.keys(), ...amortization.keys()]);
  const pick = (totals, month) => (totals.get(month) || new Decimal(0)).toNumber();
  return chartCard({
    title: t('记账口径 vs 摊销口径'),
    description: t('同一批明细两种口径的月度合计，都只画筛选区间内的月份。记账口径把整笔算在支出当月，摊销口径把它摊到摊销起止月，两条线的差就是摊销削平的那部分。'),
    config: line({
      labels: months,
      datasets: [
        { label: t('记账金额'), data: months.map((month) => pick(accounting, month)) },
        { label: t('摊销金额'), data: months.map((month) => pick(amortization, month)) },
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
    title: t('交易对手方 Top {count}', { count: TOP_COUNT }),
    description: t('筛选范围内金额最高的交易对手方，口径与上面几张图一致。'),
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
    bodyHost.appendChild(el('div', { class: 'text-center text-secondary py-5', text: t('当前筛选没有可统计的数据') }));
    mountChart();
    return;
  }
  const summary = aggregate(state.rows, state.measure, monthWindow());
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
      t('统计全部在前端算，后端只按筛选条件返回原始明细。摊销口径会把支出日期早于区间、但摊销跨进区间的明细一并取回，只统计摊进区间那几个月的份额。已删除的明细是否计入，跟着筛选里的「已删除」走。'),
    ]),
  ]);
  //节点进了文档才量得到容器尺寸，图表实例统一在这一步建
  mountChart();
}

async function reload() {
  if (!bodyHost) return;
  clear(bodyHost).appendChild(el('div', { class: 'text-center text-secondary py-5' }, [
    el('span', { class: 'spinner-border spinner-border-sm me-2' }),
    t('加载中'),
  ]));
  try {
    state.rows = await api.selectExpense(requestInquiry()).then((result) => result.object || []);
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
    el('h5', { class: 'mb-3', text: t('金额统计') }),
    buildFilter(),
    bodyHost,
  ]));
  reload();
}
