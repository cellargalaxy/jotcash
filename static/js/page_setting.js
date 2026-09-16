import * as api from './api.js';
import { API_BASE, TOKEN_MIN_LEN, USE_MOCK } from './config.js';
import { currencySelect, sectionCard } from './component.js';
import { t } from './i18n.js';
import { getAccountingCurrency, lock, setAccountingCurrency, setClientToken } from './store.js';
import { clear, confirmModal, download, el, toastErr, toastOk } from './util.js';

//与后端 tool.CheckToken 同一套判据：长度、空格、不得纯数字或纯字母
function checkToken(token) {
  if ([...token].length < TOKEN_MIN_LEN) return t('口令长度不足{min}位', { min: TOKEN_MIN_LEN });
  if (token.includes(' ')) return t('口令不能包含空格');
  let hasDigit = false;
  let hasOther = false;
  for (const char of token) {
    if (char >= '0' && char <= '9') hasDigit = true;
    else hasOther = true;
  }
  if (!hasDigit || !hasOther) return t('口令不能为纯数字或纯字母');
  return '';
}

function currencyCard(onDone) {
  const currentSelect = currencySelect(getAccountingCurrency(), { class: 'form-select form-select-sm' });
  const targetSelect = currencySelect(getAccountingCurrency(), { class: 'form-select form-select-sm' });
  const distribution = el('div', { class: 'small text-secondary mb-3', text: t('正在读取全库记账币种…') });

  //服务端只告诉「现在有哪些记账币种」，不提供每个币种的笔数
  api.selectDistinct('accounting_currency')
    .then((result) => {
      const list = result.object || [];
      distribution.textContent = list.length === 0 ? t('全库暂无明细') : t('全库记账币种：{codes}', { codes: list.join(t('、')) });
    })
    .catch(() => {
      distribution.textContent = t('记账币种集合接口尚未实现，联调后这里会显示全库有哪些记账币种');
    });

  return sectionCard(
    t('记账口径'),
    t('记账币种逐请求携带，不落库。改这里只影响本会话「新增数据」的记账口径；已有数据要改口径，必须执行记账币种切换。'),
    [
      distribution,
      el('div', { class: 'row g-3 align-items-end' }, [
        el('div', { class: 'col-12 col-md-4' }, [
          el('label', { class: 'form-label small text-secondary', text: t('本会话记账币种') }),
          currentSelect,
        ]),
        el('div', { class: 'col-12 col-md-2' }, [
          el('button', {
            class: 'btn btn-sm btn-outline-primary w-100',
            type: 'button',
            text: t('保存本会话口径'),
            onclick: () => {
              setAccountingCurrency(currentSelect.value);
              toastOk(t('本会话记账币种已改为 {currency}', { currency: currentSelect.value }));
              onDone();
            },
          }),
        ]),
        el('div', { class: 'col-12 col-md-4' }, [
          el('label', { class: 'form-label small text-secondary', text: t('记账币种切换的目标币种') }),
          targetSelect,
        ]),
        el('div', { class: 'col-12 col-md-2' }, [
          el('button', {
            class: 'btn btn-sm btn-warning w-100',
            type: 'button',
            text: t('执行切换'),
            onclick: async () => {
              const target = targetSelect.value;
              const body = el('div', { class: 'small' }, [
                t('筛选记账币种 ≠ {target} 的全部明细（含已删除），逐笔按其支出日期重新获取汇率并无条件覆盖，同步重算记账金额。', { target }),
                el('br'),
                t('单笔同事务，允许部分失败，可中断，重试即续跑。'),
              ]);
              if (!(await confirmModal(t('记账币种切换'), body))) return;
              try {
                const result = await api.switchAccountingCurrency(target);
                toastOk(t('切换完成，成功 {done} 笔，失败 {failed} 笔', { done: result.object.done, failed: result.object.failed }));
                onDone();
              } catch (err) {
                toastErr(err);
              }
            },
          }),
        ]),
      ]),
    ],
  );
}

function tokenCard() {
  const newToken = el('input', { class: 'form-control form-control-sm', type: 'password', autocomplete: 'off', placeholder: t('{min} 位起，不得纯数字或纯字母，不得含空格', { min: TOKEN_MIN_LEN }) });
  const repeatToken = el('input', { class: 'form-control form-control-sm', type: 'password', autocomplete: 'off', placeholder: t('再输一次') });

  return sectionCard(
    t('更换口令'),
    t('更换口令等价于更换密钥：服务端副本改密 → 用新口令校验能打开 → 原子替换，任一步失败原库原封不动。换之前请先导出一份当前数据库作为二次兜底。'),
    [
      el('div', { class: 'row g-3 align-items-end' }, [
        el('div', { class: 'col-12 col-md-4' }, [el('label', { class: 'form-label small text-secondary', text: t('新口令') }), newToken]),
        el('div', { class: 'col-12 col-md-4' }, [el('label', { class: 'form-label small text-secondary', text: t('确认新口令') }), repeatToken]),
        el('div', { class: 'col-12 col-md-3' }, [
          el('button', {
            class: 'btn btn-sm btn-danger w-100',
            type: 'button',
            text: t('更换口令'),
            onclick: async () => {
              const token = newToken.value;
              if (token !== repeatToken.value) {
                toastErr(new Error(t('两次输入的新口令不一致')));
                return;
              }
              const message = checkToken(token);
              if (message) {
                toastErr(new Error(message));
                return;
              }
              const body = el('div', { class: 'small' }, [
                t('口令即密钥，口令丢失等于数据永久不可读，没有找回路径。'),
                el('br'),
                t('确认之前，请确保已经导出过一份当前数据库。'),
              ]);
              if (!(await confirmModal(t('更换口令'), body, t('确认更换')))) return;
              try {
                await api.changeToken(token);
                //换完立刻以新口令继续，免得下一次请求还拿旧口令去开库
                setClientToken(token);
                newToken.value = '';
                repeatToken.value = '';
                toastOk(t('口令已更换，本会话已切到新口令'));
              } catch (err) {
                toastErr(err);
              }
            },
          }),
        ]),
      ]),
    ],
  );
}

function backupCard() {
  const importInput = el('input', { class: 'form-control form-control-sm', type: 'file', accept: '.db,application/octet-stream' });

  return sectionCard(
    t('数据库导入导出'),
    t('备份、迁移、换机、改密回滚共用这一对能力。导出的是当前加密态的一致性快照；导入是整库覆盖、不可逆。'),
    [
      el('div', { class: 'row g-3 align-items-end' }, [
        el('div', { class: 'col-12 col-md-3' }, [
          el('button', {
            class: 'btn btn-sm btn-outline-primary w-100',
            type: 'button',
            text: t('导出数据库'),
            onclick: async () => {
              try {
                const result = await api.exportDb();
                const object = result.object;
                download(object.file_name, object.blob || object.data, 'application/octet-stream');
                toastOk(t('已导出 {name}', { name: object.file_name }));
              } catch (err) {
                toastErr(err);
              }
            },
          }),
        ]),
        el('div', { class: 'col-12 col-md-6' }, [
          el('label', { class: 'form-label small text-secondary', text: t('导入数据库文件') }),
          importInput,
        ]),
        el('div', { class: 'col-12 col-md-3' }, [
          el('button', {
            class: 'btn btn-sm btn-danger w-100',
            type: 'button',
            text: t('导入并覆盖'),
            onclick: async () => {
              const file = importInput.files && importInput.files[0];
              if (!file) {
                toastErr(new Error(t('没有选择文件')));
                return;
              }
              const body = el('div', { class: 'small' }, [
                t('即将用 {name} 整库覆盖现有数据库，覆盖不可逆。', { name: file.name }),
                el('br'),
                t('上传的库必须能用本次请求的口令打开且结构合法，否则不会替换。'),
              ]);
              if (!(await confirmModal(t('导入数据库'), body, t('确认覆盖')))) return;
              try {
                await api.importDb(file);
                importInput.value = '';
                toastOk(t('数据库已导入'));
              } catch (err) {
                toastErr(err);
              }
            },
          }),
        ]),
      ]),
    ],
  );
}

function sessionCard() {
  return sectionCard(
    t('会话与运行信息'),
    t('无登录、无会话、无令牌：口令与记账币种逐请求携带，只存在本标签页的 sessionStorage 里。'),
    [
      el('dl', { class: 'row small mb-3' }, [
        el('dt', { class: 'col-4 col-md-3 text-secondary', text: t('页面地址') }),
        el('dd', { class: 'col-8 col-md-9 font-monospace text-break', text: location.href }),
        el('dt', { class: 'col-4 col-md-3 text-secondary', text: t('接口地址') }),
        //展示相对前缀解析之后的绝对地址：接口打到哪台服务上，这里一眼能看出来。
        //如果它不是你启动的那个服务，说明页面是被别的服务（比如 IDE 的内置预览）托管的
        el('dd', { class: 'col-8 col-md-9 font-monospace text-break', text: `${API_BASE} → ${new URL(API_BASE, location.href).href}` }),
        el('dt', { class: 'col-4 col-md-3 text-secondary', text: t('数据来源') }),
        el('dd', { class: 'col-8 col-md-9', text: USE_MOCK ? t('mock（浏览器内存，不发请求）') : t('真实后端接口') }),
        el('dt', { class: 'col-4 col-md-3 text-secondary', text: t('本会话记账币种') }),
        el('dd', { class: 'col-8 col-md-9', text: getAccountingCurrency() }),
      ]),
      el('button', {
        class: 'btn btn-sm btn-outline-secondary',
        type: 'button',
        text: t('锁定并清空本会话口令'),
        onclick: () => {
          lock();
          location.reload();
        },
      }),
    ],
  );
}

export function render(container) {
  const rerender = () => render(container);
  clear(container).appendChild(el('div', {}, [
    el('h5', { class: 'mb-3', text: t('设置') }),
    currencyCard(rerender),
    tokenCard(),
    backupCard(),
    sessionCard(),
  ]));
}
