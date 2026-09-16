import test from 'node:test';
import './helper/browser.js';
import { find, findAll } from './helper/fixture.js';
import { equal, includes, ok, same } from './helper/check.js';
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
