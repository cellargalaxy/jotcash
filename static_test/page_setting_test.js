import test from 'node:test';
import './helper/browser.js';
import {
  answerModal,
  click,
  find,
  findAll,
  findByText,
  flush,
  lastBlobText,
  location,
  modals,
  renderPage,
  setValue,
  takeToast,
} from './helper/fixture.js';
import { equal, includes, not, ok } from './helper/check.js';
import * as api from '../static/js/api.js';
import { TOKEN_MIN_LEN } from '../static/js/config.js';
import { render as renderSetting } from '../static/js/page_setting.js';
import { renderUnlock } from '../static/js/page_unlock.js';
import { getAccountingCurrency, getClientToken, isUnlocked, lock, unlock } from '../static/js/store.js';

api.seedMock();

test('解锁页：两把口令都要填，探针通过才算解锁', async () => {
  lock();
  let unlocked = 0;
  const host = await renderPage((container) => renderUnlock(container, () => { unlocked += 1; }), {});
  includes('mock 模式要标明', host.textContent, '当前是 mock 模式');
  includes('说清了口令只在本标签页', host.textContent, '关掉标签页即失效');

  const inputs = findAll(host, 'input');
  setValue(inputs[0], '后端口令');
  setValue(inputs[1], '');
  click(findByText(host, 'button', '解锁'));
  await flush();
  includes('缺一把都不行', takeToast(), '两个口令都要填');
  equal('没解锁', unlocked, 0);
  not('会话没建起来', isUnlocked());

  setValue(inputs[1], 'jotcash-2026');
  click(findByText(host, 'button', '解锁'));
  await flush();
  equal('解锁回调只走一次', unlocked, 1);
  ok('会话建起来了', isUnlocked());
  equal('记账币种取下拉选的', getAccountingCurrency(), find(host, 'select').value);
});

test('解锁页：口令不对时探针会失败，会话要退回锁定态', async () => {
  lock();
  let unlocked = 0;
  const host = await renderPage((container) => renderUnlock(container, () => { unlocked += 1; }), {});
  const inputs = findAll(host, 'input');
  setValue(inputs[0], '后端口令');
  setValue(inputs[1], '对不上的口令');
  click(findByText(host, 'button', '解锁'));
  await flush();
  includes('失败提示', takeToast(), '口令错误或数据库文件损坏');
  equal('没有回调', unlocked, 0);
  not('会话已退回锁定', isUnlocked());
  equal('按钮恢复可点', findByText(host, 'button', '解锁').disabled, false);
});

test('解锁页：口令输入框带明文开关', async () => {
  lock();
  const host = await renderPage((container) => renderUnlock(container, () => {}), {});
  const toggle = findAll(host, 'button').find((button) => button.textContent === '显示');
  const input = findAll(host, 'input')[0];
  equal('默认是密码框', input.type, 'password');
  click(toggle);
  equal('点一下变明文', input.type, 'text');
  equal('按钮文案跟着变', toggle.textContent, '隐藏');
  click(toggle);
  equal('再点回密码框', input.type, 'password');
});

//辅助函数：跑设置页，之前得先解锁，否则会话卡片里读到的都是空的
async function renderSettingPage() {
  unlock('后端口令', 'jotcash-2026', 'CNY');
  return renderPage(renderSetting, {});
}

//辅助函数：填两次新口令并点更换
async function changeToken(host, token, repeat) {
  const inputs = findAll(findByText(host, 'div.card', '更换口令'), 'input');
  setValue(inputs[0], token);
  setValue(inputs[1], repeat === undefined ? token : repeat);
  click(findByText(host, 'button', '更换口令'));
  await flush();
}

//口令强度与后端 tool.CheckToken 是同一套判据，前端先挡一道，省一次必然失败的请求
test('设置页：换口令的强度判据与后端一致', async () => {
  const host = await renderSettingPage();

  await changeToken(host, 'abcdefghijk1', 'abcdefghijk2');
  includes('两次不一致', takeToast(), '两次输入的新口令不一致');

  await changeToken(host, 'abc1');
  includes('长度不足', takeToast(), `口令长度不足${TOKEN_MIN_LEN}位`);

  await changeToken(host, 'abcdefghijkl');
  includes('纯字母', takeToast(), '口令不能为纯数字或纯字母');

  await changeToken(host, '123456789012');
  includes('纯数字', takeToast(), '口令不能为纯数字或纯字母');

  await changeToken(host, 'abcdef ghij1');
  includes('含空格', takeToast(), '口令不能包含空格');

  equal('全程没弹过确认框', modals().length, 0);
});

test('设置页：换口令要敲关键字确认，成功后本会话切到新口令', async () => {
  const host = await renderSettingPage();
  await changeToken(host, 'jotcash-2027');
  const modal = modals()[0].node;
  includes('弹窗说清了不可逆', modal.textContent, '口令丢失等于数据永久不可读');
  ok('没敲关键字时不放行', findByText(modal, 'button', '确认').disabled);

  setValue(find(modal, 'input'), '确认更换');
  click(findByText(modal, 'button', '确认'));
  await flush();
  includes('成功提示', takeToast(), '口令已更换，本会话已切到新口令');
  equal('会话里换成了新口令', getClientToken(), 'jotcash-2027');
});

test('设置页：改本会话记账口径只动会话，不动已有数据', async () => {
  const host = await renderSettingPage();
  const control = find(findByText(host, 'div.card', '记账口径'), 'select');
  setValue(control, 'JPY');
  click(findByText(host, 'button', '保存本会话口径'));
  await flush();
  includes('提示', takeToast(), '本会话记账币种已改为 JPY');
  equal('会话里改了', getAccountingCurrency(), 'JPY');

  const rows = await api.selectExpense({ deleted: 1, sort: 'id asc' });
  not('已有明细的记账币种没被顺手改掉', rows.object.some((row) => row.accounting_currency === 'JPY'));
  includes('页面写明了要改已有数据得走切换', host.textContent, '必须执行记账币种切换');
  unlock('后端口令', 'jotcash-2026', 'CNY');
});

test('设置页：记账币种切换要二次确认，确认后逐笔重算', async () => {
  const host = await renderSettingPage();
  const selects = findAll(findByText(host, 'div.card', '记账口径'), 'select');
  setValue(selects[1], 'USD');
  click(findByText(host, 'button', '执行切换'));
  await flush();
  includes('弹窗讲清了范围', modals()[0].node.textContent, '含已删除');
  answerModal(true);
  await flush();
  includes('结果带成功与失败笔数', takeToast(), '切换完成，成功');

  const rows = await api.selectExpense({ deleted: 1, sort: 'id asc' });
  ok('确实切过去了', rows.object.every((row) => row.accounting_currency === 'USD'));
});

test('设置页：导出数据库落下一个文件，导入必须先选文件', async () => {
  const host = await renderSettingPage();
  click(findByText(host, 'button', '导出数据库'));
  await flush();
  includes('导出提示带文件名', takeToast(), '已导出 jotcash-mock-');
  includes('导出的是同构快照', await lastBlobText(), '"expense"');

  click(findByText(host, 'button', '导入并覆盖'));
  await flush();
  includes('没选文件就报错', takeToast(), '没有选择文件');
  equal('也不该弹确认框', modals().length, 0);
});

test('设置页：会话卡片把接口地址与数据来源摊开给人看', async () => {
  const host = await renderSettingPage();
  const card = findByText(host, 'div.card', '会话与运行信息');
  includes('接口地址解析成了绝对地址', card.textContent, '../api/ → https://localhost/api/');
  includes('数据来源写明是 mock', card.textContent, 'mock（浏览器内存，不发请求）');
  includes('页面地址', card.textContent, location.href);

  click(findByText(card, 'button', '锁定并清空本会话口令'));
  not('锁上了', isUnlocked());
  ok('重新加载了页面', location.reloaded > 0);
});
