import * as api from './api.js';
import { USE_MOCK } from './config.js';
import { render as renderExpense } from './page_expense.js';
import { render as renderFileMeta } from './page_file_meta.js';
import { render as renderOperationLog } from './page_operation_log.js';
import { render as renderSetting } from './page_setting.js';
import { renderUnlock } from './page_unlock.js';
import { renderNotice } from './notice.js';
import { getAccountingCurrency, isUnlocked, lock } from './store.js';
import { clear, el, query } from './util.js';

const ROUTES = [
  { path: '/expense', name: '明细', render: renderExpense },
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
        text: route.name,
      }),
    ]));
  }
}

function renderSession() {
  const host = query('#session-host');
  clear(host);
  if (!isUnlocked()) return;
  host.appendChild(el('span', { class: 'badge text-bg-light', text: `记账币种 ${getAccountingCurrency()}` }));
  host.appendChild(el('button', {
    class: 'btn btn-sm btn-outline-light ms-2',
    type: 'button',
    text: '锁定',
    onclick: () => {
      lock();
      location.reload();
    },
  }));
}

function renderRoute() {
  const host = query('#page-host');
  renderNotice(isUnlocked());
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
  if (USE_MOCK) api.seedMock();
  window.addEventListener('hashchange', renderRoute);
  renderRoute();
}

start();
