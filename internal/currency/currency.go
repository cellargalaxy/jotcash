// Package currency 是**币种枚举**的唯一实现处：代码内置、不落库，库中只存币种代码。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md`（下称「分层」）§六 L1.0 `currency` 行：
// 「**币种枚举**：代码、名称、符号、**小数位数**、是否启用、**汇率获取实现的标识**
// （实现体不在此，由 `fxrate` 注册表按该标识路由，见准则 3）；只存代码入库；
// 提供合法性校验——应用层是唯一校验点（终版 §五 1）。**另出本位币候选口径（T38）**：
// `可作本位币 = 已启用 且 小数位数 ≤ 2`，对外提供候选集（K-1 下拉）与单值校验；
// **支出币种不受此限**（3 位币种仍可正常记账与摊分）」。
//
// 六项承载在本包的落点：
//
//	代码           → Code（本文件）
//	名称 / 符号     → Currency.Name / Currency.Symbol（table.go 的策展表）
//	小数位数        → Currency.Scale，int32 以直喂 base/decimal（见 Currency.Scale 注释）
//	是否启用        → Currency.Enabled + Enabled() / ValidateEnabled()
//	汇率获取实现标识 → FxSource（本文件），实现体在 L4 `fxrate/<源>`，由 `fxrate` 注册表路由
//	合法性校验      → Valid / Validate / ValidateEnabled / ValidateBase（本文件 + base.go）
//
// # 为什么枚举表必须在仓库内，而不是运行时查第三方库
//
// 终版 §五 1 有两条硬约束：「**枚举项只能停用不能删除**（历史明细存着旧代码）」与
// 「新增币种 = 改代码」。第三方币种库**做不到第一条**——它们会随版本删币种。实测
// （证据见 `doc/turn/2609p1/260906-092708-answer.md` §三 取舍 1）：
// `bojanz/currency` v1.4.4 里 SLL / MRO / VEF / ZWL / STD / BYR / HRK 全部
// `IsValid=false`（已被 SLE / MRU / VES / ZWG / STN / BYN 取代或退出流通），
// 而 `golang.org/x/text/currency` 里这些码仍在。若小数位数是运行时查库得来，
// 一次库升级就可能让某条历史明细的币种「查不到」，或让它的小数位数**静默变化**
// ——后者会改掉 8.2 摊分的均分基准与 T38 的本位币候选集，属准则 2 点名的
// 「算错就静默出错」。故本包把枚举表连同小数位数一起钉在仓库内，
// 第三方库只在**单测里**充当 ISO 4217 的比对基准（见 table_iso_test.go）。
//
// 另一条同样重要：**已在本仓库模块图里的 `golang.org/x/text/currency` 用来取小数位数是错的**。
// 它的 `Kind.Rounding` 返回的是 **CLDR 展示位数**，与 ISO 4217 的 minor unit 不是一件事。
// 实测 67 个币种里 **22 个不一致**（IDR 2→0、IQD 3→0、PKR 2→0、AMD 2→0…，
// 且其 CLDR 数据停在 v32）。摊分与折算的基准必须是 ISO 的 minor unit，
// 因此本包**刻意不用** x/text，并在单测中对 ISO 基准逐条断言，防止后人「顺手换成已有依赖」。
//
// # 本包不做什么
//
//   - **不做任何金额运算**。不变式 1a（折算）的唯一实现处是 `rule/convert`、
//     1b 与 8.2（摊分均分）是 `rule/amortize`（分层 §九）。本包只提供它们所需的
//     `小数位数`，**不 import `base/decimal`**——一旦本包能算钱，「唯一一处乘法与舍入」就破了。
//   - **不做币种代码的清洗与大小写归一**。与 `base/decimal.NewFromString`、
//     `base/calendar.ParseDate` 同一取向：本包严格，账单字段的 trim 与大小写整形归 `parser`
//     （分层 §六 L3 `parser` 行的「原始交易行」映射）。本包若顺手归一，
//     「这个字段到底是什么」的判断就分散到了两处。
//   - **不持有汇率获取的实现**，只持有它的标识（准则 3：枚举项里的「实现」只存标识，
//     注册表在 L3、实现体在 L4）。照搬「枚举项含实现」会让 L1 依赖 L3/L4 必成环。
//   - **不做界面文案的多语翻译**。K-4 明写「**仅界面文案**走语言配置文件，
//     **不含数据内容**（支出类型名称等）的翻译」，币种名称与符号同属数据内容，
//     本轮仅提供中文（K-4「本轮仅提供中文」）。
//
// # 依赖与分层
//
// 分层 §六 L1.0 表头「依赖 L0」、依赖列为 `base/*`：本包 import `base/errs` 以返回
// **带档位与文案键**的错误——`base/errs` 里的 `KeyCurrencyInvalid`
// （`currency.code.invalid`）与 `KeyCurrencyNotBaseCandidate`
// （`currency.code.not_base_candidate`）连同占位参数 `ArgCurrency` 都是为本包预留的，
// 故直接复用，不另造一套哨兵错误（对比 `base/calendar` 与 `base/decimal`：
// 它们在 L0 内、不能 import 兄弟包 `base/errs`，只能导出哨兵错误让上层转换；本包没有这个限制）。
package currency

import (
	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// Code 币种代码，即 ISO 4217 的三位字母代码（大写），**也是库中唯一落库的币种信息**
// （终版 §五 1「库中只存币种代码」）。
//
// 三条结构性设计：
//
//	① **可比较、可作 map 键**（与 `base/calendar.Date` 同、与 `base/decimal.Decimal` 刻意相反）：
//	   8.3 判重的四要素「归属用户 + 支出日期 + 支出金额 + 支出币种」要按
//	   「(日期, 金额, 币种)」元组批量取候选（分层 §六 L3 `store` ④「查重候选批量匹配」），
//	   元组必须能进 map。底层取 string 而非结构体，正是为了这一点。
//	② **零值即「未设置 / 留空」**：终版 §四 `Expense.卡片币种` 是非必填
//	   （「解析不出时留空」），而表头要求「业务上『不适用』的场景以零值表达，不留空」，
//	   零值 Code（空串）恰好承担这一语义，`IsZero` 是判它的入口。
//	   注意：`支出币种` 与 `折算本位币币种` 是必填，零值对它们即「上游漏填」。
//	③ **底层是 string 常量而非包级 var**：本包的 CNY 等是**真正的编译期常量**
//	   （`const CNY Code = "CNY"`），任何人都改不掉。若为了「不可构造非法值」把 Code 做成
//	   带私有字段的结构体，各币种就只能是包级 `var`——而包级 var 可被任何依赖方赋值，
//	   `currency.CNY = ...` 会全局改掉 I-2 的默认本位币，正是 `base/decimal` 拒绝
//	   shopspring 全局可变变量时点名的那类隐患。取舍代价（已登记）：包内代码写
//	   `Code("ZZZ")` 字面量在编译期拦不住，故本包在**每一处边界**都做校验
//	   （`UnmarshalText` / `Scan` / `Validate*`），非法值进不了 JSON 契约、也进不了库。
type Code string

// String 返回币种代码文本，供日志与 8.4 审计摘要（「导入 37 笔，合计 1,204.50 CNY」）使用。
func (c Code) String() string {
	return string(c)
}

// IsZero 是否为零值，即「未设置 / 留空」。
// 对应终版 §四 `Expense.卡片币种`「解析不出时留空」。
func (c Code) IsZero() bool {
	return c == ""
}

// FxSource 汇率获取实现的**标识**，不是实现本身（准则 3）。
//
// 分层 §六 L3 `fxrate` 行：「日汇率获取接口（按币种对 + 日期）+ **按 `currency` 的汇率源标识
// 路由的注册表**（与 `parser` 注册表同构，注册项由 `app` 从 L4 注入）」。
// 即：本包给出「这个币种该找谁取汇率」，`fxrate` 的注册表按此路由，实现体在 L4 `fxrate/<源>`。
//
// **本轮只有一个取值 FxSourceDefault**：具体汇率源尚未选定（分层 §十二 扩展预留位登记
// 「`fxrate/<源>` 的具体包名随汇率源选定后确定」、T31「I-7 取哪天汇率」由用户主动挂起），
// 属阶段 6 的事。本包现在就把**字段与路由键**摆出来，是为了避免重演分层 §三 修复过的
// 「字段有了却没有消费方」——将来某个币种要走专属汇率源时，改的是策展表里的一列，
// 不动任何接口形状。
type FxSource string

// FxSourceDefault 默认汇率源标识：全部已策展币种当前都走它。
//
// 取值刻意不写成某个具体源的名字（如 "ecb" / "boc"）：源未选定，写了就是猜。
// 阶段 6 选定后，要么把本常量的**取值**换成该源的键（一处改动，无调用方感知），
// 要么为走专属源的币种在策展表里另填一个 FxSource——两条路都不需要改本包的类型与函数。
const FxSourceDefault FxSource = "default"

// Valid 返回该值是否为**本包已收录**的汇率源标识。
// 供 `app` 在按标识注册 L4 实现时自检：注册了一个没有任何币种引用的源，
// 与引用了一个没人注册的源，都是装配错误。
func (s FxSource) Valid() bool {
	return s == FxSourceDefault
}

// Currency 一个币种枚举项，即终版 §五 1 列出的六个属性。
//
// **值语义、无方法修改自身**：`Get` 返回副本，调用方改不到本包的策展表
// （表本身是包级私有切片，也不对外暴露指针）。
type Currency struct {
	// Code 币种代码（ISO 4217 三位字母，大写），唯一标识，也是唯一落库的信息。
	Code Code

	// Name 中文名称，供 K-1 配置页与各处下拉展示。
	//
	// 只有中文：K-4「**仅界面文案**走语言配置文件……**不含数据内容**（支出类型名称等）的翻译」
	// + 「本轮仅提供中文」。将来若要多语币种名，那是给本结构加一个按语言取名的入口，
	// 不是把它塞进 `i18n` 的语言文件（那是界面文案，不是数据）。
	Name string

	// Symbol 币种符号，仅供展示（如金额前缀）。
	//
	// **不参与任何计算与判等**，也不保证全局唯一——SEK / NOK / DKK 的符号都是 `kr`，
	// CNY 与 JPY 在不同 locale 下都可能显示 `¥`。要唯一标识币种，用 Code。
	Symbol string

	// Scale 小数位数，即 ISO 4217 的 minor unit 位数。
	//
	// 它是**两处业务基准**，两处都不是「存储精度」（分层 §三 问题 1 已把二者拆开）：
	//	① 8.2 摊分「按币种的小数位数均分，除不尽的尾差固定计入末月」——原币按支出币种、
	//	   本位币按本位币，实现在 `rule/amortize`；
	//	② 不变式 1a 折算的舍入基准「按**本位币的** `currency.小数位数` 四舍五入」，
	//	   实现在 `rule/convert`。
	//
	// 类型取 int32 而非 int / uint8：`base/decimal` 的
	// `RoundAmount(d Decimal, currencyScale int32)` 与 `Round(d, scale int32, mode)`
	// 都收 int32，取同一类型可让调用点直接 `RoundAmount(v, cur.Scale)`，
	// 不必在每个调用点做一次转换——转换本身不难，但「哪里该转、转错了没」是无谓的复核负担。
	Scale int32

	// Enabled 是否启用。
	//
	// **停用不等于不合法**（终版 §五 1「枚举项只能停用不能删除（历史明细存着旧代码）」）：
	// 停用只是把该币种从「新录入可选集」里去掉，历史明细仍存着它、仍要能读回、能展示、
	// 能参与 I-7 本位币重算。这条区分落在两个不同的判据上，见 Valid 与 Enabled 的注释。
	Enabled bool

	// FxSource 该币种走哪个汇率源（标识，非实现）。见 FxSource 类型注释。
	FxSource FxSource
}

// Get 按代码取枚举项。第二个返回值为 false 表示该代码**不在枚举表内**（含停用项在内都算在表内）。
//
// 大小写敏感、不 trim：见包注释「本包不做什么」第 2 条。
func Get(code Code) (Currency, bool) {
	cur, ok := byCode[code]
	return cur, ok
}

// MustGet 按代码取枚举项，不存在即 panic。
// **仅供包内常量与测试使用**，禁止用于任何外部输入（账单文本、请求参数、库中读回的值）。
func MustGet(code Code) Currency {
	cur, ok := Get(code)
	if !ok {
		panic("currency：币种代码 " + code.String() + " 不在枚举表内")
	}
	return cur
}

// Valid 该代码是否为枚举表内的币种，**含已停用项**。
//
// 这是「合法性校验」的**读侧**判据（终版 §五 1「合法性校验落在应用层」，应用层是唯一校验点）：
// 凡是从库里读回、要展示、要参与 I-7 重算的币种代码，都用它判——**不能用 Enabled 判**，
// 否则一旦某币种被停用，历史明细当场变成读不出来的行（F-4 查看、F-8 复制、I-7 重算全断）。
//
// 零值（空串）返回 false：它表示「未设置」，不是一个币种。要判「留空」用 Code.IsZero。
func Valid(code Code) bool {
	_, ok := byCode[code]
	return ok
}

// Enabled 该代码是否为**已启用**的币种。
//
// 这是「合法性校验」的**写侧**判据：新录入 / 编辑时可选的币种集合以此为准
// （D-7 提交、F-5 保存的 `支出币种`）。与 Valid 的分工见 Currency.Enabled 注释。
func Enabled(code Code) bool {
	cur, ok := byCode[code]
	return ok && cur.Enabled
}

// Validate 校验币种代码合法（在枚举表内，含停用项），非法返回
// `base/errs` 的「用户输入错误」档 + 文案键 `currency.code.invalid`。
//
// 档位取 KindInput 是因为本函数的调用方是**入参校验**侧（`dto` 的写请求、`parser` 的原始交易行）。
// 库中读回时的同类失败属数据异常、不是用户输入错误，走 Code.Scan 的 KindSystem 档。
func Validate(code Code) error {
	if Valid(code) {
		return nil
	}
	return errs.NewInput(errs.KeyCurrencyInvalid, errs.A(errs.ArgCurrency, code.String()))
}

// ValidateEnabled 校验币种代码合法**且已启用**，供新录入与编辑的写侧使用。
//
// 停用项与未知代码返回同一档位与文案键（`currency.code.invalid`）：对用户而言
// 「这个币种现在不能选」是同一件事，而区分「不存在」与「已停用」会泄露枚举表的内部状态，
// 且 `base/errs` 未为「已停用」预留文案键——不凭空发明键（见 `base/errs/key.go` 收录范围）。
func ValidateEnabled(code Code) error {
	if Enabled(code) {
		return nil
	}
	return errs.NewInput(errs.KeyCurrencyInvalid, errs.A(errs.ArgCurrency, code.String()))
}

// Scale 返回该币种的小数位数，是 8.2 均分与不变式 1a 舍入的基准。
//
// 第二个返回值为 false 表示该代码不在枚举表内，此时第一个返回值为 0——
// **调用方必须检查它**：把未知币种静默当成 0 位会让摊分按整数均分，
// 属「算错就静默出错」。需要「查不到就报错」的调用点用 Get + Validate 组合，
// 或直接用本函数并对 ok 分支返回错误。
func Scale(code Code) (int32, bool) {
	cur, ok := byCode[code]
	if !ok {
		return 0, false
	}
	return cur.Scale, true
}

// All 返回全部枚举项（**含已停用**），顺序为策展表的声明顺序。
//
// 声明顺序即**展示顺序**（本位币 CNY 在首位，其后按常见程度与地区聚合），
// 因此 K-1 配置页与各处下拉可直接照此渲染，不必在前端另定一套排序。
// 顺序稳定是刻意的：若按 map 迭代返回，下拉项每次刷新都会跳动。
//
// 返回**副本**，调用方修改不影响本包的策展表。
func All() []Currency {
	out := make([]Currency, len(table))
	copy(out, table)
	return out
}

// EnabledAll 返回全部**已启用**的枚举项，顺序同 All。
// 供「新录入可选的支出币种」下拉使用；要展示历史明细里可能出现的停用币种，用 All。
func EnabledAll() []Currency {
	out := make([]Currency, 0, len(table))
	for _, cur := range table {
		if cur.Enabled {
			out = append(out, cur)
		}
	}
	return out
}

// MarshalText 实现 encoding.TextMarshaler，产出币种代码文本。
// `encoding/json` 会自动经它把本类型序列化为 JSON 字符串，因此无需另写 MarshalJSON。
//
// **零值序列化为空串而不报错**：`Expense.卡片币种` 非必填，留空是合法业务态
// （对比 `base/calendar.Date.MarshalText` 对零值报错——支出日期是必填字段）。
// 必填字段漏填的拦截在 `dto` 的必填校验，不在本方法。
//
// 不校验合法性：能构造出的 Code 都该已在边界处校验过；序列化端再报错只会把
// 一个早该在入口暴露的问题推迟到出口，且让「读一行历史数据」多出一条失败路径。
func (c Code) MarshalText() ([]byte, error) {
	return []byte(c), nil
}

// UnmarshalText 实现 encoding.TextUnmarshaler，**严格校验**：
// 空串接受（= 留空，见 MarshalText），非空则必须是枚举表内的代码，否则返回
// 「用户输入错误」档 + `currency.code.invalid`。
//
// 这是前端 JSON 契约（T34②）侧的闸门：客户端塞一个 "ZZZ" 或 "cny" 进来，
// 在反序列化这一步就被拦住，不会带着一个无处查小数位数的币种走到 `rule/amortize`。
//
// 收录停用项（走 Valid 而非 Enabled）：K-2 跳转、F-4 筛选等读链路会把库里的历史币种
// 原样送回服务端，若此处按 Enabled 判，停用币种的历史明细连筛选都做不了。
// 「新录入不许选停用币种」由 `dto` / service 侧显式调 ValidateEnabled 负责——
// 读写两侧的判据不同，不能合成一个。
func (c *Code) UnmarshalText(data []byte) error {
	parsed := Code(data)
	if parsed.IsZero() {
		*c = ""
		return nil
	}
	if err := Validate(parsed); err != nil {
		return err
	}
	*c = parsed
	return nil
}
