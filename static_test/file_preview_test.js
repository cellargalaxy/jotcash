import test from 'node:test';
import './helper/browser.js';
import { blobs, click, find, findAll } from './helper/fixture.js';
import { equal, includes, not, ok, same } from './helper/check.js';
import { previewNode } from '../static/js/file_preview.js';

//辅助函数：按下载回来的形态造一份待预览的文件
function fileOf(text, name) {
  const blob = new Blob([text]);
  return { name: name || 'sample', text, blob, size: blob.size };
}

const CSV_TEXT = ['名称,金额,日期', '咖啡,32.00,2026-09-16', '打车,18.50,2026-09-15'].join('\r\n');

test('预览：CSV 铺成表格，表头与数据行都在', () => {
  const node = previewNode(fileOf(CSV_TEXT, 'a.csv'));
  same('表头取自第一行', findAll(node, 'thead th').map((cell) => cell.textContent), ['名称', '金额', '日期']);
  equal('两行数据', findAll(node, 'tbody tr').length, 2);
  includes('单元格原样铺出来', findAll(node, 'tbody tr')[0].textContent, '咖啡');
});

//几万行一次铺进 DOM 会把页面卡死，所以只铺前 200 行，并且必须告诉用户被截断了
test('预览：CSV 超过 200 行只铺前 200 行，并说明被截断', () => {
  const lines = ['名称,金额'];
  for (let index = 0; index < 250; index += 1) lines.push(`第${index}行,1.00`);
  const node = previewNode(fileOf(lines.join('\r\n'), 'big.csv'));
  equal('只铺 200 行', findAll(node, 'tbody tr').length, 200);
  includes('说清了总行数与已铺行数', node.textContent, '共 250 行，这里只铺了前 200 行');
});

test('预览：JSON 铺成缩进过的文本', () => {
  const node = previewNode(fileOf('{"a":1,"b":[2,3]}', 'a.json'));
  equal('用 pre 保留缩进', node.tagName, 'PRE');
  includes('缩进过了', node.textContent, '\n  "a": 1');
});

//既能当 JSON 解也能当两列 CSV 解的内容，得有个确定的先后，不能看运气
test('预览：认领有先后，JSON 排在 CSV 前面', () => {
  const node = previewNode(fileOf('[[1,2],[3,4]]', 'ambiguous'));
  equal('按 JSON 认领', node.tagName, 'PRE');
});

test('预览：PDF 交给浏览器自带的阅读器', () => {
  const node = previewNode(fileOf('%PDF-1.7\n1 0 obj\n', 'a.pdf'));
  equal('用 iframe 内嵌', node.tagName, 'IFRAME');
  ok('指向一个 blob 地址', node.getAttribute('src').startsWith('blob:'));
  equal('标题给辅助技术用', node.getAttribute('title'), 'a.pdf');
});

//认领只看内容不看扩展名：后端下载响应统一是 octet-stream，名字里那一截作不得准
test('预览：扩展名说了不算，内容说了算', () => {
  const asCsv = previewNode(fileOf(CSV_TEXT, 'a.pdf'));
  ok('叫 pdf 但内容是 CSV，还是按表格铺', find(asCsv, 'table'));
  const asPdf = previewNode(fileOf('%PDF-1.4\n', 'a.csv'));
  equal('叫 csv 但内容是 PDF，还是内嵌阅读器', asPdf.tagName, 'IFRAME');
});

test('预览：认不出的格式给兜底说明，不铺乱码', () => {
  const node = previewNode(fileOf('这是一段普通的文字，既不是表格也不是 JSON。', 'a.txt'));
  includes('说清了不认这种格式', node.textContent, '这种格式没有内置预览');
  includes('指出下一步', node.textContent, '请下载之后用本机程序打开');
});

//单列文本用 CSV 解析器也能解出来，但那不是表格；判据松了就会把散文铺成一列表格
test('预览：单列文本不算表格，不认领', () => {
  const node = previewNode(fileOf(['第一行', '第二行', '第三行'].join('\n'), 'a.txt'));
  includes('落到兜底', node.textContent, '这种格式没有内置预览');
});

test('预览：二进制内容不当文本铺', () => {
  const node = previewNode(fileOf('PKbinary,payload\nhere,too\n', 'a.zip'));
  includes('落到兜底', node.textContent, '这种格式没有内置预览');
});

test('预览：空文件与超大文本都给原因', () => {
  includes('空文件', previewNode(fileOf('', 'empty.csv')).textContent, '这个文件没有内容');

  const wide = `${'名称,金额'}\r\n${'咖啡,1.00\r\n'.repeat(60000)}`;
  const node = previewNode(fileOf(wide, 'huge.csv'));
  includes('超大文本只给下载', node.textContent, '不在页面里铺开');
  includes('说清了那条线在哪', node.textContent, '512.00 KB');
});

// ===== 图片 =====

//辅助函数：按页面的形态造一份二进制文件，head 是原始字节的开头，text 是解码之后的样子
function binaryOf(bytes, name) {
  const data = Uint8Array.from(bytes);
  const blob = new Blob([data]);
  return { name: name || 'sample', text: new TextDecoder().decode(data), head: data.slice(0, 32), blob, size: blob.size };
}

const PNG_BYTES = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 0x49, 0x48, 0x44, 0x52];
const JPEG_BYTES = [0xff, 0xd8, 0xff, 0xe0, 0, 16, 0x4a, 0x46, 0x49, 0x46, 0];

function toolOf(node, label) {
  return findAll(node, 'button').find((button) => button.getAttribute('aria-label') === label);
}

function stageOf(node) {
  return find(node, '.preview-stage');
}

test('预览：PNG、JPEG 按魔数认领，扩展名说了不算', () => {
  for (const [bytes, name, type] of [[PNG_BYTES, 'a.csv', 'image/png'], [JPEG_BYTES, 'screenshot.pdf', 'image/jpeg']]) {
    const node = previewNode(binaryOf(bytes, name));
    const image = find(node, 'img');
    ok(`${name} 铺成图片`, image);
    ok('指向一个 blob 地址', image.getAttribute('src').startsWith('blob:'));
    equal('替代文本是文件名', image.getAttribute('alt'), name);
    equal('重包成图片类型，浏览器才肯当图片解', blobs()[blobs().length - 1].type, type);
  }
});

test('预览：GIF、WebP、BMP 也认', () => {
  const gif = [...'GIF89a'].map((char) => char.charCodeAt(0)).concat([1, 0, 1, 0]);
  const webp = [...'RIFF'].map((char) => char.charCodeAt(0)).concat([0, 0, 0, 0], [...'WEBPVP8 '].map((char) => char.charCodeAt(0)));
  const bmp = [0x42, 0x4d, 0x3a, 0, 0, 0, 0, 0, 0, 0, 0x36, 0, 0, 0, 40, 0, 0, 0];
  for (const bytes of [gif, webp, bmp]) ok('铺成图片', find(previewNode(binaryOf(bytes)), 'img'));
});

//BMP 只有「BM」两个字母当魔数，以 BMW 开头的表格不能被它截走
test('预览：BM 开头的文本不当 BMP', () => {
  const text = ['BMW,金额', '保养,1200.00'].join('\r\n');
  const node = previewNode(binaryOf([...new TextEncoder().encode(text)], 'car.csv'));
  ok('还是按表格铺', find(node, 'table'));
});

test('预览：认不出的二进制不当图片', () => {
  const node = previewNode(binaryOf([0x50, 0x4b, 0x03, 0x04, 0x14, 0, 0, 0, 0x08, 0, 0xff, 0xd8], 'a.png'));
  not('没有图片', find(node, 'img'));
  includes('落到兜底', node.textContent, '这种格式没有内置预览');
});

//没带原始字节的调用方不猜：文本里 0x89 早被换成替换符了，看文本认不准
test('预览：没有原始字节开头就不认图片', () => {
  const node = previewNode(fileOf('\x89PNG\r\n\x1a\n', 'a.png'));
  not('没有图片', find(node, 'img'));
});

test('预览：图片左右旋转按 90 度一档，转满一圈回到 0', () => {
  const node = previewNode(binaryOf(PNG_BYTES, 'a.png'));
  equal('起始不转', stageOf(node).dataset.rotate, '0');
  click(toolOf(node, '向右旋转'));
  equal('右转 90', stageOf(node).dataset.rotate, '90');
  includes('图上带了旋转', find(node, 'img').getAttribute('style'), 'rotate(90deg)');
  click(toolOf(node, '向左旋转'));
  click(toolOf(node, '向左旋转'));
  equal('左转越过 0 绕到 270', stageOf(node).dataset.rotate, '270');
  click(toolOf(node, '向右旋转'));
  equal('再右转满一圈回到 0，不是 360', stageOf(node).dataset.rotate, '0');
});

//侧躺时 CSS 旋转不改占位，stage 得按转后的宽高比占位，否则图会溢出被裁
test('预览：图片加载后侧躺时按转后的宽高比占位', () => {
  const node = previewNode(binaryOf(PNG_BYTES, 'long.png'));
  const image = find(node, 'img');
  image.naturalWidth = 1080;
  image.naturalHeight = 9630;
  image.dispatchEvent(new Event('load'));
  includes('竖着的按原比例占位', stageOf(node).getAttribute('style'), `aspect-ratio: ${1080 / 9630}`);
  click(toolOf(node, '向右旋转'));
  includes('侧躺后宽高比倒过来', stageOf(node).getAttribute('style'), `aspect-ratio: ${9630 / 1080}`);
  includes('图宽按原比例反推', image.getAttribute('style'), `width: ${(1080 / 9630) * 100}%`);
});

test('预览：图片默认按宽铺满，放大、缩小、复位', () => {
  const node = previewNode(binaryOf(JPEG_BYTES, 'a.jpg'));
  equal('默认适应宽度', stageOf(node).dataset.zoom, '1');
  includes('宽度铺满', stageOf(node).getAttribute('style'), 'width: 100%');
  includes('标出比例', node.textContent, '100%');
  click(toolOf(node, '放大'));
  equal('放大一档', stageOf(node).dataset.zoom, '1.25');
  includes('宽度跟着变，滚动区域才跟着变', stageOf(node).getAttribute('style'), 'width: 125%');
  click(toolOf(node, '缩小'));
  click(toolOf(node, '缩小'));
  equal('缩小两档', stageOf(node).dataset.zoom, '0.8');
  includes('比例跟着变', node.textContent, '80%');
  for (let index = 0; index < 20; index += 1) click(toolOf(node, '缩小'));
  equal('有下限', stageOf(node).dataset.zoom, '0.25');
  click(toolOf(node, '适应宽度'));
  equal('复位到适应宽度', stageOf(node).dataset.zoom, '1');
});

test('预览：图片的按钮都有给辅助技术的名字', () => {
  const node = previewNode(binaryOf(PNG_BYTES, 'a.png'));
  for (const button of findAll(node, 'button')) {
    ok('有 aria-label', button.getAttribute('aria-label'));
    equal('title 与 aria-label 一致', button.getAttribute('title'), button.getAttribute('aria-label'));
  }
  equal('五个控件', findAll(node, 'button').length, 5);
});
