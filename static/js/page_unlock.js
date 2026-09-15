import * as api from './api.js';
import { CURRENCY_DEFAULT, TOKEN_MIN_LEN, USE_MOCK } from './config.js';
import { currencySelect } from './component.js';
import { getAccountingCurrency, lock, unlock } from './store.js';
import { clear, el, toastErr } from './util.js';

//口令输入框统一带一个明文开关：口令是手抄来的，看不见更容易抄错
function tokenInput(placeholder) {
  const input = el('input', { class: 'form-control', type: 'password', placeholder, autocomplete: 'off' });
  const toggle = el('button', { class: 'btn btn-outline-secondary', type: 'button', text: '显示' });
  toggle.addEventListener('click', () => {
    const shown = input.type === 'text';
    input.type = shown ? 'password' : 'text';
    toggle.textContent = shown ? '显示' : '隐藏';
  });
  return { input, node: el('div', { class: 'input-group' }, [input, toggle]) };
}

export function renderUnlock(container, onUnlocked) {
  const serverToken = tokenInput('后端口令，用于签发请求凭据');
  const clientToken = tokenInput(`前端口令，${TOKEN_MIN_LEN} 位起，不得纯数字或纯字母`);
  const currency = currencySelect(getAccountingCurrency() || CURRENCY_DEFAULT, { class: 'form-select' });
  const submit = el('button', { class: 'btn btn-primary w-100', type: 'submit', text: '解锁' });

  const form = el('form', { class: 'vstack gap-3' }, [
    el('div', {}, [el('label', { class: 'form-label small text-secondary', text: '后端口令' }), serverToken.node]),
    el('div', {}, [el('label', { class: 'form-label small text-secondary', text: '前端口令' }), clientToken.node]),
    el('div', {}, [
      el('label', { class: 'form-label small text-secondary', text: '记账币种' }),
      currency,
      el('div', { class: 'form-text', text: '逐请求携带，决定本次会话新增数据的记账口径；已有数据的口径要走「记账币种切换」' }),
    ]),
    submit,
  ]);

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const server = serverToken.input.value.trim();
    const client = clientToken.input.value.trim();
    if (!server || !client) {
      toastErr(new Error('两个口令都要填'));
      return;
    }
    submit.disabled = true;
    submit.textContent = '校验中';
    unlock(server, client, currency.value);
    try {
      //只读探针，不签发会话、不记审计；口令错与文件损坏在这里是同一种失败
      await api.ping();
      onUnlocked();
    } catch (err) {
      lock();
      toastErr(err);
    } finally {
      submit.disabled = false;
      submit.textContent = '解锁';
    }
  });

  clear(container).appendChild(el('div', { class: 'unlock-wrap' }, [
    el('div', { class: 'card shadow-sm unlock-card' }, [
      el('div', { class: 'card-body p-4' }, [
        el('h4', { class: 'mb-1', text: 'jotcash' }),
        el('p', { class: 'text-secondary small mb-4', text: '个人记账 · 只记支出 · 整库加密 · 无登录态' }),
        USE_MOCK ? el('div', { class: 'alert alert-warning py-2 small', text: '当前是 mock 模式：数据全在浏览器内存里，不会发任何请求，刷新页面即复位。口令随便填即可进入。' }) : null,
        form,
        el('hr', { class: 'my-4' }),
        el('ul', { class: 'text-secondary small mb-0 ps-3' }, [
          el('li', { text: '两把口令都只存在本标签页的 sessionStorage，关掉标签页即失效' }),
          el('li', { text: '初始口令在服务首次启动时打印到日志，只打印一次，错过就只能删库重来' }),
          el('li', { text: '口令丢失等于数据永久不可读，没有找回路径，请先做一次数据库导出备份' }),
        ]),
      ]),
    ]),
  ]));
}
