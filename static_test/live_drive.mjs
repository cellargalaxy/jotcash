//联调驱动：用页面自己的 api.js 去打一个真起来的后端，由 handler/frontend_live_test.go 拉起。
//它不是单测——单测里的 fetch 是录音机，证明得了「请求发成什么样」，证明不了「后端认不认」。
//两件事只有这里能一起证明：凭据后端验得过，响应形态前端拆得开。
//参数：<后端地址> <后端口令> <前端口令> <仓库根目录>
import { chdir } from 'node:process';

const [base, serverToken, clientToken, repo] = process.argv.slice(2);
chdir(repo);

//页面代码要的宿主能力只有这三样：会话存储、偏好存储、语言。装到够用为止
function storage() {
  const box = new Map();
  return {
    getItem: (key) => (box.has(key) ? box.get(key) : null),
    setItem: (key, value) => box.set(key, String(value)),
    removeItem: (key) => box.delete(key),
  };
}
globalThis.sessionStorage = storage();
globalThis.localStorage = storage();
Object.defineProperty(globalThis, 'navigator', {
  value: { language: 'zh-CN', languages: ['zh-CN'] },
  configurable: true,
  writable: true,
});

//页面挂在 <前缀>/static 下，API_BASE 是 '../api/'。按真实页面地址解析相对路径，
//顺手把「相对路径落不落得到 /api 上」一并验了——这条在浏览器里是白盒，在 Node 里得自己接上
const realFetch = globalThis.fetch;
globalThis.fetch = (url, init) => realFetch(new URL(url, `${base}/static/index.html`), init);

const api = await import(`${repo}/static/js/api.js`);
const { EXPENSE_SORTS, MODE_REAL } = await import(`${repo}/static/js/config.js`);
const { defaultInquiry, newInquiry } = await import(`${repo}/static/js/expense_inquiry.js`);
const { setClientToken, unlock } = await import(`${repo}/static/js/store.js`);
const { dateToRfc3339 } = await import(`${repo}/static/js/util.js`);

const lines = [];
function check(name, got, want) {
  const ok = String(got) === String(want);
  lines.push(`${ok ? '  ok ' : '  ✗  '} ${name}: ${got}`);
  if (!ok) lines.push(`       期望: ${want}`);
  return ok;
}
function show(name, detail) {
  lines.push(`  ok  ${name}: ${detail}`);
}

unlock(serverToken, clientToken, 'CNY', MODE_REAL);

//解锁探针：口令签得对、后端开得了库
await api.ping();
show('ping', '凭据后端验过了');

const csv = [
  '银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数',
  '招商银行,1234,2026-09-01,CNY,120.00,星巴克,甲,1,CNY,餐饮,1',
  '招商银行,1234,2026-09-01,CNY,120.00,星巴克,乙,1,CNY,餐饮,1',
  '中国银行,5678,2026-08-11,USD,50.00,Amazon,丙,7.12,CNY,数码,6',
].join('\r\n');
//文件名刻意带中文与空格：Content-Disposition 的两份文件名里，只有 filename* 那份还原得回来
const inserted = await api.insertExpense('2609-招商 账单.csv', csv);
check('入库笔数', inserted.count, 3);

const batch = await api.selectExpense({ ...newInquiry(), operation_id: [inserted.object], deleted: 1 });
check('按审计ID 取回本批次', batch.count, 3);

const row = batch.object.find((item) => item.remark === '丙');
check('折算汇率按支出币种取到了', row.exchange_rate, '7.12');
check('记账金额 = 支出金额 × 折算汇率', row.accounting_amount, '356');
check('摊销月数', row.amortization_months, 6);
show('摊销区间', `${row.amortization_start_month} ~ ${row.amortization_end_month}`);

//页面初值与「重置」的落点：支出日期区间必填，默认最近一年
check('默认最近一年筛得到', (await api.selectExpense(defaultInquiry())).count, 3);
//核实视图那条查询：金额区间是字符串小数，币种是数组
const candidate = await api.selectExpense({
  ...newInquiry(),
  expense_date_start: dateToRfc3339('2026-08-01'),
  expense_date_end: dateToRfc3339('2026-09-30', true),
  expense_currency: ['CNY', 'USD'],
  expense_amount_min: '50',
  expense_amount_max: '120.00',
  deleted: 0,
});
check('金额区间用字符串小数也筛得动', candidate.count, 3);
//明细表格、图表与导出都吃这一份：不带分页参数即全量
check('不带分页即全量', (await api.selectExpense({ ...newInquiry(), deleted: 1 })).count, 3);
//统计页摊销口径用的那对筛选字段：丙那笔 2026-08 起摊 6 个月，摊到 2027-01，支出日期落不进 11~12 月，摊销区间落得进
check('区间内没有支出日期', (await api.selectExpense({
  ...newInquiry(),
  expense_date_start: dateToRfc3339('2026-11-01'),
  expense_date_end: dateToRfc3339('2026-12-31', true),
  deleted: 1,
})).count, 0);
check('摊销区间筛得到跨进区间的分期', (await api.selectExpense({
  ...newInquiry(),
  amortization_month_start: dateToRfc3339('2026-11-01'),
  amortization_month_end: dateToRfc3339('2026-12-01'),
  deleted: 1,
})).count, 1);
for (const sort of EXPENSE_SORTS) await api.selectExpense({ ...newInquiry(), sort: sort.value, deleted: 1 });
show('排序下拉', `${EXPENSE_SORTS.length} 个取值后端全认`);

const updated = await api.updateExpense({ ...row, remark: '联调改过', expense_type: '咖啡' });
check('编辑后版本号自增', updated.object.version, row.version + 1);
check('编辑后备注', updated.object.remark, '联调改过');

const distinct = await api.selectDistinct('expense_type');
//「数码」那一笔刚被改成「咖啡」，候选也就跟着少一项——候选取的是库里当下的真值，不是字典表
check('候选取值', JSON.stringify(distinct.object), JSON.stringify(['咖啡', '餐饮']));

const logs = await api.selectOperationLog({ page: 1, page_size: 20, sort: 'created_at desc' });
show('审计摘要', logs.object.map((item) => item.summary).join(' ;; '));
const changes = JSON.parse(logs.object.find((item) => item.operation_type === '明细编辑').changes);
//后端记的是前后两份整快照，审计页的字段级对照表就是照着这个形态拼的
check('变更内容的顶层键', Object.keys(changes).join(','), 'before,after');
check('变更内容记了前值', changes.before.remark, '丙');
check('变更内容记了后值', changes.after.remark, '联调改过');

const files = await api.selectFileMeta({ page: 1, page_size: 10, sort: 'created_at desc' });
const downloaded = await api.downloadFile(files.object[0].id);
check('中文带空格的文件名原样还原', downloaded.object.file_name, '2609-招商 账单.csv');
check('下下来的字节数与元数据对得上', downloaded.object.blob.size, files.object[0].file_size);
check('内容就是上传的那一份', (await downloaded.object.blob.text()).split('\r\n')[1], csv.split('\r\n')[1]);

const switched = await api.switchAccountingCurrency('USD');
show('记账币种切换', `成功 ${switched.object.done} 笔，失败 ${switched.object.failed} 笔`);

const exported = await api.exportDb();
check('导出的是二进制库文件', exported.object.blob.size > 0, true);
show('导出文件名', exported.object.file_name);

//辅助函数：跑一条注定失败的调用，把后端的原文捞出来——前端的英文词表就是照着它配的
async function reason(name, call, part) {
  let message = '';
  try {
    await call();
  } catch (err) {
    message = err.message;
  }
  check(`${name}（报错原文对得上）`, message.includes(part), true);
  show('  └ 原文', message);
}

//删除之前先验乐观锁：删掉之后后端会先报「已删除不可编辑」，版本号那条判据就够不着了
await reason('乐观锁挡住落后的版本', () => api.updateExpense({ ...row, version: 0 }), '数据已落后');
await reason('候选字段白名单', () => api.selectDistinct('remark'), '不在白名单内');
await reason('排序白名单', () => api.selectExpense({ ...newInquiry(), sort: 'remark desc' }), '不在白名单内');
await reason('摊销区间倒挂', () => api.selectExpense({
  ...newInquiry(),
  amortization_month_start: dateToRfc3339('2026-12-01'),
  amortization_month_end: dateToRfc3339('2026-11-01'),
}), '时间区间倒挂');

check('批量软删除', (await api.deleteExpense({ ...newInquiry(), id: [row.id] })).count, 1);
await reason('已删除的明细改不动', () => api.updateExpense({ ...row, remark: '再改一次' }), '已删除明细不可编辑');

//换口令是终态动作，放最后：换完之后这一门会话得能以新口令继续
const newToken = 'jotcash-2609-联调';
await api.changeToken(newToken);
setClientToken(newToken);
await api.ping();
show('更换口令', '换完之后新口令开得了库');

console.log(lines.join('\n'));
if (lines.some((line) => line.startsWith('  ✗'))) process.exit(1);
