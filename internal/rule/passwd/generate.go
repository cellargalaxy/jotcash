package passwd

import (
	"context"
	"strings"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/base/pwdhash"
)

// 生成用的字符集。去掉了 0/O/o、1/l/I 这些易混字符——沿用 base/pwdhash 既有
// 用例里的同一套字符集，理由见包注释「字符集去易混字符，是 A-3 的直接要求」。
//
// 符号集只取 !@#$%^&* 八个，刻意避开 \ " ` ' < > | 等会被 JSON 与 logrus 的 %q
// 转义的字符：A-3 的 admin 初始密码要由人从日志里抄读，转义后的反斜杠极易多抄漏抄。
//
// 不导出：这是生成侧的实现细节，与 Check 的判据无关（Check 按 Unicode 属性判，
// 接受任何合法字符，不限于此处这几个）。把它导出会招来「前端也照这套字符集
// 做一遍校验」的误用——那等于把判据实现两遍。
const (
	genLowerLetters = "abcdefghijkmnpqrstuvwxyz" //去掉 l、o
	genUpperLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ" //去掉 I、O
	genDigits       = "23456789"                 //去掉 0、1
	genSymbols      = "!@#$%^&*"
)

// 生成的初始密码规格。
//
// 长度取 16 而非 A-8 下限 10：初始密码是**系统生成**的，不需要用户记忆
// （A-2 强制首次登录改密，A-3 的 admin 初始密码由人抄读一次后即改），
// 因此长度成本几乎为零，而多出的 6 位显著抬高在线爆破成本。
//
// 定额 4 位数字 + 2 位符号 + 余下 10 位字母，三类齐备，是 A-8「至少两类」的
// 超集。故意让字母占多数：抄读与手输时字母的容错高于符号。
const (
	genLength      = 16
	genDigitCount  = 4
	genSymbolCount = 2
)

// KeyGenerateFailed 是生成初始密码失败的文案键，占位参数：无。
//
// 与 Check 的四个键分档不同，本键恒为 base/errs 的 KindInternal 档——生成失败
// 只可能源于系统随机源不可用（crypto/rand 失败）或本包自身的常量配错，
// 两者都不是用户能修的，也不该把细节回给用户。
//
// **i18n 的语言文件必须包含此键。**
const KeyGenerateFailed = "passwd.generate_failed"

// Generate 生成一个符合 A-8 策略的随机初始密码，用于 A-2 的初始密码下发与
// A-7 的管理员重置他人密码。
//
// 返回值语义：
//   - (明文, nil)   成功；
//   - ("", 非 nil)  base/errs 的 KindInternal 档错误，带 KeyGenerateFailed。
//
// **返回的明文是本包唯一的明文出口**，调用方（仅 service/account）必须遵分层
// §九 红线：立即交给 base/pwdhash.Hash 换成哈希 + 盐落库，明文本身只能走三个
// 出口之一（A-2/A-7 的一次性展示响应、A-3 的日志打印），不得写入 AuditLog
// 的变更内容、不得进 K-3 导出包、不得进任何持久化字段。本包对此无法强制，
// 只能在此写明。
//
// ctx 仅用于向 base/pwdhash 透传（其内部按约定 4 打日志需要 ctx 上的
// 请求标识）；本包自身不打日志，也不读 ctx 里的任何值。
//
// 算法是「按类别定额采样 → Fisher–Yates 洗牌 → 断言合规」，不是「随机生成后
// 重试到合规」，理由见包注释「定额 + 洗牌」：后者在常量配错时会退化成不终止的
// 循环，故障形态是 A-1 系统初始化卡住，比直接报错难查。
//
// 每次调用的结果都不同（概率意义上），因此本函数是本包唯一的非确定性来源，
// 也是整个 L2 规则层唯一的非确定性输入（分层 §六 L2 表 rule/passwd 行）。
// randString / randIndex 是 base/pwdhash 两个随机函数的**包级不可变别名**，
// 存在的唯一理由是让「随机源失败」这条路径可测。
//
// crypto/rand 在测试里不会失败，故 Generate 的五个错误分支若直接调 pwdhash
// 就永远跑不到，无法验证它们是否真的把档位设成 Internal、是否真的没把明文
// 带进错误里——那正是本包红线最需要盯的地方。
//
// 三点刻意的设计，避免这个测试便利变成生产隐患：
//   - 不导出，包外无法替换；
//   - 声明为 var 而非可注入的结构体字段或接口，生产路径上没有任何分支开销，
//     也没有「忘记初始化」的形态（值就是 pwdhash 的函数本身）；
//   - 唯一的替换点在 generate_test.go 的 withFailingRand，它用 defer 恢复原值，
//     且因此本包的测试**不可并行**（未标 t.Parallel()）。
//
// 这也是包注释「无任何包级可变状态」的唯一例外，且不涉及密码明文：
// 这两个变量存的是函数值，不存任何口令数据。
var (
	randString = pwdhash.RandString
	randIndex  = pwdhash.RandIndex
)

func Generate(ctx context.Context) (string, error) {
	//按类别定额采样：三段各自合规，拼起来必然满足 A-8「至少两类」
	letterCount := genLength - genDigitCount - genSymbolCount

	letters, err := randString(ctx, letterCount, genLowerLetters+genUpperLetters)
	if err != nil {
		//不 Wrap 明文相关的任何内容，只保留底层错误作为链上的因
		return "", errs.Wrap(err, errs.KindInternal, KeyGenerateFailed, nil)
	}
	digits, err := randString(ctx, genDigitCount, genDigits)
	if err != nil {
		return "", errs.Wrap(err, errs.KindInternal, KeyGenerateFailed, nil)
	}
	symbols, err := randString(ctx, genSymbolCount, genSymbols)
	if err != nil {
		return "", errs.Wrap(err, errs.KindInternal, KeyGenerateFailed, nil)
	}

	//必须洗牌：直接拼接会让数字与符号恒定落在固定区间，口令形态可预测
	password, err := shuffle(ctx, letters+digits+symbols)
	if err != nil {
		return "", errs.Wrap(err, errs.KindInternal, KeyGenerateFailed, nil)
	}

	//定额已保证合规，这条断言是为了让「有人改了字符集常量或定额」这类改动在
	//第一次调用时就暴露，而不是静默产出一个不合本系统策略的密码
	if err := Check(password); err != nil {
		return "", errs.Wrap(err, errs.KindInternal, KeyGenerateFailed, nil)
	}

	return password, nil
}

// shuffle 用 Fisher–Yates 洗牌打乱字符顺序，随机下标取自
// base/pwdhash.RandIndex（内部是 crypto/rand.Int 的拒绝采样，无取模偏置）。
//
// 上界必须是 i+1 而非 i：写成 i 就成了 Sattolo 算法，它只产生**单个轮换**，
// 于是「任何字符都不可能留在原位」，排列空间从 n! 缩到 (n-1)!，且分布有偏。
// 这个差别肉眼几乎看不出来（结果照样是乱的），有 TestShuffleFixedPointsExist
// 专门盯它——变异测试里把 i+1 改成 i 时，只有那条用例会失败。
//
// 按 rune 切片洗牌而非按字节：本包的字符集都是 ASCII，但按字节洗牌会在有人
// 往字符集里加入多字节字符时静默产出乱码，而按 rune 洗牌对两者都正确
// （有 TestShuffleMultibyte 直接以多字节输入盯这条）。
func shuffle(ctx context.Context, s string) (string, error) {
	runes := []rune(s)
	//从末位向前，第 i 位与 [0, i] 中随机一位交换。上界 i+1 使 j 可以等于 i，
	//即「留在原位」是合法结果——这正是 Fisher–Yates 与 Sattolo 的分界
	for i := len(runes) - 1; i > 0; i-- {
		j, err := randIndex(ctx, i+1)
		if err != nil {
			return "", err
		}
		runes[i], runes[j] = runes[j], runes[i]
	}

	//用 Builder 而非 string(runes)：两者等价，此处取 Builder 以预分配容量
	var builder strings.Builder
	builder.Grow(len(s))
	for _, r := range runes {
		builder.WriteRune(r)
	}
	return builder.String(), nil
}
