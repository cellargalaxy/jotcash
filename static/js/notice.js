import { clear, el, query } from './util.js';

//公告被关掉之后就不再出现，免得每次切页面又弹一次
let dismissed = false;
let hideTimer = 0;

const AUTO_HIDE_DELAY = 6000;

function isLoopback() {
  return ['localhost', '127.0.0.1', '::1', '[::1]'].includes(location.hostname);
}

//浏览器不把证书信息暴露给 JS：证书不被信任时会拦在页面之前，用户点了「继续前往」之后
//页面里读不到这件事。能拿到的唯一间接信号是安全上下文——协议是 https 却不是安全上下文，
//说明这条链路没被浏览器当成可信的。拿不到更多就到此为止，不编
//取值直接用 Bootstrap 的语义色名，省掉一层映射，也就不会再拼出 alert-safe 这种不存在的类
function checkLevel() {
  if (location.protocol !== 'https:') return 'danger';
  return window.isSecureContext ? 'success' : 'warning';
}

function noticeText(level) {
  switch (level) {
    case 'success':
      return '连接已加密。口令只存在本标签页的 sessionStorage，传输过程受 TLS 保护。';
    case 'warning':
      return '协议是 HTTPS，但浏览器没有把这个页面当作安全上下文——证书很可能不被信任。口令仍有被窃取的风险，请先确认证书。';
    default:
      return isLoopback()
        ? '连接未加密。当前是本机地址，请求不出网卡，风险有限；但部署到服务器后必须改用 HTTPS，否则口令会以明文过网。'
        : '连接未加密！口令会以明文在网络上传输，链路上任何一个节点都能看到它，请立刻改用 HTTPS 访问。';
    }
}

//展示的是地址本身，不带 hash——hash 是前端路由，跟着点来点去一直变，也与安全性无关
function pageUrl() {
  return location.origin + location.pathname;
}

export function renderNotice(unlocked) {
  const host = query('#notice-host');
  clear(host);
  if (dismissed) return;

  const level = checkLevel();
  //只有真正安全的那一档才允许关掉与自动消失；不安全的必须一直杵在那儿。
  //解锁页上一律不给关，让用户在输口令之前先看清自己走的是什么协议
  const closable = unlocked && level === 'success';

  const node = el('div', { class: `alert alert-${level} notice-bar mb-0 rounded-0 py-2` }, [
    el('div', { class: 'container-fluid d-flex flex-wrap align-items-center gap-2' }, [
      el('span', { class: 'badge text-bg-light text-uppercase', text: location.protocol.replace(':', '') }),
      el('code', { class: 'notice-url', text: pageUrl() }),
      el('span', { class: 'small', text: noticeText(level) }),
      closable
        ? el('button', {
          class: 'btn-close ms-auto',
          type: 'button',
          'aria-label': '关闭',
          onclick: () => {
            dismissed = true;
            clear(host);
          },
        })
        : null,
    ]),
  ]);
  host.appendChild(node);

  if (closable && !hideTimer) {
    hideTimer = setTimeout(() => {
      dismissed = true;
      clear(host);
    }, AUTO_HIDE_DELAY);
  }
}
