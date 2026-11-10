import { loadParser } from '../../static/js/expense_parse.js';

//替用户那一步：把一份本系统 CSV 交给页面同一个 wasm 识别器，解析成要上传的明细数组。
//跑的是 static/wasm 里真编出来的那一份，没先 go generate ./static 就会在这里报识别器加载失败
export async function csvExpenses(csv) {
  const engine = await loadParser();
  return engine.parse('base_csv', new TextEncoder().encode(csv));
}

//造数用：解析 + 入库一步到位，原文件就是这份 CSV
export async function insertCsv(api, filename, csv) {
  return api.insertExpense(new Blob([csv], { type: 'text/csv' }), filename, await csvExpenses(csv));
}

//mock 层直接造数，口径同上
export async function mockInsertCsv(mock, filename, csv, accountingCurrency) {
  return mock.insertExpense(filename, new Blob([csv], { type: 'text/csv' }), await csvExpenses(csv), accountingCurrency || 'CNY');
}
