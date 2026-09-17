import test from 'node:test';
import './helper/lib.js';
import { equal, ok, same } from './helper/check.js';
import {
  MEASURE_ACCOUNTING,
  MEASURE_AMORTIZATION,
  unfilled,
  aggregate,
  cellAmount,
  monthAxis,
  monthShares,
  monthTotal,
  monthTotalOf,
  percentText,
} from '../static/js/expense_statistic.js';
import { dateToRfc3339 } from '../static/js/util.js';

//造一行明细：统计只认记账金额、支出日期、摊销三字段、支出类型与对手方
function newRow(seed) {
  const startMonth = seed.start_month || (seed.expense_date || '2026-09-15').slice(0, 7);
  return {
    expense_date: dateToRfc3339(seed.expense_date || '2026-09-15'),
    accounting_amount: seed.amount,
    accounting_currency: seed.currency || 'CNY',
    amortization_months: seed.months || 1,
    amortization_start_month: dateToRfc3339(`${startMonth}-01`),
    expense_type: seed.type === undefined ? '餐饮' : seed.type,
    counterparty: seed.counterparty === undefined ? '盒马鲜生' : seed.counterparty,
  };
}

function shareTexts(row, measure) {
  return monthShares(row, measure).map((share) => `${share.month}=${share.amount.toString()}`);
}

test('记账口径：整笔落在支出日期所属月', () => {
  const shares = monthShares(newRow({ expense_date: '2026-09-15', amount: '360.00', months: 12 }), MEASURE_ACCOUNTING);
  equal('份额数', shares.length, 1);
  same('月份与金额', shareTexts(newRow({ expense_date: '2026-09-15', amount: '360.00', months: 12 }), MEASURE_ACCOUNTING), ['2026-09=360']);
});

test('摊销口径：整除时逐月等额，跨年按自然月推', () => {
  same('四个月均摊', shareTexts(newRow({ expense_date: '2026-09-15', amount: '400.00', months: 4 }), MEASURE_AMORTIZATION), [
    '2026-09=100', '2026-10=100', '2026-11=100', '2026-12=100',
  ]);
  same('跨年', shareTexts(newRow({ expense_date: '2026-11-20', amount: '400.00', months: 4 }), MEASURE_AMORTIZATION), [
    '2026-11=100', '2026-12=100', '2027-01=100', '2027-02=100',
  ]);
  same('一个月即整笔', shareTexts(newRow({ expense_date: '2026-09-15', amount: '128.50', months: 1 }), MEASURE_AMORTIZATION), ['2026-09=128.5']);
});

//除不尽时逐月四舍五入，各月之和会对不上原始金额，所以尾差一次性落在最后一个月
test('摊销口径：除不尽的余数落在最后一个月', () => {
  same('100÷3', shareTexts(newRow({ amount: '100.00', months: 3 }), MEASURE_AMORTIZATION), [
    '2026-09=33.33', '2026-10=33.33', '2026-11=33.34',
  ]);
  same('100÷6', shareTexts(newRow({ amount: '100.00', months: 6 }), MEASURE_AMORTIZATION), [
    '2026-09=16.67', '2026-10=16.67', '2026-11=16.67', '2026-12=16.67', '2027-01=16.67', '2027-02=16.65',
  ]);
  same('分币÷3', shareTexts(newRow({ amount: '0.01', months: 3 }), MEASURE_AMORTIZATION), [
    '2026-09=0', '2026-10=0', '2026-11=0.01',
  ]);
  same('退款负数÷3', shareTexts(newRow({ amount: '-100.00', months: 3 }), MEASURE_AMORTIZATION), [
    '2026-09=-33.33', '2026-10=-33.33', '2026-11=-33.34',
  ]);
});

//这条是摊销口径的不变式：怎么摊都不许把钱摊多或摊少，且份额不能长出浮点尾巴
test('摊销口径不变式：各月之和恒等于记账金额，份额不超过金额精度', () => {
  const amounts = ['0.01', '0.05', '1.00', '7.77', '-7.77', '128.50', '999999.99', '33.33'];
  let rounds = 0;
  for (const amount of amounts) {
    for (let months = 1; months <= 120; months += 1) {
      const shares = monthShares(newRow({ amount, months }), MEASURE_AMORTIZATION);
      equal(`份额数 ${amount}÷${months}`, shares.length, months);
      let sum = new Decimal(0);
      for (const share of shares) {
        sum = sum.plus(share.amount);
        ok(`小数位不超过 2 ${amount}÷${months}=${share.amount.toString()}`, share.amount.decimalPlaces() <= 2);
      }
      equal(`合计恒等 ${amount}÷${months}`, sum.toString(), new Decimal(amount).toString());
      if (months > 1) {
        equal(`前 N-1 月等额 ${amount}÷${months}`, shares[0].amount.toString(), shares[months - 2].amount.toString());
      }
      rounds += 1;
    }
  }
  equal('穷举组数', rounds, amounts.length * 120);
});

test('摊销起始月缺失时退回支出日期所属月', () => {
  const row = newRow({ expense_date: '2026-07-20', amount: '300.00', months: 3 });
  row.amortization_start_month = '';
  same('起止月', shareTexts(row, MEASURE_AMORTIZATION), ['2026-07=100', '2026-08=100', '2026-09=100']);
});

test('摊销月数非法时按 1 个月处理，不产生空份额', () => {
  for (const months of [0, -3, null, undefined, '', 'abc']) {
    const shares = monthShares(newRow({ amount: '68.00', months }), MEASURE_AMORTIZATION);
    equal(`月数 ${String(months)}`, shares.length, 1);
    equal(`金额 ${String(months)}`, shares[0].amount.toString(), '68');
  }
});

test('月轴：区间内的空月补齐，超上限退回只画有数据的月', () => {
  same('补空月', monthAxis(['2026-09', '2026-12']), ['2026-09', '2026-10', '2026-11', '2026-12']);
  same('乱序先排序', monthAxis(['2026-12', '2026-10']), ['2026-10', '2026-11', '2026-12']);
  same('单点', monthAxis(['2026-09']), ['2026-09']);
  same('空集', monthAxis([]), []);
  same('跨度过大退回原样', monthAxis(['2000-01', '2026-01']), ['2000-01', '2026-01']);
  equal('刚好 120 个月仍补齐', monthAxis(['2026-01', '2035-12']).length, 120);
});

test('聚合：类型与对手方的合计与口径无关', () => {
  const rows = [
    newRow({ expense_date: '2026-09-15', amount: '360.00', months: 12, type: '数码' }),
    newRow({ expense_date: '2026-09-20', amount: '68.00', months: 1, type: '餐饮' }),
  ];
  const accounting = aggregate(rows, MEASURE_ACCOUNTING);
  const amortization = aggregate(rows, MEASURE_AMORTIZATION);
  equal('合计一致', accounting.total.toString(), amortization.total.toString());
  equal('合计值', accounting.total.toString(), '428');
  equal('数码合计一致', accounting.typeTotal.get('数码').toString(), amortization.typeTotal.get('数码').toString());
  equal('对手方合计一致', accounting.counterpartyTotal.get('盒马鲜生').toString(), amortization.counterpartyTotal.get('盒马鲜生').toString());
  equal('记账口径只占一个月', accounting.months.length, 1);
  equal('摊销口径铺满 12 个月', amortization.months.length, 12);
});

test('聚合：月合计之和恒等于总额，逐月与 monthTotalOf 一致', () => {
  const rows = [
    newRow({ expense_date: '2026-07-03', amount: '100.00', months: 3, type: '居住' }),
    newRow({ expense_date: '2026-08-14', amount: '99.90', months: 1, type: '数码' }),
    newRow({ expense_date: '2026-09-28', amount: '-120.00', months: 6, type: '日用' }),
  ];
  for (const measure of [MEASURE_ACCOUNTING, MEASURE_AMORTIZATION]) {
    const summary = aggregate(rows, measure);
    let sum = new Decimal(0);
    for (const month of summary.months) sum = sum.plus(monthTotal(summary, month));
    equal(`月合计求和 ${measure}`, sum.toString(), summary.total.toString());
    const totals = monthTotalOf(rows, measure);
    for (const month of summary.months) {
      equal(`逐月一致 ${measure} ${month}`, monthTotal(summary, month).toString(), (totals.get(month) || new Decimal(0)).toString());
    }
    let typeSum = new Decimal(0);
    for (const month of summary.months) {
      for (const type of summary.types) typeSum = typeSum.plus(cellAmount(summary, month, type));
    }
    equal(`逐月逐类型求和 ${measure}`, typeSum.toString(), summary.total.toString());
  }
});

//摊销口径按「摊销区间与窗口有交集」拉数，跨进窗口的那几笔在窗口外还留着半截，
//窗口外那部分既不能上月轴，也不能进概览与占比，否则整屏数字互相对不上
test('聚合：给了月窗，只算摊进窗口的那几个月', () => {
  const rows = [
    //2026-07 起摊 6 个月，摊到 2026-12；窗口只取 2026-09~2026-11，算进来的是 3 个月
    newRow({ expense_date: '2026-07-10', amount: '600.00', months: 6, type: '数码' }),
    //整笔都落在窗口里
    newRow({ expense_date: '2026-10-08', amount: '68.00', months: 1, type: '餐饮' }),
    //摊销区间完全在窗口之后，一分钱都不该算
    newRow({ expense_date: '2027-01-05', amount: '999.00', months: 1, type: '居住' }),
  ];
  const window = { start: '2026-09', end: '2026-11' };
  const summary = aggregate(rows, MEASURE_AMORTIZATION, window);
  same('月轴不越界', summary.months, ['2026-09', '2026-10', '2026-11']);
  equal('只算摊进窗口的 3 个月', summary.monthTotal.get('2026-09').toString(), '100');
  equal('合计等于窗口内份额之和', summary.total.toString(), '368');
  equal('类型合计只算摊进来的部分', summary.typeTotal.get('数码').toString(), '300');
  equal('窗口之外的类型整个不出现', summary.typeTotal.has('居住'), false);
  equal('对手方合计同样只算窗口内', summary.counterpartyTotal.get('盒马鲜生').toString(), '368');
  equal('最大单笔取窗口内份额', summary.largest.toString(), '300');

  let sum = new Decimal(0);
  for (const month of summary.months) sum = sum.plus(monthTotal(summary, month));
  equal('月合计求和恒等于总额', sum.toString(), summary.total.toString());
});

test('聚合：不给月窗时与改动前逐字一致', () => {
  const rows = [
    newRow({ expense_date: '2026-07-10', amount: '600.00', months: 6, type: '数码' }),
    newRow({ expense_date: '2026-10-08', amount: '68.00', months: 1, type: '餐饮' }),
  ];
  for (const measure of [MEASURE_ACCOUNTING, MEASURE_AMORTIZATION]) {
    const summary = aggregate(rows, measure);
    equal(`合计仍是整笔记账金额 ${measure}`, summary.total.toString(), '668');
    equal(`类型合计仍是整笔 ${measure}`, summary.typeTotal.get('数码').toString(), '600');
    equal(`对手方合计仍是整笔 ${measure}`, summary.counterpartyTotal.get('盒马鲜生').toString(), '668');
  }
});

test('月度合计：月窗同样把窗口外的份额挡在外面', () => {
  const rows = [newRow({ expense_date: '2026-07-10', amount: '600.00', months: 6 })];
  const all = monthTotalOf(rows, MEASURE_AMORTIZATION);
  equal('不给窗口铺满 6 个月', all.size, 6);
  const clipped = monthTotalOf(rows, MEASURE_AMORTIZATION, { start: '2026-09', end: '2026-11' });
  same('给了窗口只剩 3 个月', [...clipped.keys()].sort(), ['2026-09', '2026-10', '2026-11']);
  const half = monthTotalOf(rows, MEASURE_AMORTIZATION, { start: '2026-10' });
  same('只给起月就只卡一头', [...half.keys()].sort(), ['2026-10', '2026-11', '2026-12']);
});

test('聚合：空支出类型与空对手方归「未填写」，不丢数据', () => {
  const rows = [
    newRow({ amount: '10.00', type: '', counterparty: '' }),
    newRow({ amount: '20.00', type: '餐饮' }),
  ];
  const summary = aggregate(rows, MEASURE_AMORTIZATION);
  equal('未填写类型', summary.typeTotal.get(unfilled()).toString(), '10');
  equal('未填写对手方', summary.counterpartyTotal.get(unfilled()).toString(), '10');
  equal('总额不丢', summary.total.toString(), '30');
});

test('聚合：类型按合计从大到小排，最大单笔按绝对值取但保留符号', () => {
  const rows = [
    newRow({ amount: '10.00', type: '餐饮' }),
    newRow({ amount: '300.00', type: '数码' }),
    newRow({ amount: '-500.00', type: '日用' }),
  ];
  const summary = aggregate(rows, MEASURE_AMORTIZATION);
  same('类型顺序', summary.types, ['数码', '餐饮', '日用']);
  equal('最大单笔', summary.largest.toString(), '-500');
});

test('聚合：空行集给出空结果而不是异常', () => {
  const summary = aggregate([], MEASURE_AMORTIZATION);
  same('月轴', summary.months, []);
  same('类型', summary.types, []);
  equal('总额', summary.total.toString(), '0');
  equal('缺失格取 0', cellAmount(summary, '2026-09', '餐饮').toString(), '0');
  equal('缺失月取 0', monthTotal(summary, '2026-09').toString(), '0');
});

test('占比：零分母给破折号，负数占比照实显示', () => {
  equal('常规', percentText(new Decimal('33.33'), new Decimal('100')), '33.3%');
  equal('整占比', percentText(new Decimal('50'), new Decimal('100')), '50%');
  equal('零分母', percentText(new Decimal('10'), new Decimal('0')), '—');
  equal('负数', percentText(new Decimal('-20'), new Decimal('100')), '-20%');
  equal('零分子', percentText(new Decimal('0'), new Decimal('100')), '0%');
});
