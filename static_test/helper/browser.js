import './lib.js';

//浏览器垫片：页面代码是写给浏览器的，Node 里没有 document/localStorage/bootstrap/Chart。
//这里把宿主环境补齐到「渲染链路能真跑一遍」为止——够用即止，不追求实现一个浏览器：
//没有布局、没有 CSS、没有默认行为（提交、跳转、data-bs-* 全都不会自己发生），
//所以它证明得了数据与结构，证明不了观感。像素层面的事只能在真浏览器里看。
//页面模块在 import 阶段就会碰 document（chart.js 要读正文色），所以本模块必须第一个被求值。

// ===== 事件 =====

//自带 Event 的 target 是只读的，而页面里 onchange 回调读的正是 event.target，只能自己来
class ShimEvent {
  constructor(type, init) {
    this.type = type;
    this.bubbles = Boolean(init && init.bubbles);
    this.target = null;
    this.currentTarget = null;
    this.defaultPrevented = false;
  }

  preventDefault() {
    this.defaultPrevented = true;
  }

  stopPropagation() {
    this.bubbles = false;
  }
}

// ===== 节点 =====

class ShimNode {
  constructor() {
    this.childNodes = [];
    this.parentNode = null;
    this.listeners = new Map();
  }

  get firstChild() {
    return this.childNodes[0] || null;
  }

  appendChild(child) {
    if (child.parentNode) child.parentNode.removeChild(child);
    child.parentNode = this;
    this.childNodes.push(child);
    return child;
  }

  removeChild(child) {
    const index = this.childNodes.indexOf(child);
    if (index >= 0) this.childNodes.splice(index, 1);
    child.parentNode = null;
    return child;
  }

  remove() {
    if (this.parentNode) this.parentNode.removeChild(this);
  }

  addEventListener(type, handler) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(handler);
  }

  dispatchEvent(event) {
    if (!event.target) event.target = this;
    let node = this;
    while (node) {
      event.currentTarget = node;
      for (const handler of (node.listeners.get(event.type) || []).slice()) handler.call(node, event);
      node = event.bubbles ? node.parentNode : null;
    }
    return !event.defaultPrevented;
  }
}

class ShimText extends ShimNode {
  constructor(data) {
    super();
    this.data = String(data);
  }

  get textContent() {
    return this.data;
  }
}

//value/checked/disabled/type 在真 DOM 里既是属性也是特性：el() 用 setAttribute 写初值，
//页面代码之后用属性读写。这里照着这个双通道实现，属性一旦被显式写过就以属性为准
class ShimElement extends ShimNode {
  constructor(tag) {
    super();
    this.tagName = String(tag).toUpperCase();
    this.attributes = new Map();
    this.dataset = {};
    this.innerHTML = '';
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }

  removeAttribute(name) {
    this.attributes.delete(name);
  }

  get className() {
    return this.getAttribute('class') || '';
  }

  set className(value) {
    this.setAttribute('class', value);
  }

  get classNames() {
    return this.className.split(/\s+/).filter((item) => item !== '');
  }

  get value() {
    if (this.ownValue !== undefined) return this.ownValue;
    if (this.tagName === 'SELECT') {
      const options = queryAll(this, 'option');
      const chosen = options.find((option) => option.attributes.has('selected')) || options[0];
      return chosen ? chosen.value : '';
    }
    return this.getAttribute('value') || '';
  }

  set value(value) {
    this.ownValue = value === null || value === undefined ? '' : String(value);
  }

  get checked() {
    return this.ownChecked === undefined ? this.attributes.has('checked') : this.ownChecked;
  }

  set checked(value) {
    this.ownChecked = Boolean(value);
  }

  get disabled() {
    return this.ownDisabled === undefined ? this.attributes.has('disabled') : this.ownDisabled;
  }

  set disabled(value) {
    this.ownDisabled = Boolean(value);
  }

  get type() {
    return this.ownType === undefined ? this.getAttribute('type') || '' : this.ownType;
  }

  set type(value) {
    this.ownType = String(value);
  }

  get textContent() {
    return this.childNodes.map((child) => child.textContent).join('');
  }

  set textContent(value) {
    this.childNodes = [];
    this.appendChild(new ShimText(value));
  }

  //输入框的 select() 是「选中文本」，垫片里没有选区，留个空壳让复制那条链路走得下去
  select() {
  }

  //程序里发起的下载点的就是这个，缺了它整条导出链路会断在 link.click()
  click() {
    this.dispatchEvent(new ShimEvent('click', { bubbles: true }));
  }

  querySelector(selector) {
    return queryAll(this, selector)[0] || null;
  }

  querySelectorAll(selector) {
    return queryAll(this, selector);
  }
}

// ===== 选择器：只认标签、#id、.class 与后代组合，够页面与用例用 =====

function descendants(node, out) {
  for (const child of node.childNodes) {
    if (child instanceof ShimElement) {
      out.push(child);
      descendants(child, out);
    }
  }
  return out;
}

function matchToken(node, token) {
  for (const part of token.match(/[#.]?[^#.]+/g) || []) {
    if (part.startsWith('#')) {
      if (node.getAttribute('id') !== part.slice(1)) return false;
    } else if (part.startsWith('.')) {
      if (!node.classNames.includes(part.slice(1))) return false;
    } else if (part !== '*' && node.tagName !== part.toUpperCase()) {
      return false;
    }
  }
  return true;
}

export function queryAll(root, selector) {
  let scope = [root];
  for (const token of selector.trim().split(/\s+/)) {
    const matched = [];
    for (const node of scope) {
      for (const child of descendants(node, [])) {
        if (matchToken(child, token) && !matched.includes(child)) matched.push(child);
      }
    }
    scope = matched;
  }
  return scope;
}

// ===== document / window =====

const documentShim = {
  body: new ShimElement('body'),
  //主题落在 html 上，语言也落在 html 上，两条链路都要够得着这个节点
  documentElement: new ShimElement('html'),
  title: '',
  createElement: (tag) => new ShimElement(tag),
  createTextNode: (text) => new ShimText(text),
  querySelector: (selector) => queryAll(documentShim.body, selector)[0] || null,
  querySelectorAll: (selector) => queryAll(documentShim.body, selector),
  //非安全上下文下的复制兜底走的是它，垫片里当它永远成功
  execCommand: () => true,
};

const locationShim = {
  protocol: 'https:',
  hostname: 'localhost',
  href: 'https://localhost/static/index.html#/expense',
  hash: '#/expense',
  reloaded: 0,
  reload() {
    locationShim.reloaded += 1;
  },
};

const windowListeners = new Map();

const bootstrapShim = {
  Toast: class {
    constructor(node) {
      this.node = node;
    }

    show() {
    }

    hide() {
      this.node.dispatchEvent(new ShimEvent('hidden.bs.toast'));
    }
  },
  //confirmModal 靠 hidden.bs.modal 兑现它的 Promise，垫片里由 hide() 负责发这一枪
  Modal: class {
    constructor(node) {
      this.node = node;
    }

    show() {
      openedModals.push(this);
    }

    hide() {
      const index = openedModals.indexOf(this);
      if (index >= 0) openedModals.splice(index, 1);
      this.node.dispatchEvent(new ShimEvent('hidden.bs.modal'));
    }
  },
  Dropdown: class {
    constructor(node) {
      this.node = node;
    }
  },
};

const openedModals = [];

let systemDark = false;
const mediaListeners = [];

// ===== Chart 录像机 =====

//不装真 Chart.js：它要量 canvas 与 2d 上下文，Node 里没有。
//录下每张图拿到的 type/data/options/plugins，断言的就是「喂给图表的数据对不对」
const drawnCharts = [];

class ChartShim {
  constructor(canvas, config) {
    this.canvas = canvas;
    this.config = config;
    this.destroyed = false;
    drawnCharts.push(this);
  }

  destroy() {
    this.destroyed = true;
  }
}

ChartShim.defaults = { color: '#212529', font: { family: 'system-ui', size: 11 } };

// ===== 安装 =====

const createdBlobs = [];

//download() 挂的是 120 秒延时清理，不摘 ref 的话测试进程要空转两分钟才肯退出
const rawSetTimeout = globalThis.setTimeout;

globalThis.document = documentShim;
globalThis.window = globalThis;
//系统主题：默认不是深色，用例要验「跟随系统」时自己改 systemDark
globalThis.matchMedia = (query) => ({
  media: query,
  get matches() {
    return systemDark && query.includes('dark');
  },
  addEventListener: (type, handler) => mediaListeners.push(handler),
});
globalThis.location = locationShim;
globalThis.isSecureContext = true;
globalThis.Node = ShimNode;
globalThis.Event = ShimEvent;
globalThis.bootstrap = bootstrapShim;
globalThis.Chart = ChartShim;
globalThis.ChartDataLabels = { id: 'datalabels' };
globalThis.getComputedStyle = () => ({ getPropertyValue: () => '', fontFamily: '' });
globalThis.setTimeout = (handler, delay, ...args) => {
  const timer = rawSetTimeout(handler, delay, ...args);
  if (timer && typeof timer.unref === 'function') timer.unref();
  return timer;
};
globalThis.addEventListener = (type, handler) => {
  if (!windowListeners.has(type)) windowListeners.set(type, []);
  windowListeners.get(type).push(handler);
};
URL.createObjectURL = (blob) => {
  createdBlobs.push(blob);
  return `blob:jotcash/${createdBlobs.length}`;
};
URL.revokeObjectURL = () => {
};

// ===== 给用例的抓手 =====

export { documentShim as document, locationShim as location, ShimElement, ShimEvent };

export function resetDom() {
  documentShim.body.childNodes = [];
  drawnCharts.length = 0;
  createdBlobs.length = 0;
  openedModals.length = 0;
}

//页面都往 #page-host 里渲染，先把 index.html 里那几个容器摆好
export function mountHost() {
  resetDom();
  for (const id of ['notice-host', 'nav-wrap', 'nav-host', 'session-host', 'pref-host', 'page-host', 'toast-container']) {
    documentShim.body.appendChild(new ShimElement('div')).setAttribute('id', id);
  }
  return documentShim.querySelector('#page-host');
}

export function charts() {
  return drawnCharts.filter((chart) => !chart.destroyed);
}

export function blobs() {
  return createdBlobs;
}

export function modals() {
  return openedModals;
}

export function fireWindow(type) {
  for (const handler of (windowListeners.get(type) || []).slice()) handler(new ShimEvent(type));
}

//把系统主题偏好切过去并发一枪，等同于用户在操作系统里改了深浅色
export function setSystemDark(dark) {
  systemDark = dark;
  for (const handler of mediaListeners.slice()) handler({ matches: dark });
}

//默认语言取自 navigator，用例要验「浏览器是英文」只能把它换掉
export function setNavigatorLanguage(language) {
  globalThis.navigator = { language, languages: [language] };
}
