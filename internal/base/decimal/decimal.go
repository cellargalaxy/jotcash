// Package decimal 是 jotcash 全仓库唯一的定点小数来源（L0 基础层）。
//
// # 为什么需要本包
//
// 「记账系统功能与模型」把三处精度口径定死：原币金额按币种小数位数、本位币金额
// 按本位币小数位数计算并以 2 位定点存储、折算汇率 8 位四舍五入。若用 float64
// 承载金额，这三处会同时静默出错且不报错：
//   - 8.1 折算（不变式 1a）：0.1 + 0.2 在二进制浮点下不等于 0.3，逐笔折算后
//     的合计与逐笔求和的合计会出现分位偏差；
//   - 8.2 摊分（不变式 1b）：均分份额乘回月数不再等于原金额，尾差无从界定；
//   - 8.3 查重（四要素含支出金额）：两笔本应相等的金额比不出相等，重复漏判。
//
// 分层设计把这三处列为「全系统仅有的算错就静默出错的逻辑」。因此本包是那句
// 「金额一律不用浮点」的唯一实现处：全仓库只有本包 import 第三方定点小数库，
// 且本包**不提供任何 float 出入口**——「不用浮点」由此从口头约定变成编译期不可达。
//
// # 承载与不承载
//
// 本包只提供「定点小数类型 + round(值, 位数, 舍入方式)」这一层纯算术能力，
// 按约定 5「base/ 是纯技术层，带业务语义的类型不得放入」，下列各项均不在本包：
//
//   - 币种小数位数的**取值**归 currency（L1.0）。本包只接收位数参数，不知道任何币种。
//   - 折算乘法与汇率规整的**时机**归 rule/convert（不变式 1a 的唯一实现处）。
//   - 均分份额与**尾差归属**归 rule/amortize（不变式 1b 的唯一实现处）。本包只给商与余。
//   - 落库**列形态**归 store/sqlite。本包只提供无损性判定与两种输出形态。
//
// # 三处精度口径的落点
//
//	原币金额    Round(币种小数位数, RoundHalfUp)      调用方 rule/amortize、service/intake
//	本位币金额  算 Round(本位币小数位数, RoundHalfUp) 调用方 rule/convert
//	            存 BaseAmountStoreScale = 2 位定点    调用方 store/sqlite
//	折算汇率    RoundRate()，即 RateScale = 8 位      调用方 rule/convert
//
// 「算按本位币小数位数、存按 2 位」两者不冲突：分层设计已把本位币限定为小数位数
// ≤ 2 的币种，故 0/1/2 位的舍入结果必然满足 FitsScale(2)，即按本位币位数算出的
// 金额一定能被 2 位定点列无损容纳。FitsScale 是这条前提的可判定形式。
//
// # 为什么类型不可比较
//
// 第三方库的 Decimal 是 comparable，而 == 比的是内部表示不是数值：实测
// 1.50 == 1.5 得 false，零值 == FromInt(0) 也得 false，且**编译通过、不报错**。
// 8.3 判重的四要素含支出金额，一旦写成 == 就静默漏判重复；含金额字段的实体
// 结构体整体 == 同理。
//
// 按准则 6「约束优先用结构表达」，本包的 Decimal 内嵌一个零宽不可比较字段，
// 使 ==、含金额字段的结构体整体比较、map[Decimal]T 全部变成**编译错误**，
// 数值相等只能走 Equal / Cmp。手法取自标准库 hash/maphash.Hash 的
// 「_ [0]func() // not comparable」，零内存开销（实测包装前后均 16 字节）。
//
// 代价是必须自实现 JSON 编解码——否则未导出字段会让默认序列化输出空对象「{}」，
// 金额静默变空值。已实现并由单测钉住。
//
// # 为什么不实现 driver.Valuer / sql.Scanner
//
// 同层 calendar.Date 实现了这对接口，本包**有意不实现**，差异源于二者形态数不同：
// Date 只有一种规范文本（定长 2006-01-02），而金额有两种目标位数——金额列 2 位、
// 汇率列 8 位。实现隐式 Value() 就必须在包内替 store 猜一个位数，猜错即静默改钱。
// 故改为要求 store/sqlite 显式调 Units(scale) 或 StringFixed(scale)。
//
// 副作用是把 Decimal 直接塞进 db.Exec 会以「unsupported type」当场失败——
// 响亮失败优于静默错值，与准则 6 同一取向。
//
// # 依赖与层内零依赖
//
// 本包处于 L0 且层内零依赖：不 import 仓库内任何其他包，特别是**不依赖 base/errs**。
// 因此错误一律为本包的哨兵错误，由上层用 errors.Is 判定后映射到 base/errs 的五档
// 与文案键（例如 parser 的 D-3 五类解析错误），本包不含任何面向用户的文案。
//
// 定点算术下沉给 github.com/shopspring/decimal（MIT、big.Int 系数 + int32 指数、
// 同作者 go_common 已依赖同一版本），本包不自行实现定点小数内核，只做语义收窄、
// 量级设界与危险 API 的隔离。
//
// # 被隔离掉的三个危险 API（均为实测结论，非推断）
//
//  1. **银行家舍入**不予暴露：它把 5.45 舍成 5.4，与已定案的「四舍五入」口径不符，
//     而两者签名一致，误用后差异只有一分钱，几乎不可能在评审中被发现。
//  2. **除法**不予暴露：第三方库的 Div 受一个包级可变量控制精度，任何包都能改掉它
//     从而远程篡改全仓库的金额除法结果。本包只提供必须显式给定位数的 QuoRem。
//  3. **取整数部分**不予暴露：它对超出 int64 的值静默返回截断后的错误整数
//     （实测 23 位数得到一个完全无关的值）。本包的 Units 改用 big.Int 显式判溢出。
package decimal

import (
	"errors"
	"fmt"
	"math/big"

	sdecimal "github.com/shopspring/decimal"
)

// 精度与量级口径。
//
// 按约定 8「业务阈值一律写死在对应包」，这些取值是业务规则而非部署差异，
// 故为编译期常量，不进 base/config。
const (
	// RateScale 是折算汇率的小数位数：8 位、四舍五入。
	//
	// 全仓库唯一的汇率精度口径，自动获取与用户手填两条入口共用同一个规整结果，
	// 避免出现「库里存 12 位、算的时候用 8 位」的偏差。
	RateScale = 8

	// BaseAmountStoreScale 是本位币金额的存储精度：2 位定点。
	//
	// 与「计算精度」是两件事：计算时按本位币自身的币种小数位数舍入（恒 ≤ 2 位），
	// 结果再以 2 位定点存储。因 0/1/2 位结果都能被 2 位无损容纳，该转换无损。
	BaseAmountStoreScale = 2

	// MaxScale 是入口允许的最大小数位数。
	//
	// 远高于汇率的 8 位与现存币种的最大小数位数（3 位，如 KWD），
	// 仅用于拦截「1e-2000」这类恶意或损坏的输入。
	MaxScale = 18

	// MaxIntDigits 是入口允许的最大整数位数。
	//
	// 取 16 位有确切算术理由：int64 上界约 9.22e18，2 位定点下 16 位整数
	// （最大 9999999999999999，乘 100 得约 1e18）可无损放入，而 17 位
	// （约 1e17，乘 100 得 1e19）必然溢出。故 16 位保证任何通过入口的金额
	// 都能无损落进 store 的「整数最小单位」列形态。
	//
	// 同时 16 位整数约合 1e16，远超个人记账场景中任何币种（含 JPY、IDR 这类
	// 面值大的币种）的真实金额，因此不会误拒真实数据。
	MaxIntDigits = 16
)

// 哨兵错误。
//
// 本包不依赖 base/errs（L0 层内零依赖），错误分类由这组哨兵表达，
// 上层用 errors.Is 判定后附加文案键与错误档位。与同层 calendar 同一取向。
var (
	// ErrSyntax 表示文本不是合法的十进制数字面值。
	//
	// 本包不做前后空格清洗、不认千分位分隔符：各银行账单版式不同，D-1 已明确
	// 不定义统一模板，格式清洗属 parser 的职责。若在此隐式清洗，格式判定就分散
	// 到了两层，同一份账单的容错行为将取决于走哪条路径。
	ErrSyntax = errors.New("decimal: 非法的数字文本")

	// ErrIntDigitsExceeded 表示整数位数超过 MaxIntDigits。
	ErrIntDigitsExceeded = errors.New("decimal: 整数位数超出上限")

	// ErrScaleExceeded 表示小数位数超过 MaxScale。
	ErrScaleExceeded = errors.New("decimal: 小数位数超出上限")

	// ErrScaleInvalid 表示目标小数位数非法（负数或大于 MaxScale）。
	//
	// 拒绝负位数：把 545 舍成 550 这种整数位舍入对金额没有任何消费方，
	// 出现即代表调用方算错了位数，应当暴露而非照做。
	ErrScaleInvalid = errors.New("decimal: 非法的小数位数")

	// ErrRoundMode 表示未知的舍入方式，含零值 RoundMode(0)。
	//
	// 零值不默许任何一种方式：忘传参数必须报错，不能静默按某种方式把钱舍掉。
	ErrRoundMode = errors.New("decimal: 未知的舍入方式")

	// ErrDivideByZero 表示除数或摊分份数为零。
	//
	// 第三方库遇除零直接 panic，本包改为返回错误：摊分月数来自用户输入（F-1），
	// 一次非法输入不应崩掉整个请求。
	//
	// 注意与 go_common/util 的 IntDivFloat 有意分歧——后者除零返回 0，
	// 那是浮点工具的容错取向；金额除法静默得 0 会让各月份额全为 0 且无人察觉。
	ErrDivideByZero = errors.New("decimal: 除数为零")

	// ErrPrecisionLoss 表示按目标位数输出会丢失有效数字，即无法无损容纳。
	//
	// 落库这一步绝不静默舍入：钱被悄悄改掉且无日志无报错，事后无从发现。
	// 需要舍入的调用方应先显式调 Round，让舍入这一动作在调用点可见。
	ErrPrecisionLoss = errors.New("decimal: 目标位数无法无损容纳该值")

	// ErrUnitsOverflow 表示换算为最小单位整数时超出 int64 表示范围。
	ErrUnitsOverflow = errors.New("decimal: 最小单位整数溢出")
)

// noCmp 是零宽且不可比较的占位类型，使内嵌它的结构体无法用 == 比较、也无法作 map 键。
// 手法取自标准库 hash/maphash.Hash 的「_ [0]func() // not comparable」。
type noCmp [0]func()

// Decimal 是定点小数，承载金额与汇率。不可变：所有方法都返回新值，不修改接收者。
//
// **不可比较**：== 会被编译器拒绝，数值相等一律走 Equal / Cmp，理由见包注释
// 「为什么类型不可比较」。
//
// 零值等价于 0，且全部方法都可直接在零值上调用——实体结构体以零值创建，
// 业务上「不适用」的场景以零值表达，上层无需先判空再取值。
type Decimal struct {
	_ noCmp
	v sdecimal.Decimal
}

// wrap 把第三方库的值收进本包类型，是包内唯一的转换入口。
// 收敛为一处便于将来更换底层实现时只改这一个函数。
func wrap(v sdecimal.Decimal) Decimal {
	return Decimal{v: v}
}

// Parse 解析十进制文本，是金额进入系统的主入口。
//
// 接受可选正负号、小数点与科学计数法，如「-123.45」「.5」「1.」「1e3」；
// 拒绝空串、含前后空格、含千分位分隔符、NaN 与 Inf 等一切非法文本。
//
// 同时按 MaxIntDigits / MaxScale 校验量级。这道校验不是冗余：parser 喂入的是
// 不可信的账单文本，而「1e1000000」这类值能被底层库在纳秒内解析成功，
// 其后任何一次舍入都会产出百万位数字、耗掉数十毫秒 CPU 与数 MB 内存
// （实测 64ms / 约 8MB），构成一条不需要任何权限的拒绝服务路径。
//
// 校验通过后把值**规范化**（剥掉尾随零）再返回。这一步同样是安全相关的：
// 量级校验看的是「有效位数」，于是「0.1 后接 12 万个 0」这类文本有效位数只有 1 位、
// 能合法通过校验，却在内部留下 12 万位的系数——此后每一次 Scale / FitsScale /
// Units / StringFixed 都要重新为这些零付一遍代价（实测单次约 0.4~1.7 秒）。
// 规范化把系数压回有效位数，让「通过校验」真正等价于「后续运算都在量级内」。
// 规范化只剥尾随零，数值与各类字符串输出均不变（已由用例锁死）。
func Parse(text string) (Decimal, error) {
	v, err := sdecimal.NewFromString(text)
	if err != nil {
		//保留原始文本便于排查，但不把第三方库的错误原文透出，避免上层依赖其措辞
		return Decimal{}, fmt.Errorf("%w: %q", ErrSyntax, text)
	}
	d := wrap(v)
	if err := d.checkMagnitude(); err != nil {
		return Decimal{}, err
	}
	return d.normalize(), nil
}

// MustParse 解析十进制文本，失败即 panic。
//
// 仅供测试与包内常量初始化使用，业务代码一律用 Parse——
// 运行期数据必须走错误分支，与同层 calendar.MustParseDate 同一取向。
func MustParse(text string) Decimal {
	d, err := Parse(text)
	if err != nil {
		panic(fmt.Sprintf("decimal: 解析失败, text=%q: %v", text, err))
	}
	return d
}

// FromInt 由整数构造，供「摊分月数」这类整数参与金额运算时使用。
func FromInt(value int64) Decimal {
	return wrap(sdecimal.NewFromInt(value))
}

// FromUnits 由最小单位整数还原定点小数，scale 为小数位数。
//
// 与 Units 互为逆运算，供 store/sqlite 的「整数最小单位」列形态无损往返：
// FromUnits(10050, 2) 得 100.5、FromUnits(712345678, 8) 得 7.12345678。
//
// 同样在返回前规范化：scale 由列口径决定（金额固定 2 位），而 units 的末位可能是
// 零（10050 → 100.50），规范化后与 Parse("100.5") 得到完全一致的内部表示，
// 两条入口不会因来源不同而在后续行为上出现差异。
func FromUnits(units int64, scale int32) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	//以 big.Int 系数 + 负指数直接构造，全程不经过浮点
	d := wrap(sdecimal.NewFromBigInt(big.NewInt(units), -scale))
	if err := d.checkMagnitude(); err != nil {
		return Decimal{}, err
	}
	return d.normalize(), nil
}

// Sum 求和。空入参返回零值 0，免去调用方为「空列表」单独分支。
//
// 不做量级校验：F-6 筛选态合计与 J 域月度聚合都是多笔累加，
// 给聚合结果设上界会误拒合法统计（入口已保证每一笔都在量级内）。
func Sum(list ...Decimal) Decimal {
	var sum Decimal
	for i := range list {
		sum = sum.Add(list[i])
	}
	return sum
}

// checkMagnitude 校验整数位数与小数位数是否落在入口允许的量级内。
func (d Decimal) checkMagnitude() error {
	if scale := d.Scale(); scale > MaxScale {
		return fmt.Errorf("%w: %d 位, 上限 %d 位", ErrScaleExceeded, scale, MaxScale)
	}
	if intDigits := d.intDigits(); intDigits > MaxIntDigits {
		return fmt.Errorf("%w: %d 位, 上限 %d 位", ErrIntDigitsExceeded, intDigits, MaxIntDigits)
	}
	return nil
}

// normalize 把底层表示压到有效位数，剥掉尾随零。
//
// 纯粹的表示层整理：数值不变，String / StringFixed / Units 的输出也都不变，
// 变的只是内部系数的长度。存在的意义是防住「有效位数很小、底层系数极长」这种
// 输入——它能通过量级校验，却让后续每一次运算都要重新处理一遍那些无意义的零。
// 入口处整理一次，后面所有运算就都在量级内进行。
//
// 只在 Parse / FromUnits 这类**外部输入**入口调用。运算结果不必再规范化：
// 参与运算的值都已规范化，其结果的系数长度天然有界。
func (d Decimal) normalize() Decimal {
	scale := d.Scale()
	if -d.v.Exponent() == scale {
		//已是有效位数，无零可剥，直接复用原值免去一次大整数运算
		return d
	}
	return wrap(d.v.Round(scale))
}

// intDigits 返回整数部分的位数；0 与纯小数一律记为 1 位。
func (d Decimal) intDigits() int32 {
	if d.IsZero() {
		return 1
	}
	//系数总位数加指数即整数部分位数：1.5 存为系数 15、指数 -1，2 + (-1) = 1 位；
	//1e3 存为系数 1、指数 3，1 + 3 = 4 位（即 1000）。两种方向同一个式子。
	n := int32(d.v.NumDigits()) + d.v.Exponent()
	if n < 1 {
		return 1
	}
	return n
}

// Add 返回 d + other。
func (d Decimal) Add(other Decimal) Decimal {
	return wrap(d.v.Add(other.v))
}

// Sub 返回 d - other。
func (d Decimal) Sub(other Decimal) Decimal {
	return wrap(d.v.Sub(other.v))
}

// Mul 返回 d * other，保留全部有效位、不做任何舍入。
//
// 刻意不在此舍入：「原币金额 × 汇率」的舍入基准是本位币的币种小数位数，
// 只有 rule/convert 知道该取几位。在此按某个默认位数舍入等于把不变式 1a
// 的一部分实现挪进基础层。
func (d Decimal) Mul(other Decimal) Decimal {
	return wrap(d.v.Mul(other.v))
}

// Neg 返回 -d。
func (d Decimal) Neg() Decimal {
	return wrap(d.v.Neg())
}

// Abs 返回 d 的绝对值。
func (d Decimal) Abs() Decimal {
	return wrap(d.v.Abs())
}

// QuoRem 按 scale 位做带余除法，返回商与余数，恒满足「商 × divisor + 余 = d」。
//
// 商向零截断，余数与被除数同号，因此负金额（退款、冲正）与正金额完全对称。
// 该恒等式是 8.2 摊分的算术底座：各月取商、尾差取余，两者相加必然等于原金额，
// 不会因舍入而多出或丢失分位。
//
// 只给商与余，**不决定尾差归属**：「尾差固定计入末月」是业务规则，
// 归 rule/amortize（不变式 1b 的唯一实现处），在此实现会让该不变式有两处实现。
func (d Decimal) QuoRem(divisor Decimal, scale int32) (quotient, remainder Decimal, err error) {
	if err = checkScale(scale); err != nil {
		return Decimal{}, Decimal{}, err
	}
	//必须先判零：底层库遇除零是 panic，而摊分份数来自用户输入
	if divisor.IsZero() {
		return Decimal{}, Decimal{}, ErrDivideByZero
	}
	q, r := d.v.QuoRem(divisor.v, scale)
	return wrap(q), wrap(r), nil
}

// Equal 判断数值是否相等，与内部表示无关：1.50 与 1.5 相等，零值与 FromInt(0) 相等。
//
// 8.3 四要素判重中「支出金额」的相等判定必须走本方法。
func (d Decimal) Equal(other Decimal) bool {
	return d.v.Equal(other.v)
}

// Cmp 比较数值：d 小于 other 返回 -1，相等返回 0，大于返回 1。
func (d Decimal) Cmp(other Decimal) int {
	return d.v.Cmp(other.v)
}

// IsZero 报告是否为零。
func (d Decimal) IsZero() bool {
	return d.v.IsZero()
}

// IsNegative 报告是否为负数（零不算负数）。负金额表示退款或冲正。
func (d Decimal) IsNegative() bool {
	return d.v.IsNegative()
}

// IsPositive 报告是否为正数（零不算正数）。
func (d Decimal) IsPositive() bool {
	return d.v.IsPositive()
}

// Sign 返回符号：负数 -1、零 0、正数 1。
func (d Decimal) Sign() int {
	return d.v.Sign()
}
