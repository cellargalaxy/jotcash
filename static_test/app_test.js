import test from 'node:test';
import { document, fireWindow, mountHost } from './helper/browser.js';
import { check, click, find, findAll, findByText, flush, location, setValue, texts, unlockForm } from './helper/fixture.js';
import { equal, includes, not, ok, same } from './helper/check.js';
import { candidateOf } from '../static/js/expense_inquiry.js';
import { isUnlocked, lock } from '../static/js/store.js';

//app.js 在 import 阶段就会跑起来：先摆好 index.html 里的容器，再让它启动
mountHost();
lock();
await import('../static/js/app.js');

//辅助函数：换个 hash 再让路由跑一遍，等同于浏览器里点导航
async function goto(hash) {
  location.hash = hash;
  fireWindow('hashchange');
  await flush();
}

//辅助函数：当前这一屏的页面容器
function page() {
  return document.querySelector('#page-host');
}

test('启动：没解锁之前只给解锁页，导航整条藏起来', async () => {
  not('初始是锁定态', isUnlocked());
  includes('落在解锁页', page().textContent, '个人记账 · 只记支出 · 整库加密 · 无登录态');
  ok('导航隐藏', document.querySelector('#nav-wrap').hidden);
  equal('会话区是空的', document.querySelector('#session-host').textContent, '');
  //输口令之前先让人看清自己走的是什么协议，这条公告在解锁页上不许关
  not('解锁页上不给关公告', find(document.querySelector('#notice-host'), 'button.btn-close'));
  //看不懂中文的人得先能把语言换掉，才轮得到输口令，所以这两个下拉不跟解锁走
  equal('锁着也给主题与语言两个下拉', findAll(document.querySelector('#pref-host'), 'select').length, 2);
});

test('启动：解锁之后导航显出来，统计紧跟在明细后面', async () => {
  const form = unlockForm(page());
  //整屏跑的是 mock 这门会话：真实模式会去打 fetch，而这套用例里没有后端
  check(form.modes[1], true);
  setValue(form.tokens[0], '后端口令');
  setValue(form.tokens[1], 'jotcash-2026');
  click(findByText(page(), 'button', '解锁'));
  await flush();

  ok('已解锁', isUnlocked());
  not('导航不再隐藏', document.querySelector('#nav-wrap').hidden);
  same('五个页面与顺序', texts(document.querySelector('#nav-host'), 'a'), ['明细', '统计', '审计', '文件', '设置']);
  includes('会话区挂着记账币种', document.querySelector('#session-host').textContent, '记账币种 CNY');
  ok('会话区有锁定按钮', findByText(document.querySelector('#session-host'), 'button', '锁定'));
  includes('默认落在明细页', page().textContent, '支出明细');
});

test('路由：hash 换了就换页，未知路径回明细', async () => {
  await goto('#/statistic');
  includes('统计页', page().textContent, '金额统计');
  includes('导航高亮统计', findByText(document.querySelector('#nav-host'), 'a', '统计').className, 'active');

  await goto('#/operation-log');
  includes('审计页', page().textContent, '操作审计');

  await goto('#/file-meta');
  includes('文件页', page().textContent, '文件');

  await goto('#/setting');
  includes('设置页', page().textContent, '设置');

  await goto('#/哪都不是');
  includes('未知路径回明细', page().textContent, '支出明细');
});

//从审计页点「本批明细」跳过来的就是这种地址，参数要原样解析给页面
test('路由：hash 里的查询参数解析成页面参数', async () => {
  await goto('#/expense?operation_id=2609021012330001&verify=1');
  includes('进了核实视图', page().textContent, '审计ID 2609021012330001 这一批次');

  await goto('#/expense?operation_id=2609021012330001');
  not('不带 verify 就不是核实视图', page().textContent.includes('核实视图'));

  await goto('#/expense?file_id=');
  includes('空参数不该把页面弄崩', page().textContent, '支出明细');
});

test('公告栏：安全上下文下的文案，关掉之后不再冒出来', async () => {
  const notice = find(document.querySelector('#notice-host'), 'div.alert');
  ok('公告在', notice);
  includes('这是安全上下文', notice.className, 'alert-success');
  includes('讲清了口令存哪', notice.textContent, '口令只存在本标签页的 sessionStorage');
  includes('把完整地址摊出来', notice.textContent, location.href);

  const close = find(notice, 'button.btn-close');
  ok('解锁之后才有关闭按钮', close);
  click(close);
  equal('关掉就空了', document.querySelector('#notice-host').textContent, '');

  await goto('#/statistic');
  equal('关过之后切页面也不再冒出来', document.querySelector('#notice-host').textContent, '');
});

//换语言是整屏重绘而不是刷新页面：mock 模式下一刷新，这一轮改过的数据全没了
test('偏好：换语言，导航与当前这一屏一起换，页面不刷新', async () => {
  const before = location.reloaded;
  setValue(findAll(document.querySelector('#pref-host'), 'select')[1], 'en');
  await flush();
  same('导航换成英文', texts(document.querySelector('#nav-host'), 'a'), ['Records', 'Stats', 'Audit', 'Files', 'Settings']);
  includes('当前这一屏也换了', page().textContent, 'Amount statistics');
  includes('会话区跟着换', document.querySelector('#session-host').textContent, 'Accounting CNY');
  equal('没有刷新页面', location.reloaded, before);
  //mock 的种子数据跟着语言走，候选是从库里 distinct 出来的，所以也不该再留着上一门语言的取值
  ok('候选取到了英文的支出类型', candidateOf('expense_type').includes('Dining'));
  not('候选里不再有中文的支出类型', candidateOf('expense_type').includes('餐饮'));

  setValue(findAll(document.querySelector('#pref-host'), 'select')[1], 'zh');
  await flush();
  same('换回中文', texts(document.querySelector('#nav-host'), 'a'), ['明细', '统计', '审计', '文件', '设置']);
  includes('这一屏也换回来了', page().textContent, '金额统计');
});

test('偏好：换主题就落到 html 上，整屏也跟着重绘', async () => {
  const themeSelect = findAll(document.querySelector('#pref-host'), 'select')[0];
  setValue(themeSelect, 'dark');
  await flush();
  equal('html 上是深色', document.documentElement.getAttribute('data-bs-theme'), 'dark');
  ok('重绘之后下拉还在', findAll(document.querySelector('#pref-host'), 'select')[0]);

  setValue(findAll(document.querySelector('#pref-host'), 'select')[0], 'light');
  await flush();
  equal('html 上是浅色', document.documentElement.getAttribute('data-bs-theme'), 'light');
});

test('会话：点锁定就清会话并重新加载页面', async () => {
  const before = location.reloaded;
  click(findByText(document.querySelector('#session-host'), 'button', '锁定'));
  not('会话已清', isUnlocked());
  equal('重新加载了一次', location.reloaded, before + 1);
});
