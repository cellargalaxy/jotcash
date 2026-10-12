import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';
import { equal, ok, same } from './helper/check.js';
import { STATIC_DIR, staticPath } from './helper/lib.js';
import { decodePng } from './helper/png.js';
import { createEngine, readyOpenCv, recognize } from '../static/js/ocr.js';

const FIXTURE_DIR = join(STATIC_DIR, '..', 'service', 'expense', 'unionpay_screenshot', 'testdata');

//跟页面一样加载 static/lib 里那一份，不另装 npm 包：测的必须是浏览器真正会跑的那份运行时与模型。
//opencv.js 在 Node 下要靠 require 读自己内嵌的 wasm，所以走 createRequire 而不是 new Function
const requireLib = createRequire(import.meta.url);

test('OCR：浏览器端加载的库与模型文件都在 static/lib 里', () => {
  const source = readFileSync(staticPath('js', 'ocr.js'), 'utf8');
  const paths = [...source.matchAll(/(?:loadScript|fetchBuffer)\('([^']+)'\)|new URL\('([^']+\.mjs)', LIB\)/g)].map((matched) => matched[1] || matched[2]);
  ok(`扫得到依赖路径: ${paths.join('|')}`, paths.length >= 6);
  for (const path of paths) ok(`${path} 存在`, existsSync(staticPath('lib', path)));
  for (const wasm of ['ort-wasm-simd-threaded.mjs', 'ort-wasm-simd-threaded.wasm']) ok(`${wasm} 存在`, existsSync(staticPath('lib', 'onnxruntime', wasm)));
});

//synthetic.json 是后端解析器单测的输入，这里盯住「同一张合成图在前端识别出来就是它」，两段拼起来才是整条链路。
//识别结果要跟 RapidOCR 1.2.3 的 python 版逐框一致：后端解析器是照着那份输出调出来的，差一个框的切分就可能丢一笔
test('OCR：合成长截图识别出的文字块与 synthetic.json 逐框一致', async () => {
  const ort = await import(pathToFileURL(staticPath('lib', 'onnxruntime', 'ort.wasm.min.mjs')).href);
  ort.env.wasm.wasmPaths = pathToFileURL(staticPath('lib', 'onnxruntime') + '/').href;
  ort.env.wasm.numThreads = 1;
  const cv = await readyOpenCv(requireLib(staticPath('lib', 'opencv', 'opencv.js')));
  const engine = await createEngine(ort, cv, requireLib(staticPath('lib', 'clipper', 'clipper.js')), {
    det: readFileSync(staticPath('lib', 'rapidocr', 'ch_PP-OCRv3_det_infer.onnx')),
    cls: readFileSync(staticPath('lib', 'rapidocr', 'ch_ppocr_mobile_v2.0_cls_infer.onnx')),
    rec: readFileSync(staticPath('lib', 'rapidocr', 'ch_PP-OCRv3_rec_infer.onnx')),
  }, JSON.parse(readFileSync(staticPath('lib', 'rapidocr', 'ppocr_keys.json'), 'utf8')));

  const png = decodePng(readFileSync(join(FIXTURE_DIR, 'synthetic.png')));
  equal('合成图是 RGB 三通道', png.channels, 3);
  const image = new cv.Mat(png.height, png.width, cv.CV_8UC3);
  image.data.set(png.pixels);
  let blocks;
  try {
    blocks = await recognize(engine, image);
  } finally {
    image.delete();
  }

  const expected = JSON.parse(readFileSync(join(FIXTURE_DIR, 'synthetic.json'), 'utf8'));
  same('逐框文字', blocks.map((block) => block.text), expected.map((block) => block.text));
  same('逐框左上角坐标', blocks.map((block) => [block.x, block.y]), expected.map((block) => [block.x, block.y]));
  ok('置信度都过了 0.5 的过滤线', blocks.every((block) => block.score >= 0.5));
});
