import { readFileSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const STATIC_DIR = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'static');

export function staticPath(...parts) {
  return join(STATIC_DIR, ...parts);
}

//装的就是 index.html 里那一份 decimal.js，不给金额算术另找替身：
//替身算得对，不代表页面上那一份算得对。UMD 见到 module 就往它身上挂，喂个空壳接回来即可
function loadUmd(path, name) {
  const source = readFileSync(staticPath(path), 'utf8');
  const shell = { exports: {} };
  new Function('module', 'exports', source)(shell, shell.exports);
  globalThis[name] = shell.exports;
}

loadUmd('lib/decimal/decimal.min.js', 'Decimal');

//纯逻辑模块也会碰宿主：语言判定要读 navigator，偏好读写要碰 storage。
//两者在 Node 里要么没有、要么取自跑测试那台机器的系统区域，不钉死就会让用例随机器变结果
Object.defineProperty(globalThis, 'navigator', {
  value: { language: 'zh-CN', languages: ['zh-CN', 'en'] },
  configurable: true,
  writable: true,
});

export function newStorage() {
  const box = new Map();
  return {
    getItem: (key) => (box.has(key) ? box.get(key) : null),
    setItem: (key, value) => box.set(key, String(value)),
    removeItem: (key) => box.delete(key),
    clear: () => box.clear(),
  };
}

globalThis.sessionStorage = newStorage();
globalThis.localStorage = newStorage();

// ===== 静态资源 =====

//页面按 import.meta.url 拼出静态资源地址（识别器的 wasm 就是这么取的），在 Node 里那是 file: 地址，
//自带的 fetch 不认。这里替静态文件服务器那一层：file: 地址读本地文件，其余照旧交给原来的 fetch。
//文件不存在就回 404，跟真服务器一样，好让「没先 go generate」这种情况在用例里暴露成加载失败
const nativeFetch = globalThis.fetch;
globalThis.fetch = async (url, init) => {
  const target = url instanceof URL ? url : new URL(String(url), 'http://shim.invalid/');
  if (target.protocol !== 'file:') return nativeFetch(url, init);
  try {
    return new Response(await readFile(target), { status: 200 });
  } catch (err) {
    return new Response('', { status: 404 });
  }
};
