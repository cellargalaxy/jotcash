// Package currency 是 jotcash 全仓库唯一的币种来源（L1.0 领域基元层）。
//
// # 承载的六项属性
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L1.0 表
// currency 行，本包承载「币种枚举：代码、名称、符号、小数位数、是否启用、
// 汇率获取实现的标识」，恰是这六项，一项不多一项不少。其中后两项是本系统专有
// 语义，第三方币种库一概不提供（详见「为什么不直接用第三方库」）。
//
// 六项属性中，**小数位数**是唯一「算错就静默出错」的一项：它同时是
//
//   - 8.1 折算的舍入基准（分层 §九 不变式 1a：「按本位币的 currency.小数位数
//     四舍五入」，唯一实现处 rule/convert）；
//   - 8.2 摊分的均分基准（不变式 1b：「按币种小数位数均分」，原币按支出币种、
//     本位币按本位币，唯一实现处 rule/amortize）。
//
// 位数取错不会报错，只会让金额差一到两个分位。故本包的枚举表在测试期与
// CLDR 48.2.0 逐项交叉校验（table_test.go 的 TestScaleMatchesCLDR），
// 而不是凭记忆写死。
//
// # 三个谓词，互不等价
//
// 终版「记账系统功能与模型」I-1 定死「新增币种须改代码，枚举项只能停用不能
// 删除（历史明细存着旧代码）」。这句话要求「已知」与「已启用」必须是两个不同
// 的谓词；T38 又在其上叠了第三个：
//
//	已知        Parse 成功        在表内（含已停用）  store 回读历史明细、K-3 导出
//	已启用      Enabled()         已知 且 enabled     D-2/F-5 选支出币种、F-4 筛选
//	可作本位币  CanBeBase()       已启用 且 位数 ≤ 2  K-1 本位币下拉、preference 切换
//
// 三者严格收窄，缺一处就出错：若 Scan 建立在「已启用」上，某币种一旦停用，
// 引用它的历史明细当场读不出来——F-4「显示已删除」查看、K-3 整库导出、
// I-7 逐笔重算（含已软删明细）三处同时失效。
//
// 反过来，「可作本位币」**不**限制支出币种：分层 §六 L1.0 明写「支出币种不受
// 此限（3 位币种仍可正常记账与摊分）」。表中保留 KWD（3 位）正是为了让这条
// 规则可被测试——删掉它，这条规则就无从验证。
//
// # 为什么不直接用第三方库
//
// 调研并实测了 4 个候选（bojanz/currency 644★、golang.org/x/text/currency、
// Rhymond/go-money 1912★、JohannesJHN/iso4217）。结论是**没有一家提供
// 「是否启用」与「汇率获取实现的标识」这两项**——它们是本系统专有语义。照用
// 第三方库仍要自建一张并行表放这两项，于是小数位数会出现两个来源，这是最该
// 避免的形态。
//
// 此外 bojanz/currency 有三处实测问题使它不适合作运行时依赖：
//
//   - 它的 Register 直接改包级 map，任何 import 到它的包都能把 CNY 的小数
//     位数从 2 改成 0（实测生效）。而该值是 8.1/8.2 的计算基准，被改即全系统
//     金额静默出错。这与 base/decimal 拒绝暴露第三方库 Div（受包级可变量
//     DivisionPrecision 控制）是同一条取向，不能在此自相矛盾。
//   - Register 与读操作并发不安全：50 个读 goroutine 与 1 个 Register 并发，
//     go test -race 当场报 DATA RACE。本包的读取方遍布全部请求协程。
//   - IsValid("") 返回 true（实测）。而 Expense.支出币种 与 User.本位币 都是
//     必填，照用其校验会让「币种为空」静默通过。
//
// 故本包自建枚举表（编译期常量、初始化后只读、无任何写入口），而把
// bojanz/currency 降为 **test-only 依赖**，只在测试中作小数位数的交叉校验
// 基准：既让这项数据有权威来源可核对、CLDR 升级导致的漂移会被测试自动暴露，
// 又把它的可变性与并发问题隔离在测试进程内，不进生产二进制。
//
// # 不做什么
//
//   - **不含汇率获取实现**。按分层 §六 准则 3，枚举项里的「实现」只存标识，
//     注册表在 L3（fxrate）、实现体在 L4。照搬「枚举项含实现」会让 L1.0
//     依赖 L3/L4，必成环。
//   - **不含金额运算**。本包只出「小数位数」这个参数，不 import decimal，
//     更不做乘法与舍入——那是 rule/convert 与 rule/amortize 的唯一实现处。
//   - **不含面向用户的币种名称渲染**。终版 K-4 要求界面文案走语言配置、加语种
//     不改代码，而 i18n 在 L3，本包 import 它会成环。故本包出文案键
//     （NameKey），渲染归 L6。Name() 返回的中文名只供日志与排错。
//   - **不含任何注册/修改入口**。提供 Register 等于把终版 I-1 的「改代码」
//     降级成「运行期任意包可改」，该条当场失效。有回归用例 TestNoMutationAPI
//     扫描导出标识符钉住这一点。
//
// # 依赖边界
//
// 承载表的依赖列写的是 base/*（普遍依赖口径），但实际实现下来本包**零仓库内
// 依赖**：小数位数只是个 int32 常量，错误用哨兵表达。这比承载要求更收敛。
//
// 特别地本包不 import base/errs：与同为基础层的 decimal、calendar 一致，
// 错误一律为哨兵错误，由上层用 errors.Is 判定后映射到 base/errs 的五档与
// 文案键（例如 service/preference 把 ErrNotBaseCandidate 映射为 D 档 +
// 对应文案）。本包不含任何面向用户的文案。
package currency

import (
	"errors"
	"fmt"
)

// 四个哨兵错误。分成多档而不是一个笼统的「非法币种」，是因为上层要给不同文案：
// 空值提示「请选择币种」、未知提示「不支持该币种」、非候选提示「该币种不能作
// 本位币」——后者是 T38④「非候选一律拒绝」在用户侧的表现，与前两者性质不同。
//
// 一律用标准库 errors.New 构造、一律用 fmt.Errorf("%w: …") 附加上下文，
// **不用 pkg/errors.New / Wrapf**。差别不在措辞而在调用栈：pkg/errors.New 在
// **变量初始化那一刻**就抓栈，而包级变量的初始化发生在 init 阶段，于是这四个
// 哨兵永久携带一份指向 currency.init 的栈。此后 base/errs.Wrap 的 hasStack
// （errs.go:107）会因为「链上已有栈」而跳过补栈，middleware 打 %+v 时看到的
// 栈顶就是包初始化，真正的出错调用点根本不在栈里——排错时会被引向完全错误的
// 方向。同层 decimal、calendar、enum 的哨兵都用标准库构造，本包与之对齐。
var (
	// ErrEmptyCode 表示币种代码为空。
	//
	// Expense.支出币种 与 User.本位币 均为必填（终版 §四），空值须在入口即被
	// 拦下。与 ErrUnknownCode 分档，避免上层把「没填」和「填错」显示成同一句话。
	ErrEmptyCode = errors.New("currency: 币种代码为空")

	// ErrUnknownCode 表示币种代码不在枚举表内。
	//
	// 「不在表内」包含两种情形：真的不支持（如 XXX），以及有人违规删除了枚举项
	// 而库中仍有历史明细引用它（终版 I-1 禁止的操作）。两者都必须响亮失败——
	// 静默降级为零值会让该笔明细的折算基准凭空消失。
	ErrUnknownCode = errors.New("currency: 未知的币种代码")

	// ErrNotBaseCandidate 表示该币种已知但不满足本位币候选口径（T38）。
	//
	// 典型是 KWD 这类 3 位小数币种：可以正常作支出币种记账与摊分，
	// 但不能作本位币——因为本位币金额按 2 位定点存储（decimal.BaseAmountStoreScale），
	// 3 位算出的结果无法被无损容纳。
	ErrNotBaseCandidate = errors.New("currency: 该币种不能作为本位币")

	// ErrScanType 表示 sql.Scanner 收到无法处理的类型。
	ErrScanType = errors.New("currency: 不支持的 Scan 源类型")
)

// Code 是币种代码，承载终版 §四 的 Expense.支出币种、User.本位币、
// 卡片币种 与 折算本位币币种 四个字段。
//
// 零值表示「未指定」：用于 卡片币种「解析不出则留空」的场景，
// 落库时映射为 SQL NULL、序列化为 JSON null。
//
// 内部字段私有，只能经 Parse / MustParse / ParseBase 构造，因此**一个 Code
// 要么 IsZero，要么是枚举表内已知的币种**，不存在 Code("蛋炒饭") 这种值。
// 这是承载「提供合法性校验——应用层是唯一校验点」（终版 §五 1）能够成立的
// 结构基础：校验收敛在构造函数，上层不必逐处自觉调用 IsValid。
//
// 不做成 type Code string：那样任何字符串都能强转，校验就退化成「各调用方
// 自觉调用」。也不用裸 string：分层 §六 L1.1 明令「币种……等字段直接用
// L1.0 的枚举类型……不用裸 string」。
//
// **可比较**：== 与 map[Code]T 都可用。这一点与 decimal.Decimal 刻意做成
// 不可比较相反，理由是币种是标称值——两个 Code 相等当且仅当代码相同，不存在
// 「1.50 与 1.5 表示不同而数值相同」那种情形。8.3 判重的四要素含支出币种，
// == 在此是正确语义。
type Code struct {
	// entry 指向枚举表中的条目；nil 即零值（未指定）。
	//
	// 存指针而非字符串代码，使属性访问无需二次查表；表在初始化后只读，
	// 因此指针共享是安全的。Code 仍可比较：同一代码必然指向同一条目。
	entry *entry
}

// Parse 解析币种代码，是本包唯一的合法性校验入口。
//
// 只要代码在枚举表内即成功，**含已停用的币种**——这是终版 I-1「枚举项只能
// 停用不能删除（历史明细存着旧代码）」的直接要求：停用只影响「能否新选」
// （见 Enabled），不影响「能否读出」。
//
// 不做大小写归一，"cny" 一律返回 ErrUnknownCode。输入来自 JSON 契约、URL
// 参数与库中已落的代码，三者都是机器生成的规范形态；放行 "cny" 会让同一币种
// 出现多种文本写法，store 的币种维度分组聚合（J-4）与 8.3 四要素判重就不再
// 能按字符串比对。同理于 calendar 拒绝 "2026-1-2"、idgen 拒绝 "007"。
func Parse(code string) (Code, error) {
	if code == "" {
		return Code{}, ErrEmptyCode
	}
	e, ok := table[code]
	if !ok {
		return Code{}, fmt.Errorf("%w: code=%q", ErrUnknownCode, code)
	}
	return Code{entry: e}, nil
}

// MustParse 解析币种代码，失败时 panic。
// 仅供测试与包内常量初始化使用，业务代码一律用 Parse。
func MustParse(code string) Code {
	c, err := Parse(code)
	if err != nil {
		panic(fmt.Sprintf("currency: 解析币种失败, code=%q: %v", code, err))
	}
	return c
}

// ParseBase 解析并校验「可作本位币」，供 service/preference 的本位币切换
// （K-1）直接调用，无需自行遍历 BaseCandidates。
//
// 这是 T38④「非候选一律拒绝」的落点。三种失败各自分档：空 → ErrEmptyCode，
// 未知 → ErrUnknownCode，已知但非候选（如 KWD、已停用币种）→ ErrNotBaseCandidate。
func ParseBase(code string) (Code, error) {
	c, err := Parse(code)
	if err != nil {
		return Code{}, err
	}
	if !c.CanBeBase() {
		return Code{}, fmt.Errorf("%w: code=%q, scale=%d, enabled=%t",
			ErrNotBaseCandidate, code, c.Scale(), c.Enabled())
	}
	return c, nil
}

// IsKnown 报告代码是否在枚举表内（含已停用）。
//
// 只做判定不做构造，供调用方在不需要 Code 值时使用（如导入时的预检）。
// 需要 Code 值时直接用 Parse，不要「先 IsKnown 再 Parse」——那是两次查表，
// 且两步之间的写法容易在重构中走散。
func IsKnown(code string) bool {
	if code == "" {
		return false
	}
	_, ok := table[code]
	return ok
}

// String 返回币种代码，即落库与 JSON 的形态；零值返回空串。
//
// 零值不返回 "UNKNOWN" 之类的占位：那是个 Parse 解不回来的非法字面值，
// 一旦落库或下发就成脏数据。同 calendar.Date.String 的取向。
func (c Code) String() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.code
}

// Name 返回币种中文名，**仅供日志与排错**；零值返回空串。
//
// 不得作为面向用户的文案下发：终版 K-4 要求界面文案走语言配置、加语种不改
// 代码，面向用户的名称须经 NameKey 取键、由 L6 渲染。这与 base/errs 的
// Kind.String() 返回中文档位名只供日志是同一取向。
func (c Code) Name() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.name
}

// NameKey 返回币种名称的文案键，形如 "currency.name.CNY"；零值返回空串。
//
// 供 i18n（L3）在语言文件中落词、由 L6 渲染。加语种只需加语言文件、不改本包
// 代码，终版 K-4 因此成立。本包不 import i18n（它在 L3，反向依赖成环）。
func (c Code) NameKey() string {
	if c.entry == nil {
		return ""
	}
	return nameKeyPrefix + c.entry.code
}

// Symbol 返回币种符号，如 "¥"、"US$"；零值返回空串。
//
// 符号是语言无关的记号，故直接内置而不进语言文件（与 NameKey 的处理相反）。
func (c Code) Symbol() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.symbol
}

// Scale 返回币种小数位数，是 8.1 折算舍入与 8.2 摊分均分的基准；零值返回 0。
//
// 类型取 int32 而非 uint8，是为了与 decimal.Round(scale int32, mode) 和
// decimal.QuoRem(divisor, scale int32) 的参数类型对齐——两个消费方
// （rule/convert、rule/amortize）拿到它立刻要喂给这两个函数。出 uint8 会让
// 每个调用点写一次类型转换，而位数写错就是金额算错。
//
// **调用前须先判 IsZero**：零值返回的 0 与「JPY 的小数位数 0」无法区分，
// 若拿一个零值 Code 去做舍入基准，712.35 会被静默舍成 712——钱少一截且不
// 报错。本包不 panic（零值可用是 Code 的既定语义，实体以零值创建），这条
// 前置要求由 consumer_check_test.go 中的消费方走查用例钉住。
func (c Code) Scale() int32 {
	if c.entry == nil {
		return 0
	}
	return c.entry.scale
}

// Enabled 报告币种是否启用；零值返回 false。
//
// 停用的币种仍可被 Parse 与 Scan 读出（终版 I-1：历史明细存着旧代码），
// 但不应出现在「新记账时可选的支出币种」（D-2/F-5）与筛选下拉（F-4）中。
func (c Code) Enabled() bool {
	if c.entry == nil {
		return false
	}
	return c.entry.enabled
}

// RateSource 返回该币种的汇率获取实现标识；零值返回空串。
//
// 按分层 §六 准则 3，本包**只存标识**：注册表在 L3（fxrate）、实现体在 L4
// （fxrate/<源>）。若把实现放进枚举项，L1.0 就会依赖 L3/L4，必成环。
func (c Code) RateSource() RateSource {
	if c.entry == nil {
		return ""
	}
	return c.entry.rateSource
}

// IsZero 报告是否为零值（未指定）。
func (c Code) IsZero() bool { return c.entry == nil }

// CanBeBase 报告该币种能否作为本位币，即 T38 的候选口径：
// **已启用 且 小数位数 ≤ 2**；零值返回 false。
//
// 两个判据缺一不可：
//
//   - 已启用——停用的币种不该出现在 K-1 的本位币下拉里；
//   - 小数位数 ≤ 2——本位币金额按 decimal.BaseAmountStoreScale（2 位定点）
//     存储，3 位币种（KWD/BHD/OMR 等）算出的金额无法被无损容纳。
//
// 这条判据**只约束本位币**：支出币种不受此限，KWD 仍可正常记账与摊分
// （分层 §六 L1.0 明写）。两者是独立谓词，不得合并。
func (c Code) CanBeBase() bool {
	if c.entry == nil {
		return false
	}
	return c.entry.enabled && c.entry.scale <= maxBaseScale
}

// Equal 报告两个币种是否相同。零值与零值相等。
//
// Code 可比较，== 与 Equal 等价；提供 Equal 是为了让比较意图在调用处更显眼，
// 也避免将来内部表示变化时调用方受影响（同 calendar.Date.Equal 的取向）。
func (c Code) Equal(other Code) bool { return c.entry == other.entry }
