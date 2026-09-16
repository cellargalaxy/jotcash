import test from 'node:test';
import './helper/browser.js';
import { equal, not, ok, same } from './helper/check.js';
import { CURRENCY_DEFAULT, EXPENSE_COLUMN_DEFAULT } from '../static/js/config.js';
import {
  getAccountingCurrency,
  getClientToken,
  getColumns,
  getServerToken,
  isUnlocked,
  lock,
  setAccountingCurrency,
  setClientToken,
  setColumns,
  unlock,
} from '../static/js/store.js';

test('会话：解锁写入、锁定清空，口令只落 sessionStorage', () => {
  lock();
  not('初始未解锁', isUnlocked());
  equal('未解锁取不到后端口令', getServerToken(), '');
  equal('未解锁回落默认记账币种', getAccountingCurrency(), CURRENCY_DEFAULT);

  unlock('后端口令', '前端口令', 'USD');
  ok('已解锁', isUnlocked());
  equal('后端口令', getServerToken(), '后端口令');
  equal('前端口令', getClientToken(), '前端口令');
  equal('记账币种', getAccountingCurrency(), 'USD');
  ok('只进 sessionStorage', sessionStorage.getItem('jotcash.session') !== null);
  equal('不进 localStorage', localStorage.getItem('jotcash.session'), null);

  lock();
  not('锁定后即失效', isUnlocked());
  equal('会话已从 sessionStorage 抹掉', sessionStorage.getItem('jotcash.session'), null);
});

//换口令与换口径都只改一个字段，不能把会话里其余的东西顺手冲掉
test('会话：改记账币种与改前端口令都只动一个字段', () => {
  unlock('后端口令', '前端口令', 'CNY');
  setAccountingCurrency('JPY');
  equal('币种已改', getAccountingCurrency(), 'JPY');
  equal('后端口令没动', getServerToken(), '后端口令');
  equal('前端口令没动', getClientToken(), '前端口令');

  setClientToken('新前端口令');
  equal('前端口令已改', getClientToken(), '新前端口令');
  equal('币种没被冲掉', getAccountingCurrency(), 'JPY');
  equal('后端口令没动', getServerToken(), '后端口令');
});

test('会话：未解锁时改什么都不该凭空造出一个会话', () => {
  lock();
  setAccountingCurrency('EUR');
  setClientToken('凭空口令');
  not('仍然未解锁', isUnlocked());
  equal('没有写出会话', sessionStorage.getItem('jotcash.session'), null);
});

test('列偏好：跨会话留在 localStorage，读坏了回默认列', () => {
  localStorage.clear();
  same('默认 10 列', getColumns(), EXPENSE_COLUMN_DEFAULT);
  not('默认列返回的是副本，改它不该污染常量', getColumns() === EXPENSE_COLUMN_DEFAULT);

  setColumns(['expense_date', 'remark']);
  same('存取往返', getColumns(), ['expense_date', 'remark']);

  localStorage.setItem('jotcash.columns', '{坏掉的 JSON');
  same('坏 JSON 回默认', getColumns(), EXPENSE_COLUMN_DEFAULT);
  localStorage.setItem('jotcash.columns', '[]');
  same('空数组回默认，否则表头一列都不剩', getColumns(), EXPENSE_COLUMN_DEFAULT);
});
