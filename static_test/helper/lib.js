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
