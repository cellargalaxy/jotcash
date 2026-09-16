import test from 'node:test';
import './helper/lib.js';
import { equal, ok, rejects, same } from './helper/check.js';
import { CSV_FIELDS, DELETED_ALL, DELETED_NO, DELETED_ONLY } from '../static/js/config.js';
import * as mock from '../static/js/mock.js';
import { formatDate, formatMonth } from '../static/js/util.js';

//辅助函数：按契约列名与列顺序拼一份 CSV，缺的列留空
function csvOf(rows) {
  const lines = [CSV_FIELDS.map((field) => field.column).join(',')];
  for (const row of rows) {
    lines.push(CSV_FIELDS.map((field) => (row[field.key] === undefined ? '' : String(row[field.key]))).join(','));
  }
  return lines.join('\r\n');
}

//辅助函数：入库一批并把这一批查回来，按明细ID 升序
async function insertAndSelect(rows, accountingCurrency) {
  const inserted = await mock.insertExpense('用例.csv', csvOf(rows), accountingCurrency || 'CNY');
  const selected = await mock.selectExpense({ operation_id: [inserted.object], deleted: DELETED_ALL, sort: 'id asc' });
  return { operationId: inserted.object, count: inserted.count, rows: selected.object, total: selected.count };
}

test('入库往返：按契约列名拼的 CSV 能原样查回，派生字段与后端同一套算法', async () => {
  const result = await insertAndSelect([{
    bank_name: '招商银行',
    card_last_4: '8821',
    expense_date: '2026-09-15',
    expense_currency: 'CNY',
    expense_amount: '128.50',
    counterparty: '盒马鲜生',
    remark: '晚饭',
    expense_type: '餐饮',
    amortization_months: '3',
  }]);
  equal('入库笔数', result.count, 1);
  const row = result.rows[0];
  equal('银行名称', row.bank_name, '招商银行');
  equal('卡号后四位', row.card_last_4, '8821');
  equal('支出日期', formatDate(row.expense_date), '2026-09-15');
  equal('支出金额', row.expense_amount, '128.50');
  equal('交易对手方', row.counterparty, '盒马鲜生');
  equal('交易备注', row.remark, '晚饭');
  equal('支出类型', row.expense_type, '餐饮');
  equal('同币种汇率恒为 1', row.exchange_rate, '1');
  equal('记账币种取请求携带的口径', row.accounting_currency, 'CNY');
  equal('记账金额 = 支出金额 × 汇率', row.accounting_amount, '128.5');
  equal('摊销月数', row.amortization_months, 3);
  equal('摊销起始月 = 支出日期所属月', formatMonth(row.amortization_start_month), '2026-09');
  equal('摊销结束月 = 起始月 + 月数 - 1', formatMonth(row.amortization_end_month), '2026-11');
  equal('版本号从 1 起', row.version, 1);
  equal('未删除', row.deleted_at, null);
});

test('入库：跨币种按汇率折算，摊销月数留空按 1 处理', async () => {
  const result = await insertAndSelect([{
    expense_date: '2026-09-15',
    expense_currency: 'USD',
    expense_amount: '100',
    counterparty: 'Apple Store',
  }]);
  const row = result.rows[0];
  equal('汇率', row.exchange_rate, '7.12');
  equal('记账金额', row.accounting_amount, '712');
  equal('摊销月数默认 1', row.amortization_months, 1);
  equal('起止月同月', formatMonth(row.amortization_start_month), formatMonth(row.amortization_end_month));
});

test('入库：表头差一个字就没有解析器认领', async () => {
  const good = csvOf([{ expense_date: '2026-09-15', expense_currency: 'CNY', expense_amount: '1' }]);
  await rejects('旧表头', mock.insertExpense('旧.csv', good.replace('摊销月数', '摊分月数'), 'CNY'), '文件格式无法识别');
  await rejects('少一列', mock.insertExpense('少.csv', good.replace(',摊销月数', ''), 'CNY'), '文件格式无法识别');
  await rejects('顺序调换', mock.insertExpense('乱.csv', good.replace('银行名称,卡号后四位', '卡号后四位,银行名称'), 'CNY'), '文件格式无法识别');
  await rejects('只有表头', mock.insertExpense('空.csv', csvOf([]), 'CNY'), '入库内容为空');
});

test('入库：逐行校验的异常分支都报到具体行号', async () => {
  const bad = (row) => mock.insertExpense('坏.csv', csvOf([row]), 'CNY');
  await rejects('日期非法', bad({ expense_date: '2026/09/15', expense_currency: 'CNY', expense_amount: '1' }), '支出日期非法');
  await rejects('金额非法', bad({ expense_date: '2026-09-15', expense_currency: 'CNY', expense_amount: '一百' }), '支出金额非法');
  await rejects('摊销月数为 0', bad({ expense_date: '2026-09-15', expense_currency: 'CNY', expense_amount: '1', amortization_months: '0' }), '摊销月数非法');
  await rejects('摊销月数带小数', bad({ expense_date: '2026-09-15', expense_currency: 'CNY', expense_amount: '1', amortization_months: '2.5' }), '摊销月数非法');
  await rejects('填了汇率没填记账币种', bad({ expense_date: '2026-09-15', expense_currency: 'USD', expense_amount: '1', exchange_rate: '7' }), '填了折算汇率就必须填记账币种');
  await rejects('币种非法', bad({ expense_date: '2026-09-15', expense_currency: 'cn', expense_amount: '1' }), '不在枚举内');
  await rejects('取不到汇率', bad({ expense_date: '2026-09-15', expense_currency: 'MOP', expense_amount: '1' }), '取不到');
});

//分页参数由调用方决定：不传就是全量，图表统计与导出要的正是这个语义
test('分页：非正即不限，count 始终是筛选全集', async () => {
  const rows = [];
  for (let index = 0; index < 25; index += 1) {
    rows.push({ expense_date: '2026-06-10', expense_currency: 'CNY', expense_amount: '1', counterparty: '分页用例', remark: `第${index}行` });
  }
  const inserted = await mock.insertExpense('分页.csv', csvOf(rows), 'CNY');
  const inquiry = { operation_id: [inserted.object], sort: 'id asc' };

  const all = await mock.selectExpense({ ...inquiry });
  equal('不传分页即全量', all.object.length, 25);
  equal('全量的 count', all.count, 25);

  const first = await mock.selectExpense({ ...inquiry, page: 1, page_size: 10 });
  equal('第 1 页条数', first.object.length, 10);
  equal('分页后 count 仍是全集', first.count, 25);
  equal('第 1 页首行', first.object[0].remark, '第0行');

  const third = await mock.selectExpense({ ...inquiry, page: 3, page_size: 10 });
  equal('末页只剩 5 条', third.object.length, 5);
  equal('第 3 页首行', third.object[0].remark, '第20行');

  const zero = await mock.selectExpense({ ...inquiry, page: 1, page_size: 0 });
  equal('每页 0 条等于不限', zero.object.length, 25);
  const negative = await mock.selectExpense({ ...inquiry, page: 0, page_size: -1 });
  equal('负数等于不限', negative.object.length, 25);
});

test('筛选：集合、模糊、金额区间、时间区间各自生效', async () => {
  const inserted = await insertAndSelect([
    { expense_date: '2026-05-01', expense_currency: 'CNY', expense_amount: '10', counterparty: '筛选甲', expense_type: '餐饮', bank_name: '筛选行' },
    { expense_date: '2026-05-20', expense_currency: 'CNY', expense_amount: '200', counterparty: '筛选乙', expense_type: '数码', bank_name: '筛选行' },
    { expense_date: '2026-06-01', expense_currency: 'USD', expense_amount: '30', counterparty: '筛选丙', expense_type: '餐饮', bank_name: '筛选行' },
    { expense_date: '2026-06-15', expense_currency: 'CNY', expense_amount: '40', counterparty: '筛选丁', expense_type: '', bank_name: '筛选行' },
  ]);
  const base = { operation_id: [inserted.operationId], sort: 'id asc' };
  const count = async (extra) => (await mock.selectExpense({ ...base, ...extra })).count;

  equal('按币种集合', await count({ expense_currency: ['USD'] }), 1);
  equal('按类型集合', await count({ expense_type: ['餐饮'] }), 2);
  equal('支出类型为空标记', await count({ expense_type_empty: true }), 1);
  equal('支出类型为空切片', await count({ expense_type: [''] }), 1);
  equal('按银行名称', await count({ bank_name: ['筛选行'] }), 4);
  equal('对手方模糊', await count({ counterparty_like: '筛选' }), 4);
  equal('对手方模糊到单条', await count({ counterparty_like: '乙' }), 1);
  equal('类型模糊', await count({ expense_type_like: '数' }), 1);
  equal('金额下限', await count({ expense_amount_min: '30' }), 3);
  equal('金额上限', await count({ expense_amount_max: '30' }), 2);
  equal('金额区间', await count({ expense_amount_min: '20', expense_amount_max: '100' }), 2);
  equal('日期区间', await count({ expense_date_start: '2026-05-10T00:00:00+08:00', expense_date_end: '2026-06-30T23:59:59+08:00' }), 3);
  const resAsc = await mock.selectExpense({ ...base, sort: 'updated_at asc' });
  equal('更新时间升序数量', resAsc.count, 4);
  const resDesc = await mock.selectExpense({ ...base, sort: 'updated_at desc' });
  equal('更新时间降序数量', resDesc.count, 4);
  await rejects('时间区间倒挂', mock.selectExpense({ ...base, expense_date_start: '2026-06-01T00:00:00+08:00', expense_date_end: '2026-05-01T00:00:00+08:00' }), '时间区间倒挂');
  await rejects('金额区间倒挂', mock.selectExpense({ ...base, expense_amount_min: '100', expense_amount_max: '10' }), '金额区间倒挂');
  await rejects('删除筛选非法', mock.selectExpense({ ...base, deleted: 9 }), '删除筛选非法');
});

test('编辑：乐观锁挡住落后的版本，审计记下前后值', async () => {
  const inserted = await insertAndSelect([
    { expense_date: '2026-04-01', expense_currency: 'CNY', expense_amount: '50', counterparty: '编辑用例', expense_type: '日用' },
  ]);
  const row = inserted.rows[0];

  const updated = await mock.updateExpense({ ...row, expense_type: '数码', remark: '改过' });
  equal('版本号自增', updated.object.version, row.version + 1);
  equal('支出类型已改', updated.object.expense_type, '数码');
  equal('记账金额跟着重算', updated.object.accounting_amount, '50');

  await rejects('拿旧版本再存', mock.updateExpense({ ...row, expense_type: '餐饮' }), '数据已落后');
  await rejects('改不存在的明细', mock.updateExpense({ ...row, id: 1, version: 1 }), '明细不存在');

  const logs = await mock.selectOperationLog({ object_id: [row.id], sort: 'id desc' });
  equal('留下一条明细编辑审计', logs.count, 1);
  //与后端 model.ExpenseChanges 同形：前后两份整快照，逐字段比对由渲染层做
  const changes = JSON.parse(logs.object[0].changes);
  equal('变更内容记了前值', changes.before.expense_type, '日用');
  equal('变更内容记了后值', changes.after.expense_type, '数码');
  equal('前快照留的是改之前的版本号', changes.before.version, row.version);
  equal('后快照是自增之后的版本号', changes.after.version, row.version + 1);
});

test('编辑：改支出币种会重取汇率，已删除的明细不可编辑', async () => {
  const inserted = await insertAndSelect([
    { expense_date: '2026-04-02', expense_currency: 'CNY', expense_amount: '100', counterparty: '换币种用例' },
  ]);
  const row = inserted.rows[0];
  const updated = await mock.updateExpense({ ...row, expense_currency: 'USD' });
  equal('汇率重取', updated.object.exchange_rate, '7.12');
  equal('记账金额重算', updated.object.accounting_amount, '712');

  await mock.deleteExpense({ id: [row.id] });
  const deleted = await mock.selectExpense({ id: [row.id], deleted: DELETED_ONLY });
  await rejects('已删除不可编辑', mock.updateExpense({ ...deleted.object[0] }), '已删除明细不可编辑');
});

test('删除：只软删未删除的行，已删除的不被二次覆盖', async () => {
  const inserted = await insertAndSelect([
    { expense_date: '2026-03-01', expense_currency: 'CNY', expense_amount: '1', counterparty: '删除用例' },
    { expense_date: '2026-03-02', expense_currency: 'CNY', expense_amount: '2', counterparty: '删除用例' },
  ]);
  const base = { operation_id: [inserted.operationId] };
  const first = await mock.deleteExpense({ ...base, id: [inserted.rows[0].id] });
  equal('删了一笔', first.count, 1);

  const stamp = (await mock.selectExpense({ ...base, deleted: DELETED_ONLY })).object[0].deleted_at;
  const second = await mock.deleteExpense({ ...base });
  equal('再删只剩一笔可删', second.count, 1);
  const onlyDeleted = await mock.selectExpense({ ...base, deleted: DELETED_ONLY, sort: 'id asc' });
  equal('两笔都已删除', onlyDeleted.count, 2);
  equal('先删那笔的删除时间没被覆盖', onlyDeleted.object[0].deleted_at, stamp);
  equal('未删除视图查不到', (await mock.selectExpense({ ...base, deleted: DELETED_NO })).count, 0);
  equal('全部视图仍在', (await mock.selectExpense({ ...base, deleted: DELETED_ALL })).count, 2);
});

test('记账币种切换：取不到汇率的行算失败，结果是部分成功', async () => {
  const inserted = await insertAndSelect([
    { expense_date: '2026-02-01', expense_currency: 'CNY', expense_amount: '100', counterparty: '切换用例' },
  ]);
  const result = await mock.switchAccountingCurrency('USD');
  ok('成功笔数不为零', result.object.done > 0);
  const row = (await mock.selectExpense({ id: [inserted.rows[0].id] })).object[0];
  equal('记账币种已切', row.accounting_currency, 'USD');
  equal('汇率按 CNY→USD 取', row.exchange_rate, '0.140449');
  equal('记账金额重算', row.accounting_amount, '14.04');
  await rejects('目标币种非法', mock.switchAccountingCurrency('人民币'), '不在枚举内');
});

test('候选取值：字段白名单之外一律报错', async () => {
  for (const field of ['expense_type', 'bank_name', 'card_last_4', 'expense_currency', 'accounting_currency']) {
    const result = await mock.selectDistinct(field);
    ok(`${field} 返回数组`, Array.isArray(result.object));
    same(`${field} 已去重排序`, result.object, [...new Set(result.object)].sort());
  }
  await rejects('白名单外', mock.selectDistinct('remark'), '字段不支持');
});

test('审计与文件：模糊匹配命中，文件内容按文件ID 取回', async () => {
  const inserted = await mock.insertExpense('2609-模糊.csv', csvOf([
    { expense_date: '2026-01-05', expense_currency: 'CNY', expense_amount: '9', counterparty: '文件用例' },
  ]), 'CNY');
  const logs = await mock.selectOperationLog({ summary_like: '2609-模糊' });
  equal('摘要模糊命中', logs.count, 1);
  equal('审计ID 对得上', logs.object[0].id, inserted.object);

  const files = await mock.selectFileMeta({ file_name_like: '模糊' });
  equal('文件名模糊命中', files.count, 1);
  const blob = await mock.downloadFileBlob(files.object[0].id);
  equal('取回的是入库时那份内容', blob.object.file_name, '2609-模糊.csv');
  ok('内容里有表头', blob.object.data.includes('摊销月数'));
  await rejects('文件不存在', mock.downloadFileBlob(1), '文件不存在');
});

test('口令：首个请求定下口令，之后对不上就打不开', async () => {
  await rejects('空口令', mock.ping(''), '获取口令，为空');
  const first = await mock.ping('jotcash-2026');
  equal('探针回了服务标识', first.object.sn, 'jotcash-mock');
  await rejects('换一把口令', mock.ping('another-token'), '口令错误或数据库文件损坏');
  await mock.changeToken('jotcash-2026', 'jotcash-2027');
  ok('新口令能开', (await mock.ping('jotcash-2027')).object.sn === 'jotcash-mock');
  await rejects('旧口令开不了', mock.ping('jotcash-2026'), '口令错误或数据库文件损坏');
});

test('种子数据：已有数据就不再种，免得刷新一次多一批', async () => {
  const before = (await mock.selectExpense({ deleted: DELETED_ALL })).count;
  mock.seed();
  equal('二次调用不追加', (await mock.selectExpense({ deleted: DELETED_ALL })).count, before);
});
