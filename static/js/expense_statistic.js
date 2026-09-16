import { AMOUNT_SCALE } from './config.js';
import { t } from './i18n.js';
import { addMonth, formatDate, formatMonth, monthOf } from './util.js';

export const MEASURE_ACCOUNTING = 'accounting';
export const MEASURE_AMORTIZATION = 'amortization';

export const MEASURES = [
  { value: MEASURE_ACCOUNTING, name: '记账金额' },
  { value: MEASURE_AMORTIZATION, name: '摊销金额' },
];

//支出类型、交易对手方留空的行不能丢掉，归到这一档里。
//写成函数而不是常量：这一档是我们造的分类名，得跟着语言走，而常量在模块求值时就定死了
export function unfilled() {
  return t('未填写');
}

//连续月轴的长度上限。摊销月数没有上限，真填了个离谱的值月轴会长到画不出来，超了就退回只画有数据的月份
const MONTH_AXIS_MAX = 120;

//一笔明细在各月的份额。记账口径整笔落在支出日期所属月；摊销口径按摊销月数均摊到摊销起止月，
//尾差落在最后一个月——除不尽时逐月四舍五入，各月之和会对不上原始记账金额
export function monthShares(row, measure) {
  const amount = new Decimal(row.accounting_amount || 0);
  if (measure === MEASURE_ACCOUNTING) {
    return [{ month: monthOf(formatDate(row.expense_date)), amount }];
  }
  //与后端 fillExpense 同一个判据：小于 1 一律按 1。不能写成 Number(x) || 1，
  //负数是真值会漏进来，循环一次都不跑，这一笔的钱就从所有图里整个消失了
  const count = Math.floor(Number(row.amortization_months));
  const months = count >= 1 ? count : 1;
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

export function monthTotalOf(rows, measure) {
  const totals = new Map();
  for (const row of rows) {
    for (const share of monthShares(row, measure)) {
      if (share.month) addAmount(totals, share.month, share.amount);
    }
  }
  return totals;
}

//月轴填满区间内的空月：那个月真的一笔没有，画成 0 才是实话，跳过去会把相隔半年的两根柱子并在一起
export function monthAxis(months) {
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
export function aggregate(rows, measure) {
  const monthType = new Map();
  const monthTotal = new Map();
  const typeTotal = new Map();
  const counterpartyTotal = new Map();
  let total = new Decimal(0);
  let largest = new Decimal(0);
  const unfilledName = unfilled();
  for (const row of rows) {
    const amount = new Decimal(row.accounting_amount || 0);
    total = total.plus(amount);
    if (amount.abs().gt(largest.abs())) largest = amount;
    addAmount(typeTotal, row.expense_type || unfilledName, amount);
    addAmount(counterpartyTotal, row.counterparty || unfilledName, amount);
    for (const share of monthShares(row, measure)) {
      if (!share.month) continue;
      if (!monthType.has(share.month)) monthType.set(share.month, new Map());
      addAmount(monthType.get(share.month), row.expense_type || unfilledName, share.amount);
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

export function cellAmount(summary, month, type) {
  const types = summary.monthType.get(month);
  return (types && types.get(type)) || new Decimal(0);
}

export function monthTotal(summary, month) {
  return summary.monthTotal.get(month) || new Decimal(0);
}

export function percentText(value, total) {
  if (total.isZero()) return '—';
  return `${value.div(total).mul(100).toDecimalPlaces(1).toString()}%`;
}
