import { createHash, createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import './helper/browser.js';
import { equal, includes, ok, rejects, same } from './helper/check.js';
import * as api from '../static/js/api.js';
import { DELETED_ALL, MODE_MOCK, MODE_REAL } from '../static/js/config.js';
import { lock, unlock } from '../static/js/store.js';

//真实模式这条链路在 mock 模式下一个字节都跑不到，而它恰恰是联调当天唯一会被走的那条。
//这里把 fetch 换成录音机：请求发成什么样、响应怎么拆、失败怎么报，全都按真接口的形态来验
const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');

function jsonResponse(body, status) {
  return {
    status: status || 200,
    headers: { get: (name) => (name.toLowerCase() === 'content-type' ? 'application/json; charset=utf-8' : null) },
    json: () => Promise.resolve(body),
  };
}

function ok200(object, count) {
  return jsonResponse({ code: 200, msg: '', data: { object, count: count || 0 } });
}

//辅助函数：装上录音机跑一段真实模式的调用，返回结果与录下来的请求
async function recording(handler, run) {
  const calls = [];
  const original = globalThis.fetch;
  unlock('后端口令', '前端口令', 'JPY', MODE_REAL);
  globalThis.fetch = (url, init) => {
    calls.push({ url, init });
    return Promise.resolve(handler(url, init, calls.length));
  };
  try {
    return { result: await run(), calls };
  } finally {
    globalThis.fetch = original;
    lock();
  }
}

//辅助函数：把 base64url 解回字符串
function fromBase64Url(text) {
  return Buffer.from(text.replace(/-/g, '+').replace(/_/g, '/'), 'base64');
}

test('凭据：签名算法与 go_common 的 EnJwt 对得上，口令只走请求头', async () => {
  const { calls } = await recording(() => ok200(null, 0), () => api.ping());
  const token = calls[0].init.headers.Authorization.replace('Bearer ', '');
  const [header, payload, signature] = token.split('.');

  same('头部就是 HS256', JSON.parse(fromBase64Url(header).toString('utf8')), { alg: 'HS256', typ: 'JWT' });
  //后端拿 sha256(后端口令) 的原始字节当 HMAC 密钥，不是十六进制串
  const key = createHash('sha256').update('后端口令', 'utf8').digest();
  const want = createHmac('sha256', key).update(`${header}.${payload}`).digest('base64url');
  equal('签名逐字节相等', signature, want);

  const claims = JSON.parse(fromBase64Url(payload).toString('utf8'));
  equal('前端口令签在 claim 里', claims.client_token, '前端口令');
  equal('记账币种逐请求携带', claims.accounting_currency, 'JPY');
  //ValidateGin 里 exp 为 0 会被当成 1970 年直接判过期，所以它必填
  ok('有过期时间', claims.exp > claims.iat);
  ok('有效期不超过十分钟', claims.exp - claims.iat <= 600);
  //claim 名要对得上 model.Claims 的 json tag，否则后端解出来是空值
  const claimSource = readFileSync(join(ROOT, 'model/claims.go'), 'utf8');
  for (const name of ['client_token', 'accounting_currency']) {
    ok(`model.Claims 认得 ${name}`, claimSource.includes(`json:"${name}`));
  }

  //口令一旦落到 query 就会被访问日志原样记下来，所以 url 上一个字都不能有
  equal('凭据不进 url', calls[0].url.includes('Authorization'), false);
});

test('请求：查询条件里的空值不进请求体，Go 的零值吃不下空串', async () => {
  const { calls } = await recording(() => ok200([], 0), () => api.selectExpense({
    id: [],
    expense_date_start: '2026-01-01T00:00:00+08:00',
    expense_date_end: '',
    expense_amount_min: null,
    counterparty_like: '',
    deleted: 0,
    sort: 'expense_date desc',
  }));
  const body = JSON.parse(calls[0].init.body);
  same('只剩真正填了的条件', body, { expense_date_start: '2026-01-01T00:00:00+08:00', deleted: 0, sort: 'expense_date desc' });
  //deleted 的 0 是「只查未删除」，不是「没填」，不能跟空串一起被筛掉
  ok('删除筛选的 0 留下来了', 'deleted' in body);
  equal('不带分页参数即取全量', 'page_size' in body, false);
});

//本轮之前这四条都是「后端接口尚未实现」，一点就报错
test('联调：四条新接的接口打的是后端注册的那几条路由', async () => {
  const cases = [
    ['明细编辑', '../api/expense/update', () => api.updateExpense({ id: 1, version: 2, remark: '改过' }), ok200({ id: 1 }, 1)],
    ['记账币种切换', '../api/expense/switch_currency', () => api.switchAccountingCurrency('USD'), ok200({ done: 3, failed: 0 }, 3)],
    ['候选取值', '../api/expense/distinct', () => api.selectDistinct('expense_type'), ok200(['餐饮'], 1)],
  ];
  for (const [name, path, call, response] of cases) {
    const { calls } = await recording(() => response, call);
    equal(`${name}的路径`, calls[0].url, path);
    equal(`${name}是 POST`, calls[0].init.method, 'POST');
  }

  //明细编辑送的是整条：后端以请求为底，再把不可变字段按库里的旧值还原回去
  const edited = await recording(() => ok200({ id: 7, version: 3 }, 1), () => api.updateExpense({ id: 7, version: 2, remark: '改过', expense_type: '数码' }));
  same('请求体是整条明细', JSON.parse(edited.calls[0].init.body), { id: 7, version: 2, remark: '改过', expense_type: '数码' });

  //候选集合要与「记账币种切换」的作用域对齐，那边改的就是含已删除的全部明细
  const distinct = await recording(() => ok200(['CNY', 'USD'], 2), () => api.selectDistinct('accounting_currency'));
  same('候选按含已删除取', JSON.parse(distinct.calls[0].init.body), { field: 'accounting_currency', deleted: DELETED_ALL });
  same('拆出来的就是取值列表', distinct.result.object, ['CNY', 'USD']);

  const switched = await recording(() => ok200({ done: 3, failed: 1 }, 3), () => api.switchAccountingCurrency('USD'));
  same('切换的请求体', JSON.parse(switched.calls[0].init.body), { accounting_currency: 'USD' });
  equal('成败计数拆得出来', switched.result.object.done, 3);
});

//下载与导出返回的是二进制，外壳必须跟 mock 那一侧一样，否则页面上 result.object 直接是 undefined
test('下载：返回的外壳与 mock 同形，文件名优先认 filename*', async () => {
  const disposition = `attachment; filename="2609-招商.csv"; filename*=UTF-8''2609-%E6%8B%9B%E5%95%86.csv`;
  const binary = (name) => ({
    status: 200,
    headers: {
      get: (key) => {
        const lower = key.toLowerCase();
        if (lower === 'content-type') return 'application/octet-stream';
        if (lower === 'content-disposition') return name;
        return null;
      },
    },
    blob: () => Promise.resolve(new Blob(['银行名称\n招商'])),
  });

  const { result, calls } = await recording(() => binary(disposition), () => api.downloadFile(2609161112130001));
  equal('打的是文件下载', calls[0].url, '../api/file_meta/download');
  same('请求体是文件ID', JSON.parse(calls[0].init.body), { id: 2609161112130001 });
  equal('外壳是 object', typeof result.object, 'object');
  equal('中文文件名从 filename* 解出来，不是乱码', result.object.file_name, '2609-招商.csv');
  ok('拿到的是 blob', result.object.blob instanceof Blob);

  //后端用的是 url.QueryEscape，它把空格编成 +，而真的 + 会被编成 %2B
  const spaced = await recording(() => binary(`attachment; filename="a b.csv"; filename*=UTF-8''%E6%88%91+%E7%9A%84.csv`), () => api.downloadFile(1));
  equal('加号还原成空格', spaced.result.object.file_name, '我 的.csv');

  //整库导出那条没有原始文件名可依，ASCII 的 filename= 就够
  const exported = await recording(() => binary('attachment; filename="jotcash-2609161112130002.db"'), () => api.exportDb());
  equal('导出打的是导出路由', exported.calls[0].url, '../api/db/export');
  equal('导出文件名', exported.result.object.file_name, 'jotcash-2609161112130002.db');

  //响应头一个都没给时不能把整条链路带崩，退回到兜底名字
  const nameless = await recording(() => binary(''), () => api.exportDb());
  equal('兜底文件名', nameless.result.object.file_name, 'jotcash.db');
});

test('失败：业务失败看响应体的 code，非 JSON 响应要说人话', async () => {
  //鉴权失败与业务失败都是 HTTP 200，成败只能看响应体
  await rejects(
    '业务失败照抄后端原文',
    recording(() => jsonResponse({ code: 500, msg: '明细编辑，已删除明细不可编辑: 7', data: null }), () => api.updateExpense({ id: 7 })),
    '明细编辑，已删除明细不可编辑',
  );
  await rejects(
    '鉴权失败也是 200 + code',
    recording(() => jsonResponse({ code: 401, msg: 'Authorization非法', data: null }), () => api.ping()),
    'Authorization非法',
  );

  //页面不是后端托的时候，打到静态服务上拿回来的是一篇 HTML
  const html = {
    status: 404,
    headers: { get: () => 'text/html; charset=utf-8' },
    json: () => Promise.reject(new Error("Unexpected token '<'")),
  };
  await rejects('非 JSON 响应给的是能照着办的话', recording(() => html, () => api.ping()), 'HTTP 404');
  await rejects('并且点名了 mock 模式这条出路', recording(() => html, () => api.selectFileMeta({})), 'mock');

  //服务压根没起来时 fetch 直接抛，抛的是浏览器自己的原文，词表里没有也不该有
  const down = { fetch: () => { throw new TypeError('fetch failed'); } };
  const original = globalThis.fetch;
  unlock('后端口令', '前端口令', 'CNY', MODE_REAL);
  globalThis.fetch = down.fetch;
  try {
    await rejects('连不上就说连不上', api.ping(), '连不上后端接口');
    await rejects('并且点出打的是哪条地址', api.selectExpense({}), '../api/expense/select');
  } finally {
    globalThis.fetch = original;
    lock();
  }

  //导出失败时流出来的是 JSON，这时候要把后端的原因抛出去，而不是把错误页当成库文件存下来
  await rejects(
    '导出失败不落盘',
    recording(() => jsonResponse({ code: 500, msg: '导出数据库，写审计异常: x', data: null }), () => api.exportDb()),
    '导出数据库，写审计异常',
  );
});

test('分发：会话说 mock 就一个请求都不发，说真实就一个 mock 都不碰', async () => {
  const original = globalThis.fetch;
  let hits = 0;
  globalThis.fetch = () => { hits += 1; return Promise.reject(new Error('mock 模式不该发请求')); };
  try {
    unlock('后端口令', '前端口令', 'CNY', MODE_MOCK);
    api.seedMock();
    const mocked = await api.selectExpense({ deleted: 0, sort: 'id asc' });
    ok('mock 模式拿到了种子数据', mocked.count > 0);
    equal('一个请求都没发', hits, 0);

    //锁定之后既没有口令也没有来源，再调就该在签名这一步被拦住，不能悄悄打出去
    lock();
    await rejects('锁定之后调不动', api.selectExpense({ deleted: 0 }), '未解锁');
    equal('仍然一个请求都没发', hits, 0);
  } finally {
    globalThis.fetch = original;
    lock();
  }
});

test('分发：换语言重写种子只在 mock 模式下发生', async () => {
  unlock('后端口令', '前端口令', 'CNY', MODE_REAL);
  //真实模式下库里存的是用户自己的数据，一个字都不该被前端改写
  api.seedMock();
  api.relocalizeMock();
  unlock('后端口令', '前端口令', 'CNY', MODE_MOCK);
  const seeded = await api.selectExpense({ deleted: 0, sort: 'id asc' });
  ok('真实模式下没播过种子，进了 mock 才播', seeded.count > 0);
  lock();
});

test('上传：明细入库与整库导入走同一个字段名，且不自己设 Content-Type', async () => {
  const cases = [
    ['明细入库', '../api/expense/insert', () => api.insertExpense('2609.csv', '银行名称\n招商')],
    ['整库导入', '../api/db/import', () => api.importDb(new File(['db'], 'jotcash.db'))],
  ];
  for (const [name, path, call] of cases) {
    const { calls } = await recording(() => ok200(2609161112130001, 1), call);
    equal(`${name}的路径`, calls[0].url, path);
    ok(`${name}送的是表单`, calls[0].init.body instanceof FormData);
    ok(`${name}的字段名是 file`, calls[0].init.body.has('file'));
    //multipart 的分隔符由 FormData 自己定，手写 Content-Type 会让后端解不出 boundary
    equal(`${name}不手写 Content-Type`, 'Content-Type' in calls[0].init.headers, false);
    includes(`${name}带着凭据`, calls[0].init.headers.Authorization, 'Bearer ');
  }
});
