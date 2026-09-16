import test from 'node:test';
import { readFileSync, readdirSync } from 'node:fs';
import './helper/browser.js';
import { setNavigatorLanguage } from './helper/browser.js';
import { renderPage } from './helper/fixture.js';
import { equal, excludes, includes, ok, same } from './helper/check.js';
import { STATIC_DIR, staticPath } from './helper/lib.js';
import {
  CURRENCIES,
  EXPENSE_FIELDS,
  EXPENSE_SORTS,
  FILE_META_SORTS,
  OPERATION_LOG_SORTS,
  OPERATION_RESULTS,
  OPERATION_TYPES,
} from '../static/js/config.js';
import { MEASURES } from '../static/js/expense_statistic.js';
import { LANGS, LANG_EN, LANG_ZH, detectLang, getLang, serverText, setLang, t } from '../static/js/i18n.js';
import { SERVER, TEXT } from '../static/js/lang_en.js';
import { THEMES } from '../static/js/theme.js';
import { LANG_KEY } from '../static/js/store.js';

//辅助函数：把页面源码里的行注释剥掉，免得注释里的中文被当成词条
function sourceOf(file) {
  return readFileSync(staticPath('js', file), 'utf8').split('\n').filter((line) => !/^\s*\/\//.test(line)).join('\n');
}

function pageModules() {
  return readdirSync(staticPath('js')).filter((file) => file.endsWith('.js') && !file.startsWith('lang_'));
}

//辅助函数：收集全仓 t('…') 里写死的那些键。变量形态的 t(field.name) 收不到，由下面的枚举用例兜住
function literalKeys() {
  const keys = new Set();
  for (const file of pageModules()) {
    const matcher = /\bt\((['"])((?:\\.|(?!\1)[^\\])*)\1/g;
    const source = sourceOf(file);
    let matched = matcher.exec(source);
    while (matched) {
      keys.add(matched[2]);
      matched = matcher.exec(source);
    }
  }
  return [...keys];
}

function placeholders(text) {
  return (String(text).match(/\{\w+\}/g) || []).sort();
}

test('默认语言：浏览器是中文才给中文，其余一律英文', () => {
  setNavigatorLanguage('zh-CN');
  equal('zh-CN', detectLang(), LANG_ZH);
  setNavigatorLanguage('zh-TW');
  equal('zh-TW 也是中文', detectLang(), LANG_ZH);
  setNavigatorLanguage('en-US');
  equal('en-US', detectLang(), LANG_EN);
  setNavigatorLanguage('ja-JP');
  equal('日语不是中文，落英文', detectLang(), LANG_EN);
  setNavigatorLanguage('');
  equal('读不到语言也落英文', detectLang(), LANG_EN);
  setNavigatorLanguage('zh-CN');
});

//浏览器首选英文、备选中文的人，按判据该拿到英文；拿整条 languages 去找中文就会翻车
test('默认语言：只看首选语言，不在备选里翻找中文', () => {
  globalThis.navigator = { language: 'en-US', languages: ['en-US', 'zh-CN'] };
  equal('首选英文即英文', detectLang(), LANG_EN);
  setNavigatorLanguage('zh-CN');
});

test('语言偏好：存进 localStorage，坏值与空值都回落到浏览器判定', async () => {
  localStorage.clear();
  setNavigatorLanguage('en-US');
  const fresh = await import('../static/js/i18n.js?case=empty');
  equal('没存过就跟浏览器走', fresh.getLang(), LANG_EN);

  localStorage.setItem(LANG_KEY, 'ja');
  const broken = await import('../static/js/i18n.js?case=broken');
  equal('存了不认识的语言，回落浏览器判定', broken.getLang(), LANG_EN);

  localStorage.setItem(LANG_KEY, LANG_ZH);
  const saved = await import('../static/js/i18n.js?case=saved');
  equal('存过就以存的为准，不再问浏览器', saved.getLang(), LANG_ZH);

  localStorage.clear();
  setNavigatorLanguage('zh-CN');
});

test('切换语言：写进偏好，下一次 t 立刻换语种', () => {
  setLang(LANG_EN);
  equal('落盘', localStorage.getItem(LANG_KEY), LANG_EN);
  equal('英文', t('查询'), 'Search');
  setLang(LANG_ZH);
  equal('中文即原文', t('查询'), '查询');
  equal('非法语种收敛到中文', (setLang('ja'), getLang()), LANG_ZH);
});

test('词条：命中、缺失、占位符替换', () => {
  setLang(LANG_EN);
  equal('命中', t('锁定'), 'Lock');
  equal('缺失就原样印中文，不印键名也不印空', t('这一句没有登记'), '这一句没有登记');
  equal('多占位符', t('共 {count} 条 · 第 {page}/{total} 页', { count: 59, page: 1, total: 3 }), '59 total · page 1/3');
  equal('少传一个就把占位符原样留着', t('共 {count} 条 · 第 {page}/{total} 页', { count: 59, page: 1 }), '59 total · page 1/{total}');
  setLang(LANG_ZH);
  equal('中文同样要替换占位符', t('共 {count} 条 · 第 {page}/{total} 页', { count: 5, page: 2, total: 9 }), '共 5 条 · 第 2/9 页');
});

//后端不改，报错原样是中文，转译只能在前端做
test('后端文案：整句命中', () => {
  setLang(LANG_EN);
  equal('报错', serverText('解析明细，文件格式无法识别'), 'Parse records: unrecognized file format');
  equal('审计操作类型', serverText('数据入库'), 'Data ingest');
  equal('审计结果', serverText('成功'), 'Success');
  equal('审计对象类型', serverText('支出明细'), 'Expense records');
  setLang(LANG_ZH);
  equal('中文下原样返回', serverText('解析明细，文件格式无法识别'), '解析明细，文件格式无法识别');
});

test('后端文案：带变量的按模板匹配，变量原样留下', () => {
  setLang(LANG_EN);
  equal('行号', serverText('解析CSV，第5行，摊销月数非法: 2.5'), 'Parse CSV: line 5, invalid amortization months: 2.5');
  equal('明细ID', serverText('明细编辑，明细不存在: 2609021012330001'), 'Record edit: record does not exist: 2609021012330001');
  equal('文件名不翻译', serverText('入库 22 笔，来源 2609-招商.csv'), 'Ingested 22 record(s) from 2609-招商.csv');
  equal('币种代码不翻译', serverText('切换记账币种为 USD，成功 3 笔，失败 1 笔'), 'Switched accounting currency to USD: 3 succeeded, 1 failed');
  setLang(LANG_ZH);
});

//后端的报错是「外层报位置 + 内层报原因」拼出来的，只翻外层等于没翻
test('后端文案：嵌套的报错要一层层翻到底', () => {
  setLang(LANG_EN);
  equal(
    '两层',
    serverText('解析明细，第3笔，记账币种，不在枚举内: xx'),
    'Parse records: record #3, Accounting currency is not in the enumeration: xx',
  );
  equal('明细编辑外层 + 汇率内层', serverText('明细编辑，折算汇率非正: -1'), 'Record edit: exchange rate is not positive: -1');
  setLang(LANG_ZH);
});

test('后端文案：没登记的原样透出，不吞不改', () => {
  setLang(LANG_EN);
  equal('整句没登记', serverText('某个还没来得及登记的后端报错'), '某个还没来得及登记的后端报错');
  equal('空值', serverText(null), '');
  setLang(LANG_ZH);
});

// ===== 词表与代码的契约 =====

test('词表：源码里每一处 t(\'…\') 都能在英文词表里找到', () => {
  const missing = literalKeys().filter((key) => !(key in TEXT));
  same('漏译的词条', missing, []);
  ok('词条数量不该是零，否则说明扫描本身失效了', literalKeys().length > 100);
});

//这些展示名是以变量形态进 t() 的，扫源码扫不到，只能逐个清单核对
test('词表：配置枚举的展示名也都在词表里', () => {
  const names = [
    ...EXPENSE_FIELDS.map((field) => field.name),
    ...CURRENCIES.map((currency) => currency.name),
    ...OPERATION_TYPES,
    ...OPERATION_RESULTS,
    ...EXPENSE_SORTS.map((sort) => sort.name),
    ...OPERATION_LOG_SORTS.map((sort) => sort.name),
    ...FILE_META_SORTS.map((sort) => sort.name),
    ...MEASURES.map((measure) => measure.name),
    ...THEMES.map((theme) => theme.name),
  ];
  same('漏译的枚举展示名', names.filter((name) => !(name in TEXT)), []);
});

//表头是一串字符串数组，逐个过 t()，同样扫不到
test('词表：三张表的列名都在词表里', () => {
  const columns = [];
  for (const file of ['page_operation_log.js', 'page_file_meta.js']) {
    const matched = /const columns = \[([^\]]+)\]/.exec(sourceOf(file));
    ok(`${file} 里找得到列名数组`, matched);
    for (const cell of matched[1].split(',')) {
      const name = cell.trim().replace(/^'|'$/g, '');
      if (name) columns.push(name);
    }
  }
  equal('两张表共 16 列', columns.length, 16);
  same('漏译的列名', columns.filter((name) => !(name in TEXT)), []);
});

//同一个键写两遍，后一个会悄悄盖掉前一个，而对象读出来是看不见这件事的，只能扫源码
test('词表：英文词表没有重复键', () => {
  const declared = readFileSync(staticPath('js', 'lang_en.js'), 'utf8').match(/^ {2}'(?:[^'\\]|\\.)*':/gm) || [];
  equal('声明条数与读出来的条数相等', declared.length, Object.keys(TEXT).length);
});

//占位符对不上是最典型的静默故障：页面上会直接印出 {count} 这种花括号
test('词表：每条译文的占位符与原文一一对上', () => {
  const broken = [];
  for (const key of Object.keys(TEXT)) {
    if (placeholders(key).join() !== placeholders(TEXT[key]).join()) broken.push(key);
  }
  same('占位符对不上的词条', broken, []);
});

test('词表：模板规则里的 $n 不超过它自己的捕获组数', () => {
  const broken = [];
  for (const rule of SERVER) {
    const groups = new RegExp(`${rule.pattern.source}|`).exec('').length - 1;
    for (const reference of rule.text.match(/\$(\d)/g) || []) {
      if (Number(reference.slice(1)) > groups) broken.push(rule.pattern.source);
    }
  }
  same('引用了不存在的捕获组', broken, []);
});

//语言下拉是给看不懂当前语言的人用的，选项名必须是该语言自己的写法，不能跟着当前语言变
test('语言下拉：两种语言下选项名都不翻译', () => {
  same('中文态', LANGS.map((lang) => lang.name), ['中文', 'English']);
  setLang(LANG_EN);
  same('英文态也一样', LANGS.map((lang) => lang.name), ['中文', 'English']);
  setLang(LANG_ZH);
});

// ===== 整页 =====

test('英文态：明细页与统计页整屏没有残留的中文文案', async () => {
  setLang(LANG_EN);
  const api = await import('../static/js/api.js');
  api.seedMock();
  const { unlock } = await import('../static/js/store.js');
  unlock('后端口令', '前端口令', 'CNY');

  const expense = await renderPage((await import('../static/js/page_expense.js')).render, {});
  includes('标题是英文', expense.textContent, 'Expense records');
  includes('工具条是英文', expense.textContent, 'Upload statement file');
  includes('分页条是英文', expense.textContent, 'total · page');
  excludes('不再出现中文标题', expense.textContent, '支出明细');
  excludes('不再出现中文按钮', expense.textContent, '上传账单文件');
  //种子里的对手方与支出类型是用户数据不是文案，翻译它才是错的
  includes('用户数据原样留着', expense.textContent, '盒马鲜生');

  const statistic = await renderPage((await import('../static/js/page_statistic.js')).render, {});
  includes('统计页标题', statistic.textContent, 'Amount statistics');
  includes('口径名', statistic.textContent, 'Amortized amount');
  excludes('不再出现中文口径', statistic.textContent, '摊销金额');
  //顿号只有中文有，拼列表时跟着语言走，否则英文句子里会冒出一个中文标点
  includes('币种列表用英文分隔符', statistic.textContent, 'CNY, USD');
  excludes('不再出现顿号', statistic.textContent, '、');
  setLang(LANG_ZH);
});

//这两页没有任何用户数据，所以「一个中文字都不该剩」是个能一眼判死的强判据
test('英文态：设置页与解锁页一个中文字都不剩', async () => {
  setLang(LANG_EN);
  const { lock, unlock } = await import('../static/js/store.js');
  unlock('后端口令', '前端口令', 'CNY');
  const setting = await renderPage((await import('../static/js/page_setting.js')).render, {});
  same('设置页残留的中文', setting.textContent.match(/[\u4e00-\u9fff]+/g) || [], []);

  lock();
  const unlockPage = await renderPage((await import('../static/js/page_unlock.js')).renderUnlock, {});
  same('解锁页残留的中文', unlockPage.textContent.match(/[\u4e00-\u9fff]+/g) || [], []);
  setLang(LANG_ZH);
});

test('英文态：审计页把后端给的操作类型、结果与摘要一并转过来', async () => {
  setLang(LANG_EN);
  const log = await renderPage((await import('../static/js/page_operation_log.js')).render, {});
  includes('操作类型', log.textContent, 'Data ingest');
  includes('操作结果', log.textContent, 'Success');
  includes('摘要模板', log.textContent, 'record(s) from');
  excludes('不再出现中文操作类型', log.textContent, '数据入库');
  includes('摘要里的文件名没被动过', log.textContent, 'Ingested 22 record(s) from 2609-招商.csv');
  setLang(LANG_ZH);
});

test('英文态：前端自己产生的校验失败也走词表', async () => {
  setLang(LANG_EN);
  const { toastErr } = await import('../static/js/util.js');
  const { takeToast } = await import('./helper/fixture.js');
  toastErr(new Error(t('两个口令都要填')));
  equal('前端报错', takeToast(), 'Both passphrases are required');
  //后端原文进的也是同一个出口
  toastErr(new Error('明细编辑，已删除明细不可编辑'));
  equal('后端报错', takeToast(), 'Record edit: a deleted record cannot be edited');
  setLang(LANG_ZH);
});

//首屏主题脚本是 theme.js 的最小复刻，键名一旦漂移，深色偏好的人每次打开都会先被闪一屏白
test('首屏主题脚本：index.html 里的键名与 store.js 的常量一致', () => {
  const html = readFileSync(`${STATIC_DIR}/index.html`, 'utf8');
  const matched = /localStorage\.getItem\('([^']+)'\)/.exec(html);
  ok('首屏脚本里读了偏好', matched);
  const themeKey = /THEME_KEY = '([^']+)'/.exec(readFileSync(staticPath('js', 'store.js'), 'utf8'));
  equal('键名一致', matched[1], themeKey[1]);
});
