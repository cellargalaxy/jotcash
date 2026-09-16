import { blobs, document, modals, mountHost, queryAll, ShimEvent } from './browser.js';
import { seedMock } from '../../static/js/api.js';
import { MODE_MOCK } from '../../static/js/config.js';
import { unlock } from '../../static/js/store.js';

export { blobs, charts, document, location, mountHost, modals, fireWindow } from './browser.js';

//页面用例跑的是「已解锁 + 数据来自 mock」这一门会话：api 的分发判据挂在会话上，
//没有会话它就会去打真实接口，而种子也只有在 mock 会话里才播得进去
export function mockSession(currency) {
  unlock('后端口令', '前端口令', currency || 'CNY', MODE_MOCK);
  seedMock();
}

//页面的 render 是同步返回、异步填数据的（内部 reload 不被 await），
//用例得把微任务与宏任务都排空才看得到最终那一屏。mock 全是立即兑现的 Promise，转几圈足够
export async function flush(times) {
  for (let round = 0; round < (times || 4); round += 1) {
    await new Promise((resolve) => setImmediate(resolve));
  }
}

export function find(root, selector) {
  return queryAll(root, selector)[0] || null;
}

export function findAll(root, selector) {
  return queryAll(root, selector);
}

//按文案找控件：页面上的按钮都是中文文案，用文案定位比用第几个稳
export function findByText(root, selector, text) {
  return queryAll(root, selector).find((node) => node.textContent.includes(text)) || null;
}

//解锁表单上现在既有模式单选也有口令框，按 type 分比按第几个稳。
//垫片不做同名单选的互斥，选中哪一个由用例自己说了算，正与真浏览器里点一下等价
export function unlockForm(root) {
  const inputs = queryAll(root, 'input');
  return {
    modes: inputs.filter((node) => node.type === 'radio'),
    tokens: inputs.filter((node) => node.type === 'password'),
  };
}

export function texts(root, selector) {
  return queryAll(root, selector).map((node) => node.textContent);
}

//垫片不做默认行为：真浏览器里点 type=submit 会带出表单提交，这一步得自己补。
//禁用的按钮在真浏览器里点了没反应，这里也照此办理，否则「按钮该禁没禁」这类缺陷测不出来
export function click(node) {
  if (node.disabled) return;
  node.dispatchEvent(new ShimEvent('click', { bubbles: true }));
  if (node.type !== 'submit') return;
  let parent = node.parentNode;
  while (parent && parent.tagName !== 'FORM') parent = parent.parentNode;
  if (parent) parent.dispatchEvent(new ShimEvent('submit', { bubbles: true }));
}

export function dblclick(node) {
  if (node.disabled) return;
  node.dispatchEvent(new ShimEvent('dblclick', { bubbles: true }));
}

export function setValue(input, value) {
  input.value = value;
  input.dispatchEvent(new ShimEvent('input', { bubbles: true }));
  input.dispatchEvent(new ShimEvent('change', { bubbles: true }));
}

export function check(box, checked) {
  box.checked = checked;
  box.dispatchEvent(new ShimEvent('change', { bubbles: true }));
}

//toast 不会自己消失（那是 bootstrap 的活），读完就清，免得上一条串到下一个用例
export function takeToast() {
  const host = document.querySelector('#toast-container');
  const text = host ? host.textContent : '';
  if (host) host.childNodes = [];
  return text;
}

//confirmModal 靠 hidden.bs.modal 兑现 Promise：确认走「确认」按钮，取消只能直接关
export function answerModal(confirmed) {
  const modal = modals()[modals().length - 1];
  if (!modal) throw new Error('没有打开中的弹窗');
  if (!confirmed) {
    modal.hide();
    return modal.node;
  }
  const button = findByText(modal.node, 'button', '确认');
  if (!button) throw new Error('弹窗里没有确认按钮');
  click(button);
  return modal.node;
}

export function lastBlobText() {
  const blob = blobs()[blobs().length - 1];
  return blob ? blob.text() : Promise.resolve('');
}

//Blob.text() 按规范会把开头的 BOM 吃掉，要验 BOM 只能看原始字节
export async function lastBlobBytes() {
  const blob = blobs()[blobs().length - 1];
  return blob ? new Uint8Array(await blob.arrayBuffer()) : new Uint8Array();
}

//页面渲染完整跑一遍：摆好容器 → render → 排空异步
export async function renderPage(render, query) {
  const host = mountHost();
  render(host, query || {});
  await flush();
  return host;
}
