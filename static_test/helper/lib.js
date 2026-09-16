import { readFileSync } from 'node:fs';
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
