import * as api from './api.js';
import { MODE_DEFAULT, MODE_MOCK, MODE_REAL, TOKEN_MIN_LEN } from './config.js';
import { currencySelect } from './component.js';
import { defaultCurrency, t } from './i18n.js';
import { lock, unlock } from './store.js';
import { clear, el, toastErr } from './util.js';

//解锁模式即这一门会话的数据来源，一旦定了就到锁定为止都不再变
const MODES = [
  { value: MODE_REAL, name: '真实后端', hint: '打后端的真实接口，读写已部署的加密数据库，两把口令都必须是真的' },
  { value: MODE_MOCK, name: 'mock 试用', hint: '数据全在浏览器内存里，不发任何请求，刷新页面即复位，口令随便填' },
];

//口令输入框统一带一个明文开关：口令是手抄来的，看不见更容易抄错
//透传 id / name / autocomplete 等标准凭据属性，以便密码自动填充工具将其识别为账户名与当前密码
function tokenInput(placeholder, attrs = {}) {
  const input = el('input', {
    class: 'form-control',
    type: 'password',
    placeholder,
    autocomplete: attrs.autocomplete || 'off',
    name: attrs.name || null,
    id: attrs.id || null,
    autocapitalize: 'none',
    autocorrect: 'off',
    spellcheck: 'false',
  });
  const toggle = el('button', { class: 'btn btn-outline-secondary', type: 'button', text: t('显示') });
  toggle.addEventListener('click', () => {
    const shown = input.type === 'text';
    input.type = shown ? 'password' : 'text';
    toggle.textContent = shown ? t('显示') : t('隐藏');
  });
  return { input, node: el('div', { class: 'input-group' }, [input, toggle]) };
}

//单选而不是下拉：两个选项各自带一句说明，下拉塞不下这句说明，而这句说明正是选它的依据
function modeChoice(onChange) {
  let value = MODE_DEFAULT;
  const node = el('div', { class: 'vstack gap-1' });
  for (const mode of MODES) {
    const id = `unlock-mode-${mode.value}`;
    const input = el('input', {
      class: 'form-check-input',
      type: 'radio',
      name: 'unlock-mode',
      id,
      value: mode.value,
      checked: mode.value === value ? true : null,
      onchange: () => {
        if (!input.checked) return;
        value = mode.value;
        onChange(value);
      },
    });
    node.appendChild(el('div', { class: 'form-check' }, [
      input,
      el('label', { class: 'form-check-label', for: id }, [
        el('span', { text: t(mode.name) }),
        el('span', { class: 'd-block form-text mt-0', text: t(mode.hint) }),
      ]),
    ]));
  }
  return { node, value: () => value };
}

export function renderUnlock(container, onUnlocked) {
  const serverToken = tokenInput(t('后端口令，用于签发请求凭据'), {
    id: 'server-token',
    name: 'username',
    autocomplete: 'username',
  });
  const clientToken = tokenInput(t('前端口令，{min} 位起，不得纯数字或纯字母', { min: TOKEN_MIN_LEN }), {
    id: 'client-token',
    name: 'password',
    autocomplete: 'current-password',
  });
  //解锁页按定义就是没有会话的状态，记账币种一定没设过，初值只能由语言来定
  const currency = currencySelect(defaultCurrency(), { class: 'form-select' });
  const submit = el('button', { class: 'btn btn-primary w-100', type: 'submit', text: t('解锁') });
  const mockNotice = el('div', {
    class: 'alert alert-warning py-2 small mb-0',
    hidden: MODE_DEFAULT === MODE_MOCK ? null : true,
    text: t('mock 模式：数据全在浏览器内存里，不会发任何请求，刷新页面即复位。口令随便填即可进入。'),
  });
  const mode = modeChoice((value) => { mockNotice.hidden = value === MODE_MOCK ? null : true; });

  const form = el('form', { class: 'vstack gap-3', autocomplete: 'on' }, [
    el('div', {}, [el('label', { class: 'form-label small text-secondary', text: t('解锁模式') }), mode.node]),
    mockNotice,
    el('div', {}, [el('label', { class: 'form-label small text-secondary', for: 'server-token', text: t('后端口令') }), serverToken.node]),
    el('div', {}, [el('label', { class: 'form-label small text-secondary', for: 'client-token', text: t('前端口令') }), clientToken.node]),
    el('div', {}, [
      el('label', { class: 'form-label small text-secondary', text: t('记账币种') }),
      currency,
      el('div', { class: 'form-text', text: t('逐请求携带，决定本次会话新增数据的记账口径；已有数据的口径要走「记账币种切换」') }),
    ]),
    submit,
  ]);

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const server = serverToken.input.value.trim();
    const client = clientToken.input.value.trim();
    if (!server || !client) {
      toastErr(new Error(t('两个口令都要填')));
      return;
    }
    submit.disabled = true;
    submit.textContent = t('校验中');
    unlock(server, client, currency.value, mode.value());
    try {
      //mock 模式的库是空的，探针之前先把演示数据播进去；真实模式下这一句自己会让开
      api.seedMock();
      //只读探针，不签发会话、不记审计；口令错与文件损坏在这里是同一种失败
      await api.ping();
      onUnlocked();
    } catch (err) {
      lock();
      toastErr(err);
    } finally {
      submit.disabled = false;
      submit.textContent = t('解锁');
    }
  });

  clear(container).appendChild(el('div', { class: 'unlock-wrap' }, [
    el('div', { class: 'card shadow-sm unlock-card' }, [
      el('div', { class: 'card-body p-4' }, [
        el('h4', { class: 'mb-1', text: 'jotcash' }),
        el('p', { class: 'text-secondary small mb-4', text: t('个人记账 · 只记支出 · 整库加密 · 无登录态') }),
        form,
        el('hr', { class: 'my-4' }),
        el('ul', { class: 'text-secondary small mb-0 ps-3' }, [
          el('li', { text: t('两把口令都只存在本标签页的 sessionStorage，关掉标签页即失效') }),
          el('li', { text: t('初始口令在服务首次启动时打印到日志，只打印一次，错过就只能删库重来') }),
          el('li', { text: t('口令丢失等于数据永久不可读，没有找回路径，请先做一次数据库导出备份') }),
        ]),
      ]),
    ]),
  ]));
}
