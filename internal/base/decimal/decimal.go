// Package decimal 提供全仓库统一的定点小数类型与舍入，是**唯一的金额与汇率运算实现处**。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/decimal` 行：
// 「定点小数类型与 `round(值, 位数, 舍入方式)`；**金额一律不用浮点**。
// **三处精度口径（全部已定，无待确认）**：① **原币金额**按 `currency.小数位数`；
// ② **本位币金额**——**算：按本位币的 `currency.小数位数` 四舍五入**（T38 保证 ≤ 2 位）；
// **存：2 位定点**（T35，0/1 位结果无损容纳）；③ **折算汇率 8 位小数、四舍五入**（T39）」。
//
// 五条硬约束（决定了本包每一个签名的形状）：
//
//  1. **L0 层内零依赖**（§六 L0 表头「仅依赖标准库与第三方库」+ 约定 5）：本包不 import
//     任何 `internal/` 包。因此错误用 `github.com/pkg/errors` 包装本包哨兵值而
//     **不用 `base/errs`**（那是 L0 兄弟包）；映射到五档是调用方的事——`parser` 的
//     D-3 五类解析错误、`dto` 的入参校验都会把本包错误转成带文案键的 `base/errs`。
//  2. **不含业务语义**（约定 5：`base/` 是纯技术层，币种类型不得放入）：本包**不认识币种**，
//     「小数位数」一律由调用方以 int32 传入。币种枚举在 §六 L1.0 的 `currency`，
//     依赖它会反向跨层，也会让「纯技术层」失效。
//  3. **金额一律不用浮点**（同行承载）：本包**不提供任何 float32/float64 的出入参**。
//     写成注释等于没写——只有出口面里根本没有浮点，才不可能出现「先经浮点中转」。
//  4. **算与存是两件事**（§三 问题 1 的修法）：舍入基准是币种小数位数、存储精度是 2 位定点。
//     承载①②的「算」走 RoundAmount（位数由调用方传），T35 的「存」走 StoreBaseAmount。
//     本位币为 JPY（0 位）时按 2 位舍会算出 `200.20 JPY` 这种该币种不存在的面额，
//     紧接着 8.2 要按 JPY 的 0 位均分这个带小数的总额，尾差归末月也归不平。
//  5. **业务阈值写死在本包**（约定 8：「业务阈值一律写死在对应包……汇率 8 位 / 本位币 2 位」）：
//     RateScale 与 BaseAmountStoreScale 是业务规则、不进 `base/config`，
//     也不出现在任何导出函数的入参——**没有任何入口能把精度口径改掉**。
//
// 主要调用方（§六 L2/L3/L4 各行与 §九 不变式 1a/1b 行）：
//
//	rule/convert    → RoundRate / Mul / RoundAmount   不变式 1a：汇率规整 8 位 → 精确相乘
//	                                                  → 按本位币小数位舍入（全仓库唯一一处
//	                                                  金额乘法与舍入）
//	rule/amortize   → DivTruncate / Add / Sub         不变式 1b + 8.2 按币种小数位均分、尾差归末月
//	entity          → Decimal                        `Expense` 的金额与汇率字段类型（§六 L1.1：
//	                                                  「金额与汇率用 base/decimal，不用裸 string」）
//	store/sqlite    → StoreBaseAmount / StoreRate /   §六 L4：金额与汇率禁用 REAL，
//	                  FixedString / NewFromString     一律「整数最小单位或定点文本」
//	api/http/dto    → MarshalJSON / UnmarshalJSON     前后端 JSON 契约（T34②）
//
// 底层采用 github.com/shopspring/decimal（MIT、无间接依赖、`go_common` 亦在用）做大数
// 定点运算，但**不以类型别名直接暴露**：该库的 NewFromFloat 会当场违反约束 3，其 Div 的
// 默认精度与 MarshalJSON 的形态又取自包级**全局可变变量**（DivisionPrecision、
// MarshalJSONWithoutQuotes），任何依赖方（含第三方库）改动它们都会全局改变本仓库的
// 金额语义。故以薄包装收窄出口面，本包完全掌握语义。
//
// 本包不打日志、不收 context：它是纯计算包，被 I-7 逐笔重算与 K-3 整库导出按行调用，
// 每次入参校验失败都打一行日志只会淹掉真正的错误；诊断信息随 error 返回，由调用方
// 在有业务语境处记录（与 `base/errs`、`base/idgen` 两个纯计算包的取向一致）。
package decimal

import (
	"math"
	"strings"

	"github.com/pkg/errors"
	shop "github.com/shopspring/decimal"
)

// 哨兵错误：供上层用 errors.Is 判定后转成 `base/errs` 的对应档与文案键。
//
// 一律用 errors.Wrapf(哨兵, ...) 附加上下文而非 errors.Errorf 另造错误：
// 后者返回的 fundamental **没有 Unwrap**，errors.Is 判不出哨兵，
// 调用方只能退化为匹配错误字符串——那会在文案微调时静默失效。
var (
	// ErrParse 文本不是合法的十进制数值
	ErrParse = errors.New("数值文本非法")
	// ErrScale 小数位数超出允许范围
	ErrScale = errors.New("小数位数非法")
	// ErrRoundMode 舍入方式不是已定义的枚举值
	ErrRoundMode = errors.New("舍入方式非法")
	// ErrDivZero 除数为零
	ErrDivZero = errors.New("除数为零")
	// ErrOverflow 数值规模超出本包支持的范围
	ErrOverflow = errors.New("数值超出支持范围")
	// ErrPrecisionLoss 以目标小数位数无法无损表示当前值
	ErrPrecisionLoss = errors.New("目标小数位数无法无损表示该值")
)

// Decimal 定点小数，用于全仓库的金额与汇率。
//
// 值语义，零值即数值 0 且可直接参与全部运算——终版 §四 表头「业务上『不适用』的场景
// 以零值表达，不留空」要求零值开箱可算，否则 `entity` 的每个金额字段都得先判空再用。
//
// **刻意不可比较**：内含 *big.Int，用 == 比的是指针，同一数值经不同路径构造必然得到
// 不同指针，`a == b` 会**静默**给出 false。金额比较错正是准则 2 点名的「算错就静默出错」，
// 故以零尺寸字段令类型不可比较，把误用变成**编译错误**，强制走 Equal / Cmp。
// 连带后果（已登记）：内嵌本类型的结构体（如 `entity.Expense`）同样不可用 == 比较，
// 需逐字段 Equal 或 reflect.DeepEqual——这正是期望的行为，整条明细本就不该用 == 判等。
type Decimal struct {
	_ [0]func() // 阻断 == 比较，零尺寸、不占内存
	v shop.Decimal
}

// MaxScale 允许的最大小数位数。
//
// **技术防御上限，不是业务口径**：业务侧最大小数位是汇率的 8 位（T39），
// ISO 4217 币种最多 4 位小数，18 已宽松到不会挡住任何合法业务值，
// 也容得下 T39② 所述「`fxrate` 返回值精度不设限」的现实取值（常见 6~12 位）。
// 设此上限是为了把 `1e-2000000000` 这类极端指数挡在入口，
// 否则后续舍入会在内部做百万位的大整数幂运算，退化为一次拒绝服务。
const MaxScale int32 = 18

// MaxDigits 允许的最大有效数字位数。
// 同为技术防御上限，取值对齐主流数据库 DECIMAL(38, x) 的精度上限惯例。
const MaxDigits = 38

// wrap 把底层值包装为本包类型。
func wrap(v shop.Decimal) Decimal {
	return Decimal{v: v}
}

// NewFromString 从十进制文本构造，是本包**唯一的文本入口**。
//
// 接受整数、小数与科学计数法（如 `1.5e-2`）；**不接受前后空格**——
// 账单字段的清洗归 `parser`（§六 L3 `parser` 行的「原始交易行」映射），
// 本包若顺手 trim，「这个字段到底是什么」的判断就分散到了两处。
//
// 三类失败：文本非法返回 ErrParse；小数位数超 MaxScale、有效数字超 MaxDigits
// 返回 ErrOverflow。失败时一律返回零值，调用方即便忽略 error 也不会拿到脏值。
func NewFromString(s string) (Decimal, error) {
	if s == "" {
		return Decimal{}, errors.Wrap(ErrParse, "空字符串")
	}
	v, err := shop.NewFromString(s)
	if err != nil {
		return Decimal{}, errors.Wrapf(ErrParse, "%q", s)
	}
	d := wrap(v)
	if err := d.checkRange(); err != nil {
		return Decimal{}, errors.Wrapf(err, "%q", s)
	}
	return d, nil
}

// MustFromString 与 NewFromString 相同，但非法输入直接 panic。
// **仅供包内常量与测试使用**，禁止用于任何外部输入（账单文本、请求参数、汇率源返回值）。
func MustFromString(s string) Decimal {
	d, err := NewFromString(s)
	if err != nil {
		panic(errors.Wrapf(err, "decimal：常量文本非法").Error())
	}
	return d
}

// NewFromInt 从整数构造。
func NewFromInt(i int64) Decimal {
	return wrap(shop.NewFromInt(i))
}

// Zero 返回数值 0。
func Zero() Decimal {
	return Decimal{}
}

// One 返回数值 1。
// 对应 I-4「原币 = 本位币时汇率取 1」与 §三 修正③ 的短路判据，
// 收敛到一处以免各调用方各写一遍字面量。
func One() Decimal {
	return NewFromInt(1)
}

// checkRange 校验数值规模是否在本包支持范围内。
func (d Decimal) checkRange() error {
	// 小数位数 = -指数；指数为正（如 1e3）表示值是 10 的若干次幂倍，不构成小数位
	if scale := -d.v.Exponent(); scale > MaxScale {
		return errors.Wrapf(ErrOverflow, "小数位数 %d 超过上限 %d", scale, MaxScale)
	}
	if n := d.v.NumDigits(); n > MaxDigits {
		return errors.Wrapf(ErrOverflow, "有效数字 %d 位超过上限 %d", n, MaxDigits)
	}
	return nil
}

// Add 返回 d + d2，精确无舍入。
// 结果指数取两者较小值、不放大，因此不存在溢出分支，无需返回 error。
func (d Decimal) Add(d2 Decimal) Decimal {
	return wrap(d.v.Add(d2.v))
}

// Sub 返回 d - d2，精确无舍入。不存在溢出分支，理由同 Add。
func (d Decimal) Sub(d2 Decimal) Decimal {
	return wrap(d.v.Sub(d2.v))
}

// Neg 返回 -d。
func (d Decimal) Neg() Decimal {
	return wrap(d.v.Neg())
}

// Abs 返回 |d|。
func (d Decimal) Abs() Decimal {
	return wrap(d.v.Abs())
}

// Mul 返回 d × d2，结果**精确、不预舍入**。
//
// 不预舍入是不变式 1a 的要求：折算全程只在末端按本位币小数位舍一次
// （§九 不变式 1a 行：汇率先规整 8 位 → 乘 → 按本位币小数位四舍五入），
// 中途多舍一次就会引入二次舍入误差，且与 8.2 的均分基准对不齐。
//
// 返回 error 而非 panic：底层 Mul 在两个指数之和溢出 int32 时**直接 panic**
// （其源码注释称「better to panic than give incorrect results」），
// 而本包位于 L0、被全仓库依赖，对外零 panic 是硬要求，故先自行判定再转错误。
func (d Decimal) Mul(d2 Decimal) (Decimal, error) {
	// 与底层同一口径：它把两个 int32 指数以 int64 相加后判越界
	expSum := int64(d.v.Exponent()) + int64(d2.v.Exponent())
	if expSum > math.MaxInt32 || expSum < math.MinInt32 {
		return Decimal{}, errors.Wrapf(ErrOverflow, "乘法指数 %d 越界", expSum)
	}
	r := wrap(d.v.Mul(d2.v))
	if err := r.checkRange(); err != nil {
		return Decimal{}, errors.Wrap(err, "乘法结果")
	}
	return r, nil
}

// DivRound 返回 d ÷ divisor，商按 scale 位四舍五入（半值远离零，负数对称）。
func (d Decimal) DivRound(divisor Decimal, scale int32) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	if divisor.IsZero() {
		return Decimal{}, errors.WithStack(ErrDivZero)
	}
	return wrap(d.v.DivRound(divisor.v, scale)), nil
}

// DivTruncate 返回 d ÷ divisor，商按 scale 位**向零截断**（负数对称）。
//
// 与 DivRound 并存是给 8.2 摊分留的选择位：「按币种小数位数均分、尾差归末月」
// 中前 N−1 月那个值用截断还是四舍五入，终版 8.2 未写死，由 `rule/amortize`
// 在阶段 3 定案（见本轮 answer §四 待确认 2）。本包不代为决定。
//
// 不用底层的 Div：其精度取自包级全局变量 DivisionPrecision，不受本包控制。
func (d Decimal) DivTruncate(divisor Decimal, scale int32) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	if divisor.IsZero() {
		return Decimal{}, errors.WithStack(ErrDivZero)
	}
	// QuoRem 的商即向零截断的结果，且不读 DivisionPrecision
	q, _ := d.v.QuoRem(divisor.v, scale)
	return wrap(q), nil
}

// Cmp 比较大小：d < d2 返回 -1、相等返回 0、d > d2 返回 1。
// 比较的是**数值**，忽略标度差异（`1.50` 与 `1.5` 视为相等）。
func (d Decimal) Cmp(d2 Decimal) int {
	return d.v.Cmp(d2.v)
}

// Equal 判断数值是否相等，忽略标度差异。
//
// 忽略标度是必需的：同一个值在库里是 2 位定点、在内存里可能是 1 位，
// 若按标度区分，I-7「`折算本位币币种 = 当前本位币` 的行跳过」这类幂等判断会误判，
// 已收敛的行会被反复重算。
func (d Decimal) Equal(d2 Decimal) bool {
	return d.v.Equal(d2.v)
}

// Sign 返回符号：负数 -1、零 0、正数 1。
func (d Decimal) Sign() int {
	return d.v.Sign()
}

// IsZero 是否为零。
func (d Decimal) IsZero() bool {
	return d.v.Sign() == 0
}

// IsNegative 是否为负。
// 负金额表示退款或冲正（终版前提 2「负金额表示退款/冲正，作抵扣而非收入」），是合法值，
// 本包不做任何符号限制。
func (d Decimal) IsNegative() bool {
	return d.v.Sign() < 0
}

// Scale 返回当前值的小数位数。指数为正（如 `1e3`）时按 0 位计。
func (d Decimal) Scale() int32 {
	if scale := -d.v.Exponent(); scale > 0 {
		return scale
	}
	return 0
}

// String 返回最简十进制文本，去掉小数尾部多余的零。
// 供日志与 8.4 审计摘要（「导入 37 笔，合计 1,204.50 CNY」）使用；
// 落库与对外契约一律走定点文本，见 FixedString。
func (d Decimal) String() string {
	return d.v.String()
}

// MarshalJSON 以 JSON **字符串**（带引号）序列化。
//
// 必须是字符串而非 JSON 数字：T34② 定案前后端分离，`dto` 是 JSON 契约，
// 而 JS 的 number 是双精度浮点，8 位汇率与大额金额都会失真——
// 那等于在出口处把约束 3 破掉。
//
// 自行实现而不复用底层库：其形态取自包级全局变量 MarshalJSONWithoutQuotes，
// 任何依赖方改写它都会静默改变本仓库的对外契约。
func (d Decimal) MarshalJSON() ([]byte, error) {
	var builder strings.Builder
	builder.WriteByte('"')
	builder.WriteString(d.v.String())
	builder.WriteByte('"')
	return []byte(builder.String()), nil
}

// UnmarshalJSON 从 JSON 反序列化，接受带引号的字符串与裸数字两种形态。
//
// 裸数字也按**文本**解析、不经浮点中转，因此客户端即使发来 `0.1` 这种数字字面量
// 也不会变成 0.1000000000000000055…（约束 3 在入口侧的落法）。
// 遇 JSON null 时保持接收者不变，遵循标准库惯例。
func (d *Decimal) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}
	// 去掉 JSON 字符串的引号；裸数字形态无引号，原样进 NewFromString
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	parsed, err := NewFromString(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
