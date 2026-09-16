import test from 'node:test';
import './helper/browser.js';
import { equal, not, ok } from './helper/check.js';
import {
  AUTO_LOCK_DEFAULT,
  AUTO_LOCK_OPTIONS,
  checkAutoLock,
  getAutoLock,
  getLastActivity,
  initAutoLock,
  recordActivity,
  resetAutoLockTimer,
  setAutoLock,
  setLastActivity,
  stopAutoLockTimer,
} from '../static/js/auto_lock.js';
import { isUnlocked, lock, unlock } from '../static/js/store.js';

test('自动锁定：选项包含5分钟/10分钟/30分钟/1小时/永不自动锁定，默认5分钟', () => {
  localStorage.removeItem('jotcash.auto_lock');
  setAutoLock(5);
  equal('默认5分钟自动锁定', getAutoLock(), AUTO_LOCK_DEFAULT);
  equal('选项数量', AUTO_LOCK_OPTIONS.length, 5);
  equal('第一项是5分钟', AUTO_LOCK_OPTIONS[0].value, 5);
  equal('第二项是10分钟', AUTO_LOCK_OPTIONS[1].value, 10);
  equal('第三项是30分钟', AUTO_LOCK_OPTIONS[2].value, 30);
  equal('第四项是1小时', AUTO_LOCK_OPTIONS[3].value, 60);
  equal('最后一项是永不自动锁定', AUTO_LOCK_OPTIONS[4].value, 0);

  setAutoLock(10);
  equal('设置后生效', getAutoLock(), 10);
  equal('落盘 localStorage', localStorage.getItem('jotcash.auto_lock'), '10');

  // 非法值回退到默认
  setAutoLock(999);
  equal('非法选项回落到默认5分钟', getAutoLock(), 5);

  setAutoLock(0);
  stopAutoLockTimer();
});

test('自动锁定：超时判定与锁屏触发', () => {
  let lockedCount = 0;
  initAutoLock(() => {
    lockedCount += 1;
  });

  unlock('server', 'client', 'CNY', 'mock');
  ok('当前已解锁', isUnlocked());

  setAutoLock(5); // 5 分钟
  recordActivity();
  const now = Date.now();
  ok('最近活跃时间已更新', getLastActivity() >= now - 100);

  // 活跃时间内不触发
  checkAutoLock();
  equal('活跃时间内不锁定', lockedCount, 0);

  // 模拟超时：设置为 5 分钟又 1 秒前
  setLastActivity(Date.now() - 5 * 60 * 1000 - 1000);
  checkAutoLock();
  equal('超时后触发锁屏回调', lockedCount, 1);

  // 锁屏状态下不再触发
  lock();
  not('当前已锁定', isUnlocked());
  setLastActivity(Date.now() - 5 * 60 * 1000 - 1000);
  checkAutoLock();
  equal('已锁定状态下不重复触发', lockedCount, 1);

  setAutoLock(0);
  stopAutoLockTimer();
});
