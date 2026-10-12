//耗时、吃内存的识别（OCR 这类）放在浏览器本地做完，后端只解析这里提取出的内容。
//以后再有这样的识别器，只在这里加一项、后端注册一个吃提取结果的解析器，上传接口不用改。
//模块按需加载：不选这类识别器的人，不该为它下载几十兆的模型与运行时
export const EXTRACTORS = {
  unionpay_screenshot: async (file) => {
    const { recognizeFile } = await import('./ocr.js');
    return JSON.stringify(await recognizeFile(file));
  },
};
