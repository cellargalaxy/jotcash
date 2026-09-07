package currency

import (
	"fmt"
	"sort"
)

// RateSource 是汇率获取实现的标识（分层 §六 L1.0 承载第六项）。
//
// 按准则 3，本包**只存标识**：fxrate（L3）持有「标识 → 实现」的注册表，
// 实现体在 fxrate/<源>（L4）。定义成具名字符串类型而非裸 string，是为了让
// 注册表的键类型自带语义，避免与币种代码等其他字符串混用。
type RateSource string

// RateSourceDefault 是当前唯一的汇率源标识。
//
// 分层 §六 L4 写明「fxrate/<源> 的具体包名随汇率源选定后确定」，§十二 亦将
// 汇率源选型登记为不影响分包的未结项；configs/jotcash.example.yaml 的
// fx_rate_endpoint / fx_rate_secret 两项当前留空并注明「汇率源尚未选定」。
//
// 故本轮先定「机制」不定「取值域」：注册表按 Code.RateSource() 路由的机制
// 已经成立，选定具体源后只需改本文件表中该列的取值，不改任何签名，下游代码
// 不受影响。这不构成阻塞项。
const RateSourceDefault RateSource = "default"

// nameKeyPrefix 是币种名称文案键的前缀，完整键形如 "currency.name.CNY"。
// 供 i18n（L3）落词、L6 渲染，见 Code.NameKey。
const nameKeyPrefix = "currency.name."

// maxBaseScale 是本位币候选的小数位数上界（T38：小数位数 ≤ 2）。
//
// 取 2 的依据是 decimal.BaseAmountStoreScale——本位币金额以 2 位定点存储，
// 按本位币自身位数算出的金额必须能被该列无损容纳。这里不 import decimal
// （本包保持零仓库内依赖），二者的一致性由 consumer_check_test.go 中的
// TestConsumerConvertBaseScaleFitsStorage 用可执行断言锁死。
const maxBaseScale int32 = 2

// entry 是枚举表中的一个条目，字段与承载要求的六项属性一一对应。
//
// 全部字段私有且表初始化后只读，故包外无法构造、无法修改——这是终版 I-1
// 「新增币种须改代码，枚举项只能停用不能删除」的结构表达。
type entry struct {
	code       string     // ① 代码，ISO 4217 三字母
	name       string     // ② 名称（中文，仅日志与排错，见 Code.Name）
	symbol     string     // ③ 符号
	scale      int32      // ④ 小数位数（8.1 舍入 / 8.2 均分的基准）
	enabled    bool       // ⑤ 是否启用
	rateSource RateSource // ⑥ 汇率获取实现的标识
}

// currencies 是币种枚举表的**唯一数据来源**。
//
// # 清单口径
//
// T38 写明「本仓库中长期只实现常见场景币种」。清单按个人记账的真实场景选取：
// 人民币 + 港澳台 + 主要储备货币 + 亚太与欧美常用货币，共 27 项。
//
// # 小数位数从哪来
//
// 每一项的 scale 均与 CLDR 48.2.0 交叉校验（table_test.go 的
// TestScaleMatchesCLDR 用 test-only 依赖 bojanz/currency 逐项断言），
// 不是凭记忆写死——这项数据算错不会报错，只会让金额差一到两个分位。
//
// 一处已实测的数据分歧需要登记：golang.org/x/text/currency 对 IDR 给 0 位，
// bojanz/currency 给 2 位。核查 CLDR 原始数据 currencyData.json 得
// IDR: {_digits: 2, _cashDigits: 0}——CLDR 本身区分「账面位数 2」与
// 「现金位数 0」，x/text 取的是其冻结版本（CLDR v32）的 cash 口径，
// ISO 4217 官方 minor unit 亦为 2。**本表取账面位数 2**：终版 §一 定位
// 「以银行账单导入为主要数据来源」，记的是账单流水而非现金找零。
// x/text 因数据整体停在 v32 不予采用。
//
// # 为什么保留 KWD
//
// KWD 是 3 位小数币种，按 T38 不能作本位币。保留它是因为它是「支出币种不受
// 本位币候选限制」（分层 §六 L1.0）这条规则**唯一可被测试的载体**——删掉它，
// 这条规则就无从验证。TestSpendCurrencyNotLimitedByBase 依赖它。
//
// # 关于 enabled
//
// 当前 27 项全部启用。该字段不是冗余：终版 I-1 规定枚举项只能停用不能删除，
// 停用机制必须先存在，将来才停得掉。TestDisabledCurrencyStillReadable 用
// 一个临时构造的停用条目验证「停用后历史明细仍可读出」这条行为。
var currencies = []entry{
	// 本位币默认值与主场景币种
	{"CNY", "人民币", "¥", 2, true, RateSourceDefault},
	{"HKD", "港元", "HK$", 2, true, RateSourceDefault},
	{"MOP", "澳门元", "MOP", 2, true, RateSourceDefault},
	{"TWD", "新台币", "NT$", 2, true, RateSourceDefault},

	// 主要储备货币
	{"USD", "美元", "US$", 2, true, RateSourceDefault},
	{"EUR", "欧元", "€", 2, true, RateSourceDefault},
	{"GBP", "英镑", "£", 2, true, RateSourceDefault},
	{"JPY", "日元", "JP¥", 0, true, RateSourceDefault},
	{"CHF", "瑞士法郎", "CHF", 2, true, RateSourceDefault},

	// 亚太常用
	{"KRW", "韩元", "₩", 0, true, RateSourceDefault},
	{"SGD", "新加坡元", "SGD", 2, true, RateSourceDefault},
	{"AUD", "澳大利亚元", "AU$", 2, true, RateSourceDefault},
	{"NZD", "新西兰元", "NZ$", 2, true, RateSourceDefault},
	{"THB", "泰铢", "THB", 2, true, RateSourceDefault},
	{"MYR", "马来西亚林吉特", "MYR", 2, true, RateSourceDefault},
	{"IDR", "印尼盾", "IDR", 2, true, RateSourceDefault},
	{"PHP", "菲律宾比索", "₱", 2, true, RateSourceDefault},
	{"VND", "越南盾", "₫", 0, true, RateSourceDefault},
	{"INR", "印度卢比", "₹", 2, true, RateSourceDefault},

	// 美洲与其他
	{"CAD", "加拿大元", "CA$", 2, true, RateSourceDefault},
	{"BRL", "巴西雷亚尔", "R$", 2, true, RateSourceDefault},
	{"MXN", "墨西哥比索", "MX$", 2, true, RateSourceDefault},

	// 欧洲非欧元区
	{"SEK", "瑞典克朗", "SEK", 2, true, RateSourceDefault},
	{"NOK", "挪威克朗", "NOK", 2, true, RateSourceDefault},
	{"DKK", "丹麦克朗", "DKK", 2, true, RateSourceDefault},
	{"RUB", "俄罗斯卢布", "RUB", 2, true, RateSourceDefault},

	// 3 位小数币种：可作支出币种，不可作本位币（T38）。见上文「为什么保留 KWD」。
	{"KWD", "科威特第纳尔", "KWD", 3, true, RateSourceDefault},
}

// defaultBaseCode 是默认本位币代码（终版 I-2）。
const defaultBaseCode = "CNY"

// 包级只读索引与预排序集合。均在 init 中一次构建，此后只读。
//
// 因为无任何写入口（见包注释「不做什么」第四条），并发读天然安全，不需要
// 加锁——这与否决 bojanz/currency 的第二条理由（Register 与读并发即 race）
// 互为表里。
var (
	// table 是「代码 → 条目」索引，Parse 与 IsKnown 的 O(1) 查询依据。
	table map[string]*entry

	// allCodes 是全部已知币种（含停用），按代码字典序。
	allCodes []Code
	// enabledCodes 是全部启用币种，按代码字典序。
	enabledCodes []Code
	// baseCandidates 是本位币候选（T38），按代码字典序。
	baseCandidates []Code
	// nameKeys 是全部币种的文案键，按代码字典序，供 i18n 穷尽性校验。
	nameKeys []string

	// defaultBase 是默认本位币（终版 I-2）。
	defaultBase Code
)

// built 是由枚举表派生出的全部只读索引与集合。
//
// 把它抽成一个结构体、由纯函数 buildTable 产出，是为了让 init 的各条自检
// 分支**可被测试**：直接在 init 里做校验的话，那些 panic 分支只有在表被改坏
// 时才会触发，测试无法构造——一个从未被验证过的保险，等于没有保险。
type built struct {
	table          map[string]*entry
	allCodes       []Code
	enabledCodes   []Code
	baseCandidates []Code
	nameKeys       []string
	defaultBase    Code
}

// buildTable 由条目切片构建索引与预排序集合，并做三项自检：
// 条目字段完整、代码不重复、默认本位币存在且满足候选口径。
//
// 返回 error 而非直接 panic，使各分支可被 TestBuildTableRejectsBadTable 覆盖。
func buildTable(entries []entry, defaultCode string) (built, error) {
	var b built
	b.table = make(map[string]*entry, len(entries))
	for i := range entries {
		e := &entries[i]
		if err := checkEntry(e); err != nil {
			return built{}, fmt.Errorf("枚举表条目非法: %w", err)
		}
		if _, dup := b.table[e.code]; dup {
			return built{}, fmt.Errorf("枚举表存在重复的币种代码 %q", e.code)
		}
		b.table[e.code] = e
	}

	// 按代码字典序构建三个集合。顺序必须确定——map 遍历是随机的，
	// 若直接由 map 生成，K-1 的本位币下拉每次刷新都会重排（承载 6 要求
	// 「对外提供候选集（K-1 下拉）」，一个每次都变序的下拉是缺陷）。
	codes := make([]string, 0, len(b.table))
	for code := range b.table {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	b.allCodes = make([]Code, 0, len(codes))
	b.nameKeys = make([]string, 0, len(codes))
	for _, code := range codes {
		c := Code{entry: b.table[code]}
		b.allCodes = append(b.allCodes, c)
		b.nameKeys = append(b.nameKeys, c.NameKey())
		if c.Enabled() {
			b.enabledCodes = append(b.enabledCodes, c)
		}
		if c.CanBeBase() {
			b.baseCandidates = append(b.baseCandidates, c)
		}
	}

	e, ok := b.table[defaultCode]
	if !ok {
		return built{}, fmt.Errorf("默认本位币 %q 不在枚举表内", defaultCode)
	}
	b.defaultBase = Code{entry: e}
	if !b.defaultBase.CanBeBase() {
		return built{}, fmt.Errorf("默认本位币 %q 不满足本位币候选口径", defaultCode)
	}
	return b, nil
}

// init 构建索引与预排序集合。
//
// 自检失败一律 panic：这些都是「表本身写错了」的情形（重复代码、空字段、
// 负位数、默认本位币不在表内），属编码错误而非运行期输入错误。让它在进程
// 启动时当场炸掉，远好过带着一张坏表跑起来、直到某笔折算才算错钱。
func init() {
	b, err := buildTable(currencies, defaultBaseCode)
	if err != nil {
		panic("currency: " + err.Error())
	}
	table = b.table
	allCodes = b.allCodes
	enabledCodes = b.enabledCodes
	baseCandidates = b.baseCandidates
	nameKeys = b.nameKeys
	defaultBase = b.defaultBase
}

// checkEntry 校验单个条目的字段完整性。
func checkEntry(e *entry) error {
	if e.code == "" {
		return errEntryField("代码为空")
	}
	if len(e.code) != codeLen {
		return errEntryField(fmt.Sprintf("代码 %q 不是 3 个字符", e.code))
	}
	for i := 0; i < len(e.code); i++ {
		if e.code[i] < 'A' || e.code[i] > 'Z' {
			return errEntryField(fmt.Sprintf("代码 %q 含非大写字母字符", e.code))
		}
	}
	if e.name == "" {
		return errEntryField(fmt.Sprintf("%s 的名称为空", e.code))
	}
	if e.symbol == "" {
		return errEntryField(fmt.Sprintf("%s 的符号为空", e.code))
	}
	// 位数下界为 0；不设硬上界，只拒绝负数与明显失真的值。
	// 现存币种最大 3 位（KWD 等），CLDR 中的 4 位项（CLF/UYW）是记账单位
	// 而非流通货币，不纳入清单。
	if e.scale < 0 || e.scale > maxEntryScale {
		return errEntryField(fmt.Sprintf("%s 的小数位数 %d 越界", e.code, e.scale))
	}
	if e.rateSource == "" {
		return errEntryField(fmt.Sprintf("%s 的汇率源标识为空", e.code))
	}
	return nil
}

// codeLen 是 ISO 4217 字母代码的长度。
const codeLen = 3

// maxEntryScale 是枚举表允许的小数位数上界，用于自检兜底。
// 取 4 以容纳 CLDR 中的极端项，实际清单最大为 3（KWD）。
const maxEntryScale int32 = 4

// errEntryField 构造表自检错误。
func errEntryField(msg string) error {
	return fmt.Errorf("枚举表字段错误: %s", msg)
}

// All 返回全部已知币种（**含已停用**），按代码字典序。
//
// 含停用项是刻意的：K-3 整库导出与 I-7 逐笔重算都要覆盖历史明细引用到的
// 旧币种。需要「可选项」时用 Enabled，不要在这里过滤。
//
// 返回新切片，调用方的改动不会污染包状态。
func All() []Code {
	out := make([]Code, len(allCodes))
	copy(out, allCodes)
	return out
}

// Enabled 返回全部已启用币种，按代码字典序，供 D-2/F-5 选支出币种与 F-4
// 筛选下拉使用。
//
// 返回新切片，调用方的改动不会污染包状态。
func Enabled() []Code {
	out := make([]Code, len(enabledCodes))
	copy(out, enabledCodes)
	return out
}

// BaseCandidates 返回本位币候选集（T38：已启用 且 小数位数 ≤ 2），
// 按代码字典序，供 K-1 的本位币下拉使用。
//
// 单值校验请用 ParseBase，不必取回本集合再自行遍历。
//
// 返回新切片，调用方的改动不会污染包状态。
func BaseCandidates() []Code {
	out := make([]Code, len(baseCandidates))
	copy(out, baseCandidates)
	return out
}

// NameKeys 返回全部币种的名称文案键，按代码字典序。
//
// 供 i18n（L3）那一轮做穷尽性校验：语言文件必须覆盖这里的每一个键，
// 否则某个币种在界面上会渲染成空白或键名本身。
//
// 返回新切片，调用方的改动不会污染包状态。
func NameKeys() []string {
	out := make([]string, len(nameKeys))
	copy(out, nameKeys)
	return out
}

// DefaultBase 返回默认本位币 CNY（终版 I-2）。
//
// 用于 A-3 建 admin 账号与 A-2 建普通账号时的本位币初值。
// 返回值必然满足 CanBeBase（init 中已断言）。
func DefaultBase() Code { return defaultBase }
