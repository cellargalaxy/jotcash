//识别器全部在浏览器里跑：Go 写的识别器编译成 wasm（go generate ./static 产出 static/wasm/），
//跟服务端测过的是同一份代码，解析口径不会因为换了语言而跑偏。服务端只收这里解析好的明细数组。
//wasm 按需加载：页面打开不下载，第一次上传时才拉，之后复用同一个实例
import { t } from './i18n.js';

const WASM_URL = new URL('../wasm/parser.wasm', import.meta.url);

//有的识别器吃的不是文件原样：截图要先在本地 OCR 成文字块，识别器解析的是文字块 JSON。
//模块按需加载：不选这类识别器的人，不该为它下载几十兆的模型与运行时
const EXTRACTORS = {
  unionpay_screenshot: async (file) => {
    const { recognizeFile } = await import('./ocr.js');
    return new TextEncoder().encode(JSON.stringify(await recognizeFile(file)));
  },
};

export function needExtract(parser) {
  return Boolean(EXTRACTORS[parser]);
}

//wasm_exec.js 是 Go 自带的胶水脚本，执行后挂出 globalThis.Go；识别器的入口在 Go 的 main 里挂出 jotcashParse。
//字节由调用方给：浏览器走 fetch，Node 测试直接读文件，加载的是同一个 wasm
export async function createParser(wasmBytes) {
  await import('../wasm/wasm_exec.js');
  const go = new globalThis.Go();
  const { instance } = await WebAssembly.instantiate(wasmBytes, go.importObject);
  //run 要等 Go 的 main 返回才 resolve，而 main 为了让导出的函数可用永远不返回，所以不能 await
  go.run(instance);
  const parse = globalThis.jotcashParse;
  const names = globalThis.jotcashParsers;
  if (typeof parse !== 'function' || typeof names !== 'function') throw new Error(t('识别器加载失败'));
  return {
    names: () => [...names()],
    //报错是 Go 侧拼好的中文原文，照原样抛出，由 toastErr 统一转译
    parse: (parser, bytes) => {
      const result = parse(parser, bytes);
      if (result.error) throw new Error(result.error);
      return JSON.parse(result.expense);
    },
  };
}

let loading = null;

//失败了要能重试：缓存的是正在进行的加载，失败就清掉，下次再拉
export function loadParser() {
  if (!loading) {
    loading = fetch(WASM_URL)
      .then((response) => {
        if (!response.ok) throw new Error(t('识别器加载失败'));
        return response.arrayBuffer();
      })
      .then((buffer) => createParser(buffer))
      .catch((err) => {
        loading = null;
        throw err;
      });
  }
  return loading;
}

//把用户上传的文件按所选识别器解析成明细数组（契约见 service/expense/parser.Expense）
export async function parseExpenseFile(file, parser) {
  const extract = EXTRACTORS[parser];
  const bytes = extract ? await extract(file) : new Uint8Array(await file.arrayBuffer());
  const engine = await loadParser();
  return engine.parse(parser, bytes);
}
