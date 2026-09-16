import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import './helper/lib.js';
import { equal, ok, rejects, same } from './helper/check.js';
import {
  AMOUNT_SCALE,
  API_BASE,
  CSV_FIELDS,
  DELETED_ALL,
  DELETED_NO,
  DELETED_ONLY,
  EXPENSE_SORTS,
  FILE_META_SORTS,
  OPERATION_LOG_SORTS,
  OPERATION_RESULTS,
  OPERATION_TYPES,
  PATH_CHANGE_TOKEN,
  PATH_EXPENSE_DELETE,
  PATH_EXPENSE_INSERT,
  PATH_EXPENSE_SELECT,
  PATH_EXPORT_DB,
  PATH_FILE_META_SELECT,
  PATH_IMPORT_DB,
  PATH_OPERATION_LOG_SELECT,
  PATH_PING,
  TOKEN_MIN_LEN,
  UPLOAD_FILE_KEY,
} from '../static/js/config.js';
import * as mock from '../static/js/mock.js';

//前端那些「必须与后端逐字一致」的常量，靠人盯是盯不住的：
//后端改一个词，前端不改，故障要等到联调甚至上线才显形。这里直接读 Go 源码来对
const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');

//辅助函数：读一份 Go 源码
function goSource(path) {
  return readFileSync(join(ROOT, path), 'utf8');
}

//辅助函数：抓出 Go 里 `名字 = "值"` 形态的字符串常量
function goConsts(path, prefix) {
  const found = new Map();
  const pattern = new RegExp(`(${prefix}\\w*)\\s*=\\s*"([^"]*)"`, 'g');
  for (const matched of goSource(path).matchAll(pattern)) found.set(matched[1], matched[2]);
  return found;
}

//辅助函数：抓出 `var xxxSortMap = map[string]string{...}` 里的排序键
function goSortKeys(path, name) {
  const block = new RegExp(`var ${name} = map\\[string\\]string\\{([\\s\\S]*?)\\n\\}`).exec(goSource(path));
  if (!block) throw new Error(`没找到排序白名单: ${name}`);
  return [...block[1].matchAll(/"([^"]+)"\s*:/g)].map((matched) => matched[1]);
}

test('CSV 列契约：列名与顺序与 base_csv.columns 逐字相等', () => {
  const values = goConsts('model/expense.go', 'Csv');
  const block = /var columns = \[\]string\{([\s\S]*?)\n\}/.exec(goSource('service/expense/base_csv/base_csv.go'));
  ok('找得到后端列定义', block);
  const columns = [...block[1].matchAll(/model\.(Csv\w+)/g)].map((matched) => {
    const value = values.get(matched[1]);
    if (value === undefined) throw new Error(`后端列常量没有取值: ${matched[1]}`);
    return value;
  });
  same('列名与顺序', CSV_FIELDS.map((field) => field.column), columns);
  equal('列数', CSV_FIELDS.length, 11);
});

test('审计枚举：操作类型与操作结果与 model 常量逐字相等', () => {
  same('操作类型', OPERATION_TYPES, [...goConsts('model/operation_log.go', 'OperationType').values()]);
  same('操作结果', OPERATION_RESULTS, [...goConsts('model/operation_log.go', 'Result').values()]);
});

test('删除筛选三态：取值与 model 的 iota 顺序一致', () => {
  const block = /const \(\s*\n([\s\S]*?)\n\)/.exec(goSource('model/expense.go'));
  const names = [...block[1].matchAll(/(Deleted\w+)/g)].map((matched) => matched[1]);
  same('顺序', names, ['DeletedNo', 'DeletedAll', 'DeletedOnly']);
  same('取值', [DELETED_NO, DELETED_ALL, DELETED_ONLY], [0, 1, 2]);
});

test('口令强度与金额精度：与后端同一个判据', () => {
  const minLen = /TokenMinLen\s*=\s*(\d+)/.exec(goSource('tool/token.go'));
  equal('口令最短长度', TOKEN_MIN_LEN, Number(minLen[1]));
  const scale = /amountScale\s*=\s*(\d+)/.exec(goSource('config/config.go'));
  equal('金额精度', AMOUNT_SCALE, Number(scale[1]));
});

test('接口路径：拼出来的地址与后端注册的路由相等', () => {
  const paths = goConsts('config/handler.go', 'Path');
  const pairs = [
    [PATH_EXPENSE_INSERT, paths.get('PathExpenseInsert')],
    [PATH_EXPENSE_SELECT, paths.get('PathExpenseSelect')],
    [PATH_EXPENSE_DELETE, paths.get('PathExpenseDelete')],
    [PATH_OPERATION_LOG_SELECT, paths.get('PathOperationLogSelect')],
    [PATH_FILE_META_SELECT, paths.get('PathFileMetaSelect')],
    [PATH_CHANGE_TOKEN, paths.get('PathChangeToken')],
    [PATH_EXPORT_DB, paths.get('PathExportDb')],
    [PATH_IMPORT_DB, paths.get('PathImportDb')],
  ];
  for (const [front, back] of pairs) {
    //页面在 /static 下，接口在 /api 下，前端用 ../api/ 相对拼，落到浏览器里就是后端那条绝对路径
    equal(`路径 ${front}`, API_BASE.replace('../', '/') + front, back);
  }
  equal('ping 走的是共享库的路径', `/api/${PATH_PING}`, '/api/ping');
  const fileKeys = goConsts('config/handler.go', '');
  equal('明细上传字段名', UPLOAD_FILE_KEY, fileKeys.get('ExpenseFileKey'));
  equal('整库导入字段名', UPLOAD_FILE_KEY, fileKeys.get('ImportFileKey'));
});

test('排序下拉：取值全部落在后端白名单内', () => {
  const cases = [
    ['明细', EXPENSE_SORTS, goSortKeys('rdb/expense.go', 'expenseSortMap')],
    ['审计', OPERATION_LOG_SORTS, goSortKeys('rdb/operation_log.go', 'operationLogSortMap')],
    ['文件', FILE_META_SORTS, goSortKeys('rdb/file_meta.go', 'fileMetaSortMap')],
  ];
  for (const [name, sorts, whitelist] of cases) {
    ok(`${name}白名单非空`, whitelist.length > 0);
    for (const sort of sorts) {
      ok(`${name}排序 ${sort.value} 在白名单内，白名单=${whitelist.join('|')}`, whitelist.includes(sort.value));
    }
  }
});

//mock 是后端在前端这一侧的替身，白名单窄了会让页面上能选的排序在 mock 里报错，
//宽了则会让前端偷偷传一个后端不认的取值还看着正常
test('mock 的排序白名单与后端三张表逐条等价', async () => {
  const cases = [
    ['明细', (sort) => mock.selectExpense({ sort }), goSortKeys('rdb/expense.go', 'expenseSortMap')],
    ['审计', (sort) => mock.selectOperationLog({ sort }), goSortKeys('rdb/operation_log.go', 'operationLogSortMap')],
    ['文件', (sort) => mock.selectFileMeta({ sort }), goSortKeys('rdb/file_meta.go', 'fileMetaSortMap')],
  ];
  for (const [name, call, whitelist] of cases) {
    for (const sort of whitelist) {
      const result = await call(sort);
      ok(`${name}接受白名单内的 ${sort}`, Array.isArray(result.object));
    }
    await rejects(`${name}拒绝白名单外的排序`, call('remark desc'), '不在白名单内');
  }
});
