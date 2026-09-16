import * as api from './api.js';
import { refreshChartTheme } from './chart.js';
import { resetCandidate } from './expense_inquiry.js';
import { LANGS, getLang, setLang, t } from './i18n.js';
import { render as renderExpense } from './page_expense.js';
import { render as renderFileMeta } from './page_file_meta.js';
import { render as renderOperationLog } from './page_operation_log.js';
import { render as renderSetting } from './page_setting.js';
import { render as renderStatistic } from './page_statistic.js';
import { renderUnlock } from './page_unlock.js';
import { renderNotice } from './notice.js';
import { AUTO_LOCK_OPTIONS, getAutoLock, initAutoLock, setAutoLock, watchAutoLock } from './auto_lock.js';
import { getAccountingCurrency, isUnlocked, lock } from './store.js';
import { THEMES, applyTheme, getTheme, setTheme, watchSystemTheme } from './theme.js';
import { clear, el, query } from './util.js';

const ROUTES = [
  { path: '/expense', name: '明细', render: renderExpense },
  { path: '/statistic', name: '统计', render: renderStatistic },
  { path: '/operation-log', name: '审计', render: renderOperationLog },
  { path: '/file-meta', name: '文件', render: renderFileMeta },
  { path: '/setting', name: '设置', render: renderSetting },
];

//hash 不进请求路径，反代加不加前缀都不影响路由
function parseHash() {
  const hash = location.hash.replace(/^#/, '') || '/expense';
  const [path, search] = hash.split('?');
  const params = {};
  for (const pair of (search || '').split('&')) {
    if (!pair) continue;
    const index = pair.indexOf('=');
    const key = index < 0 ? pair : pair.slice(0, index);
    params[decodeURIComponent(key)] = index < 0 ? '' : decodeURIComponent(pair.slice(index + 1));
  }
  return { path, params };
}

function renderNav(activePath) {
  const nav = query('#nav-host');
  clear(nav);
  for (const route of ROUTES) {
    nav.appendChild(el('li', { class: 'nav-item' }, [
      el('a', {
        class: `nav-link ${route.path === activePath ? 'active' : ''}`,
        href: `#${route.path}`,
        text: t(route.name),
      }),
    ]));
  }
}

function renderSession() {
  const host = query('#session-host');
  clear(host);
  if (!isUnlocked()) return;
  host.appendChild(el('span', { class: 'badge text-bg-light', text: t('记账币种 {currency}', { currency: getAccountingCurrency() }) }));
  const autoLockSelect = preferenceSelect(
    AUTO_LOCK_OPTIONS.map((opt) => ({ value: opt.value, name: t(opt.name) })),
    getAutoLock(),
    t('自动锁定'),
    (value) => {
      setAutoLock(Number(value));
    },
    'ms-2',
  );
  host.appendChild(autoLockSelect);
  host.appendChild(el('button', {
    class: 'btn btn-sm btn-outline-light ms-2',
    type: 'button',
    text: t('锁定'),
    onclick: () => {
      lock();
      location.reload();
    },
  }));
}

//name 传进来就是最终文案：主题名要翻译，语言名恰恰不能翻译
function preferenceSelect(options, value, title, onChange, extraClass) {
  const node = el('select', { class: `form-select form-select-sm w-auto ${extraClass || ''}`.trim(), title });
  for (const option of options) {
    node.appendChild(el('option', { value: option.value, selected: String(option.value) === String(value) ? true : null }, option.name));
  }
  node.addEventListener('change', () => onChange(node.value));
  return node;
}

//这两个下拉不跟解锁走：看不懂中文的人得先能把语言换掉，才轮得到输口令
function renderPreference() {
  const host = query('#pref-host');
  clear(host);
  host.appendChild(preferenceSelect(
    THEMES.map((theme) => ({ value: theme.value, name: t(theme.name) })),
    getTheme(),
    t('主题'),
    (value) => {
      setTheme(value);
      applyTheme();
      //Chart.js 的默认色是建实例时读的，得先换默认色再整屏重绘，否则图表还是上一套配色
      refreshChartTheme();
      renderRoute();
    },
  ));
  host.appendChild(preferenceSelect(
    LANGS,
    getLang(),
    t('语言'),
    (value) => {
      setLang(value);
      applyLang();
      //mock 的种子数据是演示内容，跟着界面语言走；筛选按库里存的值匹配，所以得先重写再重绘。
      //候选是累积的，一并清掉，免得下拉里中英文各挂一份
      api.relocalizeMock();
      resetCandidate();
      renderRoute();
    },
  ));
}

//语言落到 html 上，屏幕阅读器与浏览器的翻译提示都认它
function applyLang() {
  document.documentElement.setAttribute('lang', getLang() === 'zh' ? 'zh-CN' : 'en');
  document.title = t('jotcash · 记账');
}

//换语言、换主题都走这里整屏重来，而不是刷新页面：
//mock 模式下刷新会把内存库连同这一轮的改动一起清掉
function renderRoute() {
  const host = query('#page-host');
  renderNotice(isUnlocked());
  renderPreference();
  if (!isUnlocked()) {
    query('#nav-wrap').hidden = true;
    renderSession();
    renderUnlock(host, () => {
      //解锁成功后回到路由，nav 才跟着显出来
      renderRoute();
    });
    return;
  }
  query('#nav-wrap').hidden = false;

  const { path, params } = parseHash();
  const route = ROUTES.find((item) => item.path === path) || ROUTES[0];
  renderNav(route.path);
  renderSession();
  route.render(host, params);
}

function start() {
  //刷新会把 mock 的内存库清空，而会话是活过刷新的：会话还说着 mock，就得把演示数据重新播一遍
  api.seedMock();
  applyTheme();
  applyLang();
  watchSystemTheme(() => {
    refreshChartTheme();
    renderRoute();
  });
  window.addEventListener('hashchange', renderRoute);
  initAutoLock(() => {
    if (!isUnlocked()) return;
    lock();
    location.reload();
  });
  watchAutoLock(() => {
    if (isUnlocked()) {
      renderSession();
    }
  });
  renderRoute();
}

start();
