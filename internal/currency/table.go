package currency

// 本文件是币种枚举的**策展表**：终版 §五 1「新增币种 = 改代码」的那个「代码」就是这里。
//
// # 三条改动纪律
//
//  1. **只增不删**：「枚举项只能停用不能删除（历史明细存着旧代码）」。要下线一个币种，
//     把 Enabled 改成 false，**不要删行**——删了它的历史明细就查不到小数位数，
//     F-4 展示、8.2 摊分重算、I-7 本位币重算会一起断。
//  2. **Scale 一经使用不得修改**：它是 8.2 均分与不变式 1a 舍入的基准。改一个已有币种的
//     Scale，等于静默改掉该币种所有历史摊分的均分结果与所有折算的舍入位数，
//     且不会有任何报错——属准则 2 点名的「算错就静默出错」。ISO 4217 真的改了某币种的
//     minor unit（如 2018 年 MRO→MRU）时，正确做法是**新增一行新代码**并停用旧代码，
//     这也正是 ISO 自己的做法。
//  3. **Scale 必须等于 ISO 4217 的 minor unit**，不是 CLDR 展示位数、不是「习惯写几位」。
//     table_iso_test.go 会拿 `github.com/bojanz/currency`（其数据直接取自 ISO 官方
//     list-one.xml）对本表逐条断言，填错会在单测里失败。
//
// # 币种范围（为什么是这 33 个而不是 165 个）
//
// 需求是「个人 / 家庭记账」（终版 §一），账单来源是 D-1 的银行 / 信用卡账单。全量收录
// 165 个 ISO 币种会让 K-1 的下拉变成一条几百项的长列表，而其中绝大多数
// （BIF / GNF / KPW…）对目标用户永不出现——这属于分层 §三 修复过的「字段有了却没有消费方」。
// 故本表策展为「中国大陆个人用户在境内消费与境外旅行 / 网购中实际会遇到的币种」，
// 并**刻意覆盖全部四类小数位数分支**，让 8.2 摊分与 1a 折算的边界在真实数据上就能被走到：
//
//	0 位：JPY / KRW / VND —— 均分即整数分配，无尾差小数
//	2 位：CNY / USD / EUR 等 26 个 —— 主流分支
//	3 位：KWD / BHD / OMR / JOD —— T38 的「不可作本位币」分支（Scale > 2）
//	4 位：**刻意不收录**。ISO 里 4 位的只有 CLF（智利 UF）与 UYW（乌拉圭名义工资指数），
//	      二者都是记账单位（unit of account）而非可支付货币，不会出现在个人银行账单上。
//	      需要时按纪律 1 新增即可。
//
// 不收录的另外三类，以及为什么：
//
//	非流通代码（XAU 黄金 / XDR 特别提款权 / XXX 无币种 / BOV、CHE 等基金码）：
//	  它们不是可支付货币；`bojanz/currency` 对 XAU / XDR / XXX 一律 IsValid=false，
//	  收录会与 ISO「流通货币」的口径打架。
//	加密货币（BTC / USDT 等）：不在 ISO 4217 内，无官方 minor unit，
//	  且小数位数远超本仓库 `base/decimal` 的金额档（8 位小数以上），会连带影响 store 列宽决策。
//	CNH（离线人民币）：非 ISO 4217 代码（ISO 侧人民币只有 CNY），
//	  `bojanz/currency` IsValid=false。账单上出现 CNH 时应由 `parser` 归一到 CNY。
//
// # 顺序即展示顺序
//
// 声明顺序被 All / EnabledAll 原样返回，K-1 与各处下拉据此渲染：
// CNY 在首位（I-2 的默认本位币），其后按「主要国际货币 → 亚太 → 其他地区 → 0 位 → 3 位」聚合。
//
// # 符号取值
//
// Symbol 取**中文环境下的惯用写法**（与 K-4「本轮仅提供中文」一致）。
// 无广泛认知专属符号的币种（CHF / SGD / MOP / THB 等）直接以代码作符号——
// 这是 CLDR 在 zh-Hans 下的实际取值（实测 165 个币种里 143 个如此），
// 也比生造一个用户认不出的符号更可用。符号不参与计算与判等，见 Currency.Symbol 注释。
var table = []Currency{
	// —— 本位币与主要国际货币 ——
	{Code: CNY, Name: "人民币", Symbol: "¥", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: USD, Name: "美元", Symbol: "US$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: EUR, Name: "欧元", Symbol: "€", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: GBP, Name: "英镑", Symbol: "£", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: CHF, Name: "瑞士法郎", Symbol: "CHF", Scale: 2, Enabled: true, FxSource: FxSourceDefault},

	// —— 亚太 ——
	{Code: HKD, Name: "港元", Symbol: "HK$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: MOP, Name: "澳门元", Symbol: "MOP", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: TWD, Name: "新台币", Symbol: "NT$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: SGD, Name: "新加坡元", Symbol: "SGD", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: AUD, Name: "澳大利亚元", Symbol: "AU$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: NZD, Name: "新西兰元", Symbol: "NZ$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: CAD, Name: "加拿大元", Symbol: "CA$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: THB, Name: "泰铢", Symbol: "THB", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: MYR, Name: "马来西亚林吉特", Symbol: "MYR", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: PHP, Name: "菲律宾比索", Symbol: "₱", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: IDR, Name: "印尼卢比", Symbol: "IDR", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: INR, Name: "印度卢比", Symbol: "₹", Scale: 2, Enabled: true, FxSource: FxSourceDefault},

	// —— 其他地区 ——
	{Code: RUB, Name: "俄罗斯卢布", Symbol: "RUB", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: SEK, Name: "瑞典克朗", Symbol: "SEK", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: NOK, Name: "挪威克朗", Symbol: "NOK", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: DKK, Name: "丹麦克朗", Symbol: "DKK", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: TRY, Name: "土耳其里拉", Symbol: "TRY", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: ZAR, Name: "南非兰特", Symbol: "ZAR", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: BRL, Name: "巴西雷亚尔", Symbol: "R$", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: AED, Name: "阿联酋迪拉姆", Symbol: "AED", Scale: 2, Enabled: true, FxSource: FxSourceDefault},
	{Code: SAR, Name: "沙特里亚尔", Symbol: "SAR", Scale: 2, Enabled: true, FxSource: FxSourceDefault},

	// —— 0 位小数（均分即整数分配，8.2 的无尾差小数分支）——
	{Code: JPY, Name: "日元", Symbol: "JP¥", Scale: 0, Enabled: true, FxSource: FxSourceDefault},
	{Code: KRW, Name: "韩元", Symbol: "₩", Scale: 0, Enabled: true, FxSource: FxSourceDefault},
	{Code: VND, Name: "越南盾", Symbol: "₫", Scale: 0, Enabled: true, FxSource: FxSourceDefault},

	// —— 3 位小数（T38 的「不可作本位币」分支：Scale > 2，但可正常作支出币种）——
	{Code: KWD, Name: "科威特第纳尔", Symbol: "KWD", Scale: 3, Enabled: true, FxSource: FxSourceDefault},
	{Code: BHD, Name: "巴林第纳尔", Symbol: "BHD", Scale: 3, Enabled: true, FxSource: FxSourceDefault},
	{Code: OMR, Name: "阿曼里亚尔", Symbol: "OMR", Scale: 3, Enabled: true, FxSource: FxSourceDefault},
	{Code: JOD, Name: "约旦第纳尔", Symbol: "JOD", Scale: 3, Enabled: true, FxSource: FxSourceDefault},
}

// 币种代码常量。
//
// 是**编译期常量**而非包级 var：任何依赖方都无法赋值改写（见 Code 类型注释 ③）。
// 调用方应优先用这些常量而非字符串字面量——`currency.CNY` 拼错编译不过，`Code("CNY")` 拼错要等运行时。
const (
	// CNY 人民币，I-2 的默认本位币（终版：本位币默认 CNY）。
	CNY Code = "CNY"
	USD Code = "USD"
	EUR Code = "EUR"
	GBP Code = "GBP"
	CHF Code = "CHF"

	HKD Code = "HKD"
	MOP Code = "MOP"
	TWD Code = "TWD"
	SGD Code = "SGD"
	AUD Code = "AUD"
	NZD Code = "NZD"
	CAD Code = "CAD"
	THB Code = "THB"
	MYR Code = "MYR"
	PHP Code = "PHP"
	IDR Code = "IDR"
	INR Code = "INR"

	RUB Code = "RUB"
	SEK Code = "SEK"
	NOK Code = "NOK"
	DKK Code = "DKK"
	TRY Code = "TRY"
	ZAR Code = "ZAR"
	BRL Code = "BRL"
	AED Code = "AED"
	SAR Code = "SAR"

	JPY Code = "JPY"
	KRW Code = "KRW"
	VND Code = "VND"

	KWD Code = "KWD"
	BHD Code = "BHD"
	OMR Code = "OMR"
	JOD Code = "JOD"
)

// byCode 是 table 的代码索引，供 Get / Valid / Enabled / Scale 做 O(1) 查询。
//
// 在 init 里构建而非手写字面量：手写等于把同一份数据抄两遍，两处迟早不一致。
// init 同时承担**启动即失败**的自检（见 init 注释）。
var byCode map[Code]Currency

func init() {
	byCode = make(map[Code]Currency, len(table))
	for _, cur := range table {
		// 自检：策展表写错时在**进程启动**就 panic，而不是等到某笔支出摊分时才算错。
		// 这几项都是「写错了但不会报错」的类型——正是准则 2 要求当场失败的场景。
		if len(cur.Code) != 3 {
			panic("currency：币种代码 " + cur.Code.String() + " 不是三位字母")
		}
		for i := 0; i < len(cur.Code); i++ {
			if c := cur.Code[i]; c < 'A' || c > 'Z' {
				panic("currency：币种代码 " + cur.Code.String() + " 含非大写字母字符")
			}
		}
		if _, dup := byCode[cur.Code]; dup {
			panic("currency：币种代码 " + cur.Code.String() + " 重复")
		}
		if cur.Name == "" {
			panic("currency：币种 " + cur.Code.String() + " 缺少名称")
		}
		if cur.Symbol == "" {
			panic("currency：币种 " + cur.Code.String() + " 缺少符号")
		}
		// 上界 4 取自 ISO 4217 实际最大 minor unit（CLF / UYW 为 4）。
		if cur.Scale < 0 || cur.Scale > 4 {
			panic("currency：币种 " + cur.Code.String() + " 的小数位数超出 ISO 4217 取值范围 [0,4]")
		}
		// 汇率源标识必须是本包收录的取值，否则 fxrate 注册表将路由到一个无人注册的源。
		if !cur.FxSource.Valid() {
			panic("currency：币种 " + cur.Code.String() + " 的汇率源标识 " + string(cur.FxSource) + " 未收录")
		}
		byCode[cur.Code] = cur
	}
}
