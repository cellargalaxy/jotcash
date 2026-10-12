import { inflateSync } from 'node:zlib';

//Node 没有图片解码，浏览器里那条 createImageBitmap 的路走不了；opencv.js 的构建也没带 imdecode。
//只认合成截图用得到的那一种：8 位深、不隔行的 RGB 或 RGBA，其余形态直接报错，不猜
export function decodePng(bytes) {
  let offset = 8;
  let width = 0;
  let height = 0;
  let channels = 0;
  const chunks = [];
  while (offset < bytes.length) {
    const length = bytes.readUInt32BE(offset);
    const type = bytes.toString('ascii', offset + 4, offset + 8);
    const data = bytes.subarray(offset + 8, offset + 8 + length);
    if (type === 'IHDR') {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      const [depth, colorType, , , interlace] = data.subarray(8, 13);
      if (depth !== 8 || interlace !== 0 || (colorType !== 2 && colorType !== 6)) {
        throw new Error(`不支持的 PNG 形态: depth=${depth} colorType=${colorType} interlace=${interlace}`);
      }
      channels = colorType === 2 ? 3 : 4;
    } else if (type === 'IDAT') {
      chunks.push(data);
    }
    offset += 12 + length;
  }
  const raw = inflateSync(Buffer.concat(chunks));
  const stride = width * channels;
  const pixels = new Uint8Array(height * stride);
  for (let row = 0; row < height; row += 1) {
    const filter = raw[row * (stride + 1)];
    const line = raw.subarray(row * (stride + 1) + 1, (row + 1) * (stride + 1));
    for (let col = 0; col < stride; col += 1) {
      const left = col >= channels ? pixels[row * stride + col - channels] : 0;
      const up = row > 0 ? pixels[(row - 1) * stride + col] : 0;
      const upLeft = row > 0 && col >= channels ? pixels[(row - 1) * stride + col - channels] : 0;
      let predicted = 0;
      if (filter === 1) predicted = left;
      else if (filter === 2) predicted = up;
      else if (filter === 3) predicted = (left + up) >> 1;
      else if (filter === 4) {
        const estimate = left + up - upLeft;
        const toLeft = Math.abs(estimate - left);
        const toUp = Math.abs(estimate - up);
        const toUpLeft = Math.abs(estimate - upLeft);
        predicted = toLeft <= toUp && toLeft <= toUpLeft ? left : toUp <= toUpLeft ? up : upLeft;
      }
      pixels[row * stride + col] = (line[col] + predicted) & 0xff;
    }
  }
  return { width, height, channels, pixels };
}
