//接口挂在 <前缀>/api 下，页面挂在 <前缀>/static 下，用相对路径拼才能跟着反代前缀走
export const API_BASE = '../api/';

export const PATH_PING = 'ping';
export const PATH_EXPENSE_INSERT = 'expense/insert';
export const PATH_EXPENSE_SELECT = 'expense/select';
export const PATH_EXPENSE_DELETE = 'expense/delete';
export const PATH_OPERATION_LOG_SELECT = 'operation_log/select';
export const PATH_FILE_META_SELECT = 'file_meta/select';
export const PATH_CHANGE_TOKEN = 'token/change';
export const PATH_EXPORT_DB = 'db/export';
export const PATH_IMPORT_DB = 'db/import';

//上传字段名，与后端 config.ExpenseFileKey / config.ImportFileKey 一致
export const UPLOAD_FILE_KEY = 'file';

//本轮不联调，全部数据走内存 mock；联调时把它改成 false
export const USE_MOCK = true;

//金额精度，与后端配置 amount_scale 的默认值一致
export const AMOUNT_SCALE = 2;

//删除筛选三态，取值必须与后端 model.DeletedNo/DeletedAll/DeletedOnly 一致
export const DELETED_NO = 0;
export const DELETED_ALL = 1;
export const DELETED_ONLY = 2;

export const PAGE_SIZE_DEFAULT = 20;
export const PAGE_SIZE_MAX = 200;
export const PAGE_SIZES = [10, 20, 50, 100, 200];

//口令强度，与后端 tool.TokenMinLen 及 CheckToken 的判据一致
export const TOKEN_MIN_LEN = 12;

//审计操作类型，取值必须与后端 model.OperationType* 逐字一致
export const OPERATION_TYPES = [
  '系统初始化',
  '数据入库',
  '明细编辑',
  '明细删除',
  '记账币种切换',
  '更换口令',
  '数据库导入',
  '数据库导出',
];

//审计操作结果，取值必须与后端 model.Result* 逐字一致
export const OPERATION_RESULTS = ['成功', '失败', '部分成功'];

//明细字段：顺序即「全字段平铺」的展示顺序，editable 即允许内联编辑的 10 项
export const EXPENSE_FIELDS = [
  { key: 'bank_name', name: '银行名称', type: 'text', editable: true },
  { key: 'card_last_4', name: '卡号后四位', type: 'text', editable: true },
  { key: 'expense_date', name: '支出日期', type: 'date', editable: true },
  { key: 'expense_currency', name: '支出币种', type: 'currency', editable: true },
  { key: 'expense_amount', name: '支出金额', type: 'amount', editable: true },
  { key: 'counterparty', name: '交易对手方', type: 'text', editable: true },
  { key: 'remark', name: '交易备注', type: 'text', editable: true },
  { key: 'exchange_rate', name: '折算汇率', type: 'rate', editable: true },
  { key: 'accounting_currency', name: '记账币种', type: 'currency', editable: false },
  { key: 'accounting_amount', name: '记账金额', type: 'amount', editable: false },
  { key: 'expense_type', name: '支出类型', type: 'expense_type', editable: true },
  { key: 'amortization_months', name: '摊分月数', type: 'int', editable: true },
  { key: 'amortization_start_month', name: '摊分起始月', type: 'month', editable: false },
  { key: 'amortization_end_month', name: '摊分结束月', type: 'month', editable: false },
  { key: 'id', name: '明细ID', type: 'id', editable: false },
  { key: 'operation_id', name: '审计ID', type: 'id', editable: false },
  { key: 'file_id', name: '文件ID', type: 'id', editable: false },
  { key: 'version', name: '版本号', type: 'int', editable: false },
  { key: 'created_at', name: '创建时间', type: 'datetime', editable: false },
  { key: 'updated_at', name: '更新时间', type: 'datetime', editable: false },
  { key: 'deleted_at', name: '删除时间', type: 'datetime', editable: false },
];

//默认展示的 10 列
export const EXPENSE_COLUMN_DEFAULT = [
  'expense_date',
  'expense_currency',
  'expense_amount',
  'exchange_rate',
  'accounting_currency',
  'accounting_amount',
  'counterparty',
  'remark',
  'expense_type',
  'amortization_months',
];

//疑似重复的判定字段：三者相同即同组
export const DUPLICATE_KEYS = ['expense_date', 'expense_amount', 'expense_currency'];

//CSV 列契约：列名与顺序都必须与后端 base_csv.columns 完全相等，差一列解析器就不认领
export const CSV_FIELDS = [
  'bank_name',
  'card_last_4',
  'expense_date',
  'expense_currency',
  'expense_amount',
  'counterparty',
  'remark',
  'exchange_rate',
  'accounting_currency',
  'expense_type',
  'amortization_months',
];

//排序白名单，越界后端直接报错，下拉框只能给这些
export const EXPENSE_SORTS = [
  { value: 'expense_date desc', name: '支出日期 · 新→旧' },
  { value: 'expense_date asc', name: '支出日期 · 旧→新' },
  { value: 'expense_amount desc', name: '支出金额 · 大→小' },
  { value: 'expense_amount asc', name: '支出金额 · 小→大' },
  { value: 'created_at desc', name: '创建时间 · 新→旧' },
  { value: 'created_at asc', name: '创建时间 · 旧→新' },
  { value: 'id desc', name: '明细ID · 降序' },
  { value: 'id asc', name: '明细ID · 升序' },
];

export const OPERATION_LOG_SORTS = [
  { value: 'created_at desc', name: '操作时间 · 新→旧' },
  { value: 'created_at asc', name: '操作时间 · 旧→新' },
  { value: 'id desc', name: '审计ID · 降序' },
  { value: 'id asc', name: '审计ID · 升序' },
];

export const FILE_META_SORTS = [
  { value: 'created_at desc', name: '创建时间 · 新→旧' },
  { value: 'created_at asc', name: '创建时间 · 旧→新' },
  { value: 'file_name asc', name: '文件名 · 升序' },
  { value: 'file_name desc', name: '文件名 · 降序' },
  { value: 'id desc', name: '文件ID · 降序' },
  { value: 'id asc', name: '文件ID · 升序' },
];

//币种下拉的常用项。后端认的是 ISO 4217 全集（bojanz/currency 的 IsValid），
//这里只挑常用的做下拉，输入框允许直接敲三位代码，不在此表内也能提交
export const CURRENCIES = [
  { code: 'CNY', name: '人民币', digits: 2 },
  { code: 'USD', name: '美元', digits: 2 },
  { code: 'EUR', name: '欧元', digits: 2 },
  { code: 'JPY', name: '日元', digits: 0 },
  { code: 'HKD', name: '港元', digits: 2 },
  { code: 'GBP', name: '英镑', digits: 2 },
  { code: 'KRW', name: '韩元', digits: 0 },
  { code: 'SGD', name: '新加坡元', digits: 2 },
  { code: 'AUD', name: '澳大利亚元', digits: 2 },
  { code: 'CAD', name: '加拿大元', digits: 2 },
  { code: 'CHF', name: '瑞士法郎', digits: 2 },
  { code: 'TWD', name: '新台币', digits: 2 },
  { code: 'MOP', name: '澳门元', digits: 2 },
  { code: 'THB', name: '泰铢', digits: 2 },
  { code: 'MYR', name: '马来西亚林吉特', digits: 2 },
  { code: 'NZD', name: '新西兰元', digits: 2 },
  { code: 'RUB', name: '俄罗斯卢布', digits: 2 },
  { code: 'INR', name: '印度卢比', digits: 2 },
  { code: 'VND', name: '越南盾', digits: 0 },
  { code: 'PHP', name: '菲律宾比索', digits: 2 },
];

export const CURRENCY_DEFAULT = 'CNY';
