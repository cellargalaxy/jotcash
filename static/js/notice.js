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

//展示完整地址，query 与 hash 一个都不能少。
//去掉 query 的话，像 IDE 内置服务那种把授权 token 放在 query 里的地址，
//复制出去到别的浏览器就打不开了——之前正是这么翻的车
function pageUrl() {
  return location.href;
}

//复制走 clipboard API，非安全上下文下它不可用，退回到老办法
async function copyUrl() {
  const url = pageUrl();
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(url);
      return true;
    }
  } catch (err) {
    //权限被拒就走下面的兜底，不该把整条公告带崩
  }
  const area = el('textarea', { class: 'notice-copy-area' });
  area.value = url;
  document.body.appendChild(area);
  area.select();
  let copied = false;
  try {
    copied = document.execCommand('copy');
  } catch (err) {
    copied = false;
  }
  area.remove();
  return copied;
}

function copyButton() {
  const button = el('button', { class: 'btn btn-sm btn-outline-secondary py-0 px-2', type: 'button', text: '复制' });
  button.addEventListener('click', async () => {
    button.textContent = (await copyUrl()) ? '已复制' : '复制失败';
    setTimeout(() => { button.textContent = '复制'; }, 2000);
  });
  return button;
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
      copyButton(),
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
