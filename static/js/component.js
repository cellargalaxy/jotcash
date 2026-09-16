import { CURRENCIES, PAGE_SIZES } from './config.js';
import { t } from './i18n.js';
import { clear, el, formatAmount, formatDate, formatDateTime, formatMonth } from './util.js';

//筛选区的一格：统一标签与控件的排布，免得每个页面各写一套栅格
export function filterItem(label, control, width, hint) {
  return el('div', { class: `filter-item col-12 col-sm-6 col-lg-${width || 3}` }, [
    el('label', { class: 'form-label small text-secondary mb-1', text: label }),
    control,
    hint ? el('div', { class: 'form-text small mt-1', text: hint }) : null,
  ]);
}

export function textInput(attrs) {
  return el('input', { class: 'form-control form-control-sm', type: 'text', ...attrs });
}

export function dateInput(attrs) {
  return el('input', { class: 'form-control form-control-sm', type: 'date', ...attrs });
}

export function numberInput(attrs) {
  return el('input', { class: 'form-control form-control-sm', type: 'number', ...attrs });
}

//下拉的候选一律来自配置枚举，展示名就是词条原文，所以在这里统一转译。
//用户录入的取值走的是组合框那条路，不经过这里——那些是数据，翻译它才是错的
export function select(options, value, attrs) {
  const node = el('select', { class: 'form-select form-select-sm', ...attrs });
  for (const option of options) {
    node.appendChild(el('option', { value: option.value, selected: String(option.value) === String(value) ? true : null }, t(option.name)));
  }
  return node;
}

//候选项允许写成纯字符串，也允许写成 {value,name} 让下拉显示得更全
function comboOption(item) {
  return typeof item === 'string' ? { value: item, name: item } : item;
}

//多选筛选框里，点下拉是往已有取值后面追加一项，不是把前面选的顶掉
function appendValue(current, value) {
  const values = (current || '').split(',').map((item) => item.trim()).filter((item) => item !== '');
  if (!values.includes(value)) values.push(value);
  return values.join(',');
}

//组合框：左边是能自由录入的输入框，右边挂一个下拉给已有候选。
//候选传的是取值函数而不是快照——候选要等接口回来才有，传快照的话首次渲染永远是空的
function buildCombo(getOptions, value, attrs, append) {
  const input = el('input', { class: 'form-control form-control-sm', type: 'text', value: value || '', ...attrs });
  const toggle = el('button', {
    class: 'btn btn-outline-secondary dropdown-toggle',
    type: 'button',
    'data-bs-toggle': 'dropdown',
    'aria-expanded': 'false',
  });
  const menu = el('ul', { class: 'dropdown-menu dropdown-menu-end combo-menu' });
  const node = el('div', { class: 'input-group input-group-sm' }, [input, toggle, menu]);

  //菜单必须用 fixed 定位。编辑器嵌在 .table-responsive 里，那层是 overflow-x:auto，
  //按 CSS 规范另一个方向的 visible 会被一并算成 auto——菜单一展开就把容器撑出纵向滚动条，
  //表格随之变窄、页面跳一下。fixed 定位的菜单不参与祖先的滚动区计算，撑不出滚动条
  new bootstrap.Dropdown(toggle, {
    popperConfig: (config) => ({ ...config, strategy: 'fixed' }),
  });

  //展开时才建菜单：候选会随着用户录入新值而变，建一次就对不上了
  node.addEventListener('show.bs.dropdown', () => {
    clear(menu);
    const options = (getOptions() || []).map(comboOption);
    if (options.length === 0) {
      menu.appendChild(el('li', {}, [el('span', { class: 'dropdown-item-text small text-secondary', text: t('暂无候选') })]));
      return;
    }
    for (const option of options) {
      menu.appendChild(el('li', {}, [
        el('button', {
          class: 'dropdown-item small',
          type: 'button',
          text: option.name,
          onclick: () => {
            input.value = append ? appendValue(input.value, option.value) : option.value;
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new Event('change', { bubbles: true }));
          },
        }),
      ]));
    }
  });
  return { node, input };
}

//单选：点下拉直接替换输入框的值
export function comboInput(getOptions, value, attrs) {
  return buildCombo(getOptions, value, attrs, false);
}

//多选筛选：点下拉往逗号分隔的取值后面追加
export function comboFilterInput(getOptions, value, attrs) {
  return buildCombo(getOptions, value, attrs, true);
}

//币种控件：常用币种走下拉，罕见币种允许直接敲三位代码，后端认的是 ISO 4217 全集
export function currencyOptions(extra) {
  const options = CURRENCIES.map((currency) => ({ value: currency.code, name: `${currency.code} ${t(currency.name)}` }));
  const known = new Set(CURRENCIES.map((currency) => currency.code));
  for (const code of extra || []) {
    if (code && !known.has(code)) {
      known.add(code);
      options.push({ value: code, name: code });
    }
  }
  return options;
}

export function currencyInput(value, attrs) {
  return comboInput(() => currencyOptions(), value, {
    class: 'form-control form-control-sm text-uppercase',
    maxlength: '3',
    placeholder: t('币种代码'),
    ...attrs,
  });
}

export function currencySelect(value, attrs) {
  return select(CURRENCIES.map((currency) => ({ value: currency.code, name: `${currency.code} ${t(currency.name)}` })), value, attrs);
}

//多选用复选框组而不是 multiple select：条数少、看得见、点得准
export function checkGroup(options, values, onChange) {
  const chosen = new Set(values || []);
  const node = el('div', { class: 'd-flex flex-wrap gap-2 pt-1' });
  for (const option of options) {
    const id = `check-${option}-${Math.random().toString(36).slice(2, 8)}`;
    const input = el('input', {
      class: 'form-check-input',
      type: 'checkbox',
      id,
      checked: chosen.has(option) ? true : null,
      onchange: () => {
        if (input.checked) chosen.add(option);
        else chosen.delete(option);
        onChange([...chosen]);
      },
    });
    node.appendChild(el('div', { class: 'form-check form-check-inline me-0' }, [
      input,
      el('label', { class: 'form-check-label small', for: id, text: t(option) }),
    ]));
  }
  return node;
}

//分页条挂在页面最底部，原生 select 的选项会展开到视口外点不着，
//换成向上展开的下拉，Popper 还会在空间不够时自己翻面
function pageSizeDropdown(pageSize, onChange) {
  const menu = el('ul', { class: 'dropdown-menu dropdown-menu-end' });
  for (const size of PAGE_SIZES) {
    menu.appendChild(el('li', {}, [
      el('button', {
        class: `dropdown-item small ${size === pageSize ? 'active' : ''}`,
        type: 'button',
        text: t('{size} 条/页', { size }),
        onclick: () => onChange({ page: 1, page_size: size }),
      }),
    ]));
  }
  return el('div', { class: 'btn-group btn-group-sm dropup' }, [
    el('button', {
      class: 'btn btn-outline-secondary dropdown-toggle',
      type: 'button',
      'data-bs-toggle': 'dropdown',
      'aria-expanded': 'false',
      text: t('{size} 条/页', { size: pageSize }),
    }),
    menu,
  ]);
}

//分页条：后端 page 从 1 起，pageSize 上限 200
export function pager(state, count, onChange) {
  const pageSize = state.page_size;
  const page = state.page || 1;
  const total = Math.max(1, Math.ceil(count / pageSize));
  const button = (text, target, disabled) => el('button', {
    class: 'btn btn-sm btn-outline-secondary',
    type: 'button',
    disabled: disabled ? true : null,
    onclick: () => onChange({ page: target }),
    text,
  });
  return el('div', { class: 'd-flex flex-wrap align-items-center gap-2 py-2' }, [
    el('span', { class: 'text-secondary small', text: t('共 {count} 条 · 第 {page}/{total} 页', { count, page, total }) }),
    el('div', { class: 'btn-group btn-group-sm ms-auto' }, [
      button(t('首页'), 1, page <= 1),
      button(t('上一页'), page - 1, page <= 1),
      button(t('下一页'), page + 1, page >= total),
      button(t('末页'), total, page >= total),
    ]),
    pageSizeDropdown(pageSize, onChange),
  ]);
}

export function emptyRow(colspan, text) {
  return el('tr', {}, [el('td', { colspan, class: 'text-center text-secondary py-4', text: text || t('没有匹配的数据') })]);
}

export function loadingRow(colspan) {
  return el('tr', {}, [el('td', { colspan, class: 'text-center text-secondary py-4' }, [
    el('span', { class: 'spinner-border spinner-border-sm me-2' }),
    t('加载中'),
  ])]);
}

//ID 是 16 位十进制，展示成字符串免得被误当数字做计算
export function idText(value) {
  return value ? String(value) : '';
}

//按字段类型渲染只读单元格，展示口径全仓只有这一处
export function fieldText(field, row) {
  const value = row[field.key];
  switch (field.type) {
    case 'date':
      return formatDate(value);
    case 'month':
      return formatMonth(value);
    case 'datetime':
      return formatDateTime(value);
    case 'amount':
      return formatAmount(value);
    case 'rate':
      //汇率有自己的有效位数，补成金额精度反而失真
      return value === null || value === undefined ? '' : String(value);
    case 'id':
      return idText(value);
    default:
      return value === null || value === undefined ? '' : String(value);
  }
}

export function badge(text, level) {
  return el('span', { class: `badge text-bg-${level || 'secondary'}`, text });
}

//审计结果只有三种，颜色固定，一眼能分出成功与部分成功。
//颜色认后端原文、文案走转译：取值是契约，展示才是文案
export function resultBadge(result) {
  const level = result === '成功' ? 'success' : result === '失败' ? 'danger' : 'warning';
  return badge(t(result), level);
}

//筛选条件在手机上会占满一屏，折叠起来才看得到表格
export function filterCard(form) {
  const id = `filter-${Math.random().toString(36).slice(2, 8)}`;
  return el('div', { class: 'card mb-3' }, [
    el('div', { class: 'card-header py-2 d-flex align-items-center' }, [
      el('span', { class: 'small fw-semibold', text: t('筛选条件') }),
      el('button', {
        class: 'btn btn-sm btn-link ms-auto p-0 text-decoration-none',
        type: 'button',
        'data-bs-toggle': 'collapse',
        'data-bs-target': `#${id}`,
        text: t('展开 / 收起'),
      }),
    ]),
    el('div', { class: 'collapse show', id }, [el('div', { class: 'card-body py-3' }, [form])]),
  ]);
}

export function sectionCard(title, description, children) {
  return el('div', { class: 'card mb-3' }, [
    el('div', { class: 'card-body' }, [
      el('h6', { class: 'card-title mb-1', text: title }),
      description ? el('p', { class: 'text-secondary small mb-3', text: description }) : null,
      ...(Array.isArray(children) ? children : [children]),
    ]),
  ]);
}
