//RapidOCR 1.2.3（rapidocr_onnxruntime）在浏览器里的逐行移植，模型、参数、每一步的取整方式都跟它对齐。
//对不齐的后果不是报错而是识别结果悄悄变了：后端解析器是照着它的输出调出来的，差一个框的切分就可能丢一笔明细。
//python 侧是 numpy float32 运算，这里用 Math.fround 逐步收窄；numpy 与 python 的 round 是银行家舍入，不是 Math.round

const LIB = new URL('../lib/', import.meta.url);

const DET_LIMIT_SIDE_LEN = 736;
const DET_MEAN = [0.485, 0.456, 0.406].map(Math.fround);
const DET_STD = [0.229, 0.224, 0.225].map(Math.fround);
const DET_SCALE = Math.fround(1 / 255);
const DET_THRESH = 0.3;
const DET_BOX_THRESH = 0.5;
const DET_MAX_CANDIDATES = 1000;
const DET_UNCLIP_RATIO = 1.6;
const DET_MIN_SIZE = 3;
const CLS_SHAPE = [3, 48, 192];
const CLS_BATCH = 6;
const CLS_THRESH = 0.9;
const CLS_LABELS = ['0', '180'];
const REC_HEIGHT = 48;
const REC_BATCH = 6;
const TEXT_SCORE = 0.5;
const MIN_HEIGHT = 30;
const WIDTH_HEIGHT_RATIO = 8;

function roundHalfEven(value) {
  const floor = Math.floor(value);
  const diff = value - floor;
  if (diff > 0.5) return floor + 1;
  if (diff < 0.5) return floor;
  return floor % 2 === 0 ? floor : floor + 1;
}

function clip(value, low, high) {
  return Math.min(Math.max(value, low), high);
}

function norm32(left, right) {
  const dx = Math.fround(left.x - right.x);
  const dy = Math.fround(left.y - right.y);
  return Math.fround(Math.sqrt(Math.fround(Math.fround(dx * dx) + Math.fround(dy * dy))));
}

//python 的 sorted 与这里的 Array.prototype.sort 都是稳定排序；argsort 只用在 4 个点上，numpy 对短数组走插入排序，同样稳定
function argsortBy(items, key) {
  return items.map((item, index) => index).sort((left, right) => key(items[left]) - key(items[right]));
}

function getMiniBoxes(cv, contour) {
  const rect = cv.minAreaRect(contour);
  const points = cv.RotatedRect.points(rect).sort((left, right) => left.x - right.x);
  const [first, second] = points[1].y > points[0].y ? [0, 1] : [1, 0];
  const [third, fourth] = points[3].y > points[2].y ? [2, 3] : [3, 2];
  return { box: [points[first], points[third], points[fourth], points[second]], side: Math.min(rect.size.width, rect.size.height) };
}

function boxScoreFast(cv, pred, width, height, box) {
  const xs = box.map((point) => point.x);
  const ys = box.map((point) => point.y);
  const xmin = clip(Math.floor(Math.min(...xs)), 0, width - 1);
  const xmax = clip(Math.ceil(Math.max(...xs)), 0, width - 1);
  const ymin = clip(Math.floor(Math.min(...ys)), 0, height - 1);
  const ymax = clip(Math.ceil(Math.max(...ys)), 0, height - 1);
  const maskWidth = xmax - xmin + 1;
  const maskHeight = ymax - ymin + 1;
  const mask = cv.Mat.zeros(maskHeight, maskWidth, cv.CV_8UC1);
  const polygon = cv.matFromArray(4, 1, cv.CV_32SC2, box.flatMap((point) => [
    Math.trunc(Math.fround(point.x - xmin)),
    Math.trunc(Math.fround(point.y - ymin)),
  ]));
  const polygons = new cv.MatVector();
  try {
    polygons.push_back(polygon);
    cv.fillPoly(mask, polygons, new cv.Scalar(1));
    let sum = 0;
    let count = 0;
    for (let row = 0; row < maskHeight; row += 1) {
      for (let col = 0; col < maskWidth; col += 1) {
        if (!mask.data[row * maskWidth + col]) continue;
        sum += pred[(ymin + row) * width + xmin + col];
        count += 1;
      }
    }
    return count ? sum / count : 0;
  } finally {
    mask.delete();
    polygon.delete();
    polygons.delete();
  }
}

//shapely 算面积与周长，pyclipper 把浮点坐标向零截断成整数再外扩
function unclip(cv, ClipperLib, box) {
  let area = 0;
  let length = 0;
  for (let index = 0; index < box.length; index += 1) {
    const current = box[index];
    const next = box[(index + 1) % box.length];
    area += current.x * next.y - next.x * current.y;
    length += Math.hypot(next.x - current.x, next.y - current.y);
  }
  const distance = (Math.abs(area) / 2) * DET_UNCLIP_RATIO / length;
  const offset = new ClipperLib.ClipperOffset(2, 0.25);
  offset.AddPath(box.map((point) => ({ X: Math.trunc(point.x), Y: Math.trunc(point.y) })), ClipperLib.JoinType.jtRound, ClipperLib.EndType.etClosedPolygon);
  const solution = new ClipperLib.Paths();
  offset.Execute(solution, distance);
  const points = solution.flat().flatMap((point) => [point.X, point.Y]);
  return points.length ? cv.matFromArray(points.length / 2, 1, cv.CV_32SC2, points) : null;
}

function orderPointsClockwise(box) {
  const xSorted = argsortBy(box, (point) => point.x).map((index) => box[index]);
  const [topLeft, bottomLeft] = argsortBy(xSorted.slice(0, 2), (point) => point.y).map((index) => xSorted[index]);
  const [topRight, bottomRight] = argsortBy(xSorted.slice(2), (point) => point.y).map((index) => xSorted[2 + index]);
  return [topLeft, topRight, bottomRight, bottomLeft];
}

async function detect(engine, image) {
  const { ort, cv, ClipperLib } = engine;
  const height = image.rows;
  const width = image.cols;
  let ratio = 1;
  if (Math.min(height, width) < DET_LIMIT_SIDE_LEN) ratio = height < width ? DET_LIMIT_SIDE_LEN / height : DET_LIMIT_SIDE_LEN / width;
  const resizeHeight = roundHalfEven(Math.trunc(height * ratio) / 32) * 32;
  const resizeWidth = roundHalfEven(Math.trunc(width * ratio) / 32) * 32;
  if (resizeHeight <= 0 || resizeWidth <= 0) return [];

  const resized = new cv.Mat();
  const plane = resizeHeight * resizeWidth;
  const input = new Float32Array(3 * plane);
  try {
    cv.resize(image, resized, new cv.Size(resizeWidth, resizeHeight));
    const pixels = resized.data;
    for (let index = 0; index < plane; index += 1) {
      for (let channel = 0; channel < 3; channel += 1) {
        const scaled = Math.fround(Math.fround(pixels[index * 3 + channel] * DET_SCALE) - DET_MEAN[channel]);
        input[channel * plane + index] = Math.fround(scaled / DET_STD[channel]);
      }
    }
  } finally {
    resized.delete();
  }

  const output = await engine.det.run({ [engine.det.inputNames[0]]: new ort.Tensor('float32', input, [1, 3, resizeHeight, resizeWidth]) });
  const pred = output[engine.det.outputNames[0]].data;

  const segmentation = new cv.Mat(resizeHeight, resizeWidth, cv.CV_8UC1);
  const bitmap = new cv.Mat();
  const kernel = cv.Mat.ones(2, 2, cv.CV_8UC1);
  const contours = new cv.MatVector();
  const hierarchy = new cv.Mat();
  const boxes = [];
  try {
    for (let index = 0; index < plane; index += 1) segmentation.data[index] = pred[index] > DET_THRESH ? 255 : 0;
    cv.dilate(segmentation, bitmap, kernel);
    cv.findContours(bitmap, contours, hierarchy, cv.RETR_LIST, cv.CHAIN_APPROX_SIMPLE);
    const candidates = Math.min(contours.size(), DET_MAX_CANDIDATES);
    for (let index = 0; index < candidates; index += 1) {
      const contour = contours.get(index);
      try {
        const mini = getMiniBoxes(cv, contour);
        if (mini.side < DET_MIN_SIZE) continue;
        if (DET_BOX_THRESH > boxScoreFast(cv, pred, resizeWidth, resizeHeight, mini.box)) continue;
        const expanded = unclip(cv, ClipperLib, mini.box);
        if (!expanded) continue;
        let unclipped;
        try {
          unclipped = getMiniBoxes(cv, expanded);
        } finally {
          expanded.delete();
        }
        if (unclipped.side < DET_MIN_SIZE + 2) continue;
        boxes.push(unclipped.box.map((point) => ({
          x: Math.trunc(clip(roundHalfEven(Math.fround(Math.fround(point.x / resizeWidth) * width)), 0, width)),
          y: Math.trunc(clip(roundHalfEven(Math.fround(Math.fround(point.y / resizeHeight) * height)), 0, height)),
        })));
      } finally {
        contour.delete();
      }
    }
  } finally {
    segmentation.delete();
    bitmap.delete();
    kernel.delete();
    contours.delete();
    hierarchy.delete();
  }

  const kept = [];
  for (const box of boxes) {
    const ordered = orderPointsClockwise(box).map((point) => ({
      x: Math.trunc(Math.min(Math.max(point.x, 0), width - 1)),
      y: Math.trunc(Math.min(Math.max(point.y, 0), height - 1)),
    }));
    if (Math.trunc(norm32(ordered[0], ordered[1])) <= 3 || Math.trunc(norm32(ordered[0], ordered[3])) <= 3) continue;
    kept.push(ordered);
  }
  return kept;
}

function sortBoxes(boxes) {
  const sorted = [...boxes].sort((left, right) => left[0].y - right[0].y || left[0].x - right[0].x);
  for (let index = 0; index < sorted.length - 1; index += 1) {
    if (Math.abs(sorted[index + 1][0].y - sorted[index][0].y) < 10 && sorted[index + 1][0].x < sorted[index][0].x) {
      [sorted[index], sorted[index + 1]] = [sorted[index + 1], sorted[index]];
    }
  }
  return sorted;
}

function cropBox(cv, image, box) {
  const cropWidth = Math.trunc(Math.max(norm32(box[0], box[1]), norm32(box[2], box[3])));
  const cropHeight = Math.trunc(Math.max(norm32(box[0], box[3]), norm32(box[1], box[2])));
  const source = cv.matFromArray(4, 1, cv.CV_32FC2, box.flatMap((point) => [point.x, point.y]));
  const target = cv.matFromArray(4, 1, cv.CV_32FC2, [0, 0, cropWidth, 0, cropWidth, cropHeight, 0, cropHeight]);
  const matrix = cv.getPerspectiveTransform(source, target);
  const crop = new cv.Mat();
  try {
    cv.warpPerspective(image, crop, matrix, new cv.Size(cropWidth, cropHeight), cv.INTER_CUBIC, cv.BORDER_REPLICATE, new cv.Scalar());
    if (crop.rows / crop.cols >= 1.5) {
      const rotated = new cv.Mat();
      cv.rotate(crop, rotated, cv.ROTATE_90_COUNTERCLOCKWISE);
      crop.delete();
      return rotated;
    }
    return crop;
  } catch (err) {
    crop.delete();
    throw err;
  } finally {
    source.delete();
    target.delete();
    matrix.delete();
  }
}

//按指定宽度缩放、归一化到 [-1,1]，右侧补零到 paddingWidth，写进批张量的第 slot 张
function fillNormalized(cv, crop, resizedWidth, height, paddingWidth, batch, slot) {
  const resized = new cv.Mat();
  try {
    cv.resize(crop, resized, new cv.Size(resizedWidth, height));
    const plane = height * paddingWidth;
    const base = slot * 3 * plane;
    for (let row = 0; row < height; row += 1) {
      for (let col = 0; col < resizedWidth; col += 1) {
        for (let channel = 0; channel < 3; channel += 1) {
          const value = Math.fround(Math.fround(Math.fround(resized.data[(row * resizedWidth + col) * 3 + channel] / 255) - 0.5) / 0.5);
          batch[base + channel * plane + row * paddingWidth + col] = value;
        }
      }
    }
  } finally {
    resized.delete();
  }
}

function resizedWidthOf(crop, height, maxWidth) {
  const ceiled = Math.ceil(height * (crop.cols / crop.rows));
  return ceiled > maxWidth ? maxWidth : ceiled;
}

async function classify(engine, crops) {
  const { ort, cv } = engine;
  const [, height, width] = CLS_SHAPE;
  const order = argsortBy(crops, (crop) => crop.cols / crop.rows);
  for (let begin = 0; begin < crops.length; begin += CLS_BATCH) {
    const indices = order.slice(begin, begin + CLS_BATCH);
    const batch = new Float32Array(indices.length * 3 * height * width);
    indices.forEach((index, slot) => fillNormalized(cv, crops[index], resizedWidthOf(crops[index], height, width), height, width, batch, slot));
    const output = await engine.cls.run({ [engine.cls.inputNames[0]]: new ort.Tensor('float32', batch, [indices.length, 3, height, width]) });
    const probs = output[engine.cls.outputNames[0]].data;
    indices.forEach((index, slot) => {
      const label = probs[slot * 2 + 1] > probs[slot * 2] ? 1 : 0;
      if (CLS_LABELS[label] === '180' && probs[slot * 2 + label] > CLS_THRESH) {
        const rotated = new cv.Mat();
        cv.rotate(crops[index], rotated, cv.ROTATE_180);
        crops[index].delete();
        crops[index] = rotated;
      }
    });
  }
}

async function recognizeCrops(engine, crops) {
  const { ort, cv, characters } = engine;
  const order = argsortBy(crops, (crop) => crop.cols / crop.rows);
  const results = new Array(crops.length);
  for (let begin = 0; begin < crops.length; begin += REC_BATCH) {
    const indices = order.slice(begin, begin + REC_BATCH);
    const maxRatio = Math.max(0, ...indices.map((index) => crops[index].cols / crops[index].rows));
    const width = Math.trunc(REC_HEIGHT * maxRatio);
    const batch = new Float32Array(indices.length * 3 * REC_HEIGHT * width);
    indices.forEach((index, slot) => fillNormalized(cv, crops[index], resizedWidthOf(crops[index], REC_HEIGHT, width), REC_HEIGHT, width, batch, slot));
    const output = await engine.rec.run({ [engine.rec.inputNames[0]]: new ort.Tensor('float32', batch, [indices.length, 3, REC_HEIGHT, width]) });
    const tensor = output[engine.rec.outputNames[0]];
    const [, steps, classes] = tensor.dims;
    indices.forEach((index, slot) => {
      let text = '';
      let sum = 0;
      let count = 0;
      let previous = -1;
      for (let step = 0; step < steps; step += 1) {
        const base = (slot * steps + step) * classes;
        let best = 0;
        for (let klass = 1; klass < classes; klass += 1) if (tensor.data[base + klass] > tensor.data[base + best]) best = klass;
        const current = previous;
        previous = best;
        if (best === 0 || (step > 0 && current === best)) continue;
        text += characters[best];
        sum += tensor.data[base + best];
        count += 1;
      }
      //python 侧是 np.mean(conf_list + [1e-50])，分母比字数多 1
      results[index] = { text, score: (sum + 1e-50) / (count + 1) };
    });
  }
  return results;
}

//image 是 RGB 三通道的 cv.Mat；返回值跟原后端 ocr.py 的输出逐字段一致
export async function recognize(engine, image) {
  const { cv } = engine;
  let boxes;
  let crops;
  if (image.rows <= MIN_HEIGHT || image.cols / image.rows > WIDTH_HEIGHT_RATIO) {
    boxes = [[{ x: 0, y: 0 }, { x: image.cols, y: 0 }, { x: image.cols, y: image.rows }, { x: 0, y: image.rows }]];
    crops = [image.clone()];
  } else {
    boxes = sortBoxes(await detect(engine, image));
    if (boxes.length === 0) return [];
    crops = boxes.map((box) => cropBox(cv, image, box));
  }
  try {
    await classify(engine, crops);
    const texts = await recognizeCrops(engine, crops);
    const blocks = [];
    boxes.forEach((box, index) => {
      if (texts[index].score < TEXT_SCORE) return;
      blocks.push({ text: texts[index].text.trim(), x: box[0].x, y: box[0].y, score: texts[index].score });
    });
    return blocks;
  } finally {
    for (const crop of crops) crop.delete();
  }
}

export async function createEngine(ort, cv, ClipperLib, models, characters) {
  const options = { executionProviders: ['wasm'], graphOptimizationLevel: 'all' };
  const [det, cls, rec] = await Promise.all([
    ort.InferenceSession.create(models.det, options),
    ort.InferenceSession.create(models.cls, options),
    ort.InferenceSession.create(models.rec, options),
  ]);
  return { ort, cv, ClipperLib, det, cls, rec, characters: ['blank', ...characters, ' '] };
}

//opencv 的初始化有两种形态：有的构建直接给 Promise，有的给对象再等 onRuntimeInitialized
export async function readyOpenCv(cv) {
  if (cv instanceof Promise) return cv;
  if (cv.Mat) return cv;
  await new Promise((resolve) => {
    cv.onRuntimeInitialized = resolve;
  });
  return cv;
}

function loadScript(path) {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = new URL(path, LIB).href;
    script.onload = resolve;
    script.onerror = () => reject(new Error(`加载 ${path} 失败`));
    document.head.appendChild(script);
  });
}

async function fetchBuffer(path) {
  const response = await fetch(new URL(path, LIB));
  if (!response.ok) throw new Error(`加载 ${path} 失败: ${response.status}`);
  return response.arrayBuffer();
}

let engineLoading = null;

function browserEngine() {
  if (!engineLoading) {
    engineLoading = (async () => {
      const ort = await import(new URL('onnxruntime/ort.wasm.min.mjs', LIB).href);
      ort.env.wasm.wasmPaths = new URL('onnxruntime/', LIB).href;
      //没有跨源隔离就用不了 SharedArrayBuffer，多线程会自己退回单线程并刷一条告警，不如直接说清楚
      ort.env.wasm.numThreads = globalThis.crossOriginIsolated ? Math.min(4, navigator.hardwareConcurrency || 1) : 1;
      await Promise.all([loadScript('opencv/opencv.js'), loadScript('clipper/clipper.js')]);
      const cv = await readyOpenCv(window.cv);
      const [det, cls, rec, keys] = await Promise.all([
        fetchBuffer('rapidocr/ch_PP-OCRv3_det_infer.onnx'),
        fetchBuffer('rapidocr/ch_ppocr_mobile_v2.0_cls_infer.onnx'),
        fetchBuffer('rapidocr/ch_PP-OCRv3_rec_infer.onnx'),
        fetchBuffer('rapidocr/ppocr_keys.json'),
      ]);
      return createEngine(ort, cv, window.ClipperLib, { det, cls, rec }, JSON.parse(new TextDecoder().decode(keys)));
    })();
    //加载失败不能把坏掉的 Promise 缓存下来，否则不刷新页面就再也识别不了
    engineLoading.catch(() => {
      engineLoading = null;
    });
  }
  return engineLoading;
}

//python 侧用 PIL 解码、不做色彩管理，这里关掉色彩空间转换才拿得到同一份像素
export async function recognizeFile(file) {
  const engine = await browserEngine();
  const { cv } = engine;
  const bitmap = await createImageBitmap(file, { colorSpaceConversion: 'none', premultiplyAlpha: 'none' });
  const canvas = document.createElement('canvas');
  canvas.width = bitmap.width;
  canvas.height = bitmap.height;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  context.drawImage(bitmap, 0, 0);
  bitmap.close();
  const rgba = cv.matFromImageData(context.getImageData(0, 0, canvas.width, canvas.height));
  const rgb = new cv.Mat();
  try {
    cv.cvtColor(rgba, rgb, cv.COLOR_RGBA2RGB);
    return await recognize(engine, rgb);
  } finally {
    rgba.delete();
    rgb.delete();
  }
}
