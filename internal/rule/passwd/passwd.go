// Package passwd 承载 jotcash 的密码策略规则（L2 规则层）。
//
// # 两件承载，恰好对应两条功能
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L2 表
// rule/passwd 行，本包恰承载两件事：
//
//	Check     A-8 策略校验（长度 ≥ 10、字母/数字/符号至少两类）
//	Generate  符合策略的随机初始密码生成（A-2），采样自 base/pwdhash 的安全随机源
//
// 依赖列只有 base/pwdhash 一项（外加普遍依赖的 base/errs）。承载末句是本包
// 最硬的一条口径：「**明文只在返回值中出现，本包不落盘、不打日志**」。
//
// # 为什么本包一行日志都不打
//
// 这是本包与 base/pwdhash 最明显的差别——后者打日志（只记长度与版本，有
// TestNoPasswordInLog 兜底），本包**一行不打**，连 logrus 都不 import。
// 理由不是取向而是实情：本包每个错误分支的**唯一入参就是密码明文本身**，
// 能写进日志的上下文只有「这个明文的哪一项不合规」。一旦允许打日志，
// 「顺手把明文带上便于排查」这件事在语法上就毫无阻力，而分层 §九 红线行只给
// 密码明文留了三个出口，日志那一个已经被 base/log 的 A-3 打印占满。
// 不 import logrus 是把这件事变成编译期约束——有 TestNoLoggingDependency
// 扫描 import 图钉住。
//
// 同理本包**没有任何包级可变状态**：明文只以入参形式出现在 Check 的调用栈内，
// 只以返回值形式离开 Generate，中途不进任何包级变量、不进错误的占位参数
// （见「占位参数只放计数」）。
//
// # 长度按 rune 计，且拒绝非法 UTF-8
//
// 「长度 ≥ 10」按 **rune** 计而非 byte：一个 4 个汉字的口令是 12 字节但只有
// 4 个字符，按 byte 计会判它「够 10 位」，与用户看到的东西直接相反。按 rune
// 计也与 base/pwdhash.RandString 的「返回串长度按 rune 计恰为 n」同口径。
//
// 但 rune 计数在**非法 UTF-8** 输入上不可靠，故本包把它单独判为一档错误。
// 这不是洁癖，是实测出来的一种「写得进去、再也登不进来」的形态：
//
//	"aaaaaaaaa\xff"  →  RuneCountInString 得 10（每个非法字节各算一个 U+FFFD），
//	                    看起来满足长度；末字节按 IsSymbol 归入符号类，
//	                    于是「两类」也满足，A-8 判它合规；
//	                 →  经 base/pwdhash.Hash 落库；
//	                 →  下次登录时前端经 JSON 回传，json.Marshal 会把非法字节
//	                    替换成真正的 U+FFFD（实测 10 字节变 12 字节、往返不相等），
//	                    Verify 拿到的已是另一个字符串，永远校验不通过。
//
// 结果是该账户被永久锁死，且 A-3 对 admin 明写「不设计恢复路径」。与 enum 那一轮
// 修掉的「写出侧不校验、读入侧校验」属同一类缺陷：错误必须在**造成它的那一次
// 请求**里失败，不能留到以后。
//
// 注意本包只拒绝**非法编码**，不拒绝合法的多字节字符：用户真的输入 U+FFFD
// 字面量（合法 UTF-8）是允许的，它与非法字节的区别在解码长度（实测 size 3 vs 1）。
//
// # 三类判据，以及「不计类别」不等于「不许出现」
//
// A-8 的三类按 Unicode 属性判定，判据与实测归类如下：
//
//	字母  unicode.IsLetter              a Z 汉 五 ｱ 全部为真
//	数字  unicode.IsDigit               ASCII 数字与 ٥（阿拉伯-印度数字）为真
//	符号  unicode.IsPunct 或 IsSymbol   ! _ - 为 Punct；$ ^ + 🙂 为 Symbol
//
// 三类之外的字符（空白、控制字符、组合音标 Mn、½ Ⅷ 这类 No/Nl 数字）
// **不计入任何类别，但也不被禁止**。这两句必须同时成立，各自都有反例兜着：
//
//   - 若把空白算作符号，"aaaaaaaaa "（9 字母 + 1 空格）就成了「两类」，
//     而它实质是个单类别弱口令（实测在本包判据下类别数为 1，正确拒绝）；
//   - 若因为含空白就拒绝，"hello world1" 这类 passphrase 会被拒——而它字母 10 个、
//     数字 1 个，实实在在是两类，A-8 没有任何理由拒绝它（实测通过）。
//
// # 生成侧：定额 + 洗牌，不用「生成后重试」
//
// Generate 用「按类别定额采样 → 密码学洗牌」保证结果必然合规，而**不**用
// 「随机生成一个 → Check → 不合规就重来」：后者在字符集或定额配错时不会报错，
// 只会变成一个跑很久甚至不终止的循环，故障形态是「A-1 系统初始化卡住」，
// 比直接失败难查得多。定额法的合规性是结构性的，无需依赖循环收敛。
//
// 洗牌是必需的，不能只把三段类别块拼起来：那样数字与符号恒定落在固定区间，
// 口令形态可预测（有 TestGenerateShuffled 用位置分布拦这条）。洗牌用
// Fisher–Yates，随机下标取自 base/pwdhash.RandIndex（内部是 crypto/rand.Int
// 的拒绝采样，无取模偏置）。
//
// 生成完仍再过一遍 Check 并在不通过时报错——定额已保证合规，这条断言是为了让
// 「有人改了字符集常量或定额」这类改动在运行期第一次调用就暴露，而不是产出一个
// 不合本系统策略的密码。
//
// # 字符集去易混字符，是 A-3 的直接要求
//
// 生成用的字符集去掉了 0/O/o、1/l/I（沿用 base/pwdhash 既有用例的同一套字符集），
// 符号只取 !@#$%^&* 八个。理由来自 A-3 的交付方式而非美观：admin 初始密码要
// **由人从日志里抄读**，其余场景是管理员在界面上一次性展示后**线下口头或手写交付**；
// 抄错一位与日志丢失的后果相同——A-3 明写「唯一出路是重新部署服务从头初始化」。
//
// 符号集也刻意避开了反斜杠、双引号、反引号、尖括号与竖线：它们会被 JSON 与
// logrus 的 %q 转义，人从日志里抄读时极易多抄或漏抄一个反斜杠。作对照，
// sethvargo/go-password 的默认符号集实测恰好含 \ " ` < > | $ 七个。
//
// # 为什么不引第三方库
//
// prompt 要求优先采用成熟库，故实测评估了两类候选，结论是**都不采用**，
// 详细依据与实测数据见本轮 answer 的「第三方库评估」一节，要点：
//
//   - sethvargo/go-password（688★、MIT、v0.4.0）只解决生成、不解决 A-8 校验，
//     且它的随机源是 io.Reader，而 base/pwdhash 只出 RandBytes/RandIndex/RandString，
//     接上去要自建一个 Reader 适配器（实测生成 5 个 12 位口令触发 153 次 Read，
//     即 153 次 RandBytes 调用）。承载写死「采样自 base/pwdhash 的安全随机源」，
//     为了用库反而要在本包写一层适配器 + 一套自定义字符集 + 一层错误档位映射，
//     比直接用 RandString 长且多一个依赖；
//   - wagslane/go-password-validator（593★，最近推送 2022-09）按**熵**判定强度，
//     与 A-8 的「长度 + 类别数」是两套不兼容的判据。照用等于改业务规则。
//
// 对照参考：base/pwdhash 用了官方 golang.org/x/crypto/argon2，currency 把
// bojanz/currency 降为 test-only 依赖——**该用则用**。本包属确实无可用之处的一类，
// 两个函数合计的算法内容只是「按 Unicode 属性数三类」与「定额采样 + 洗牌」。
//
// # 错误是 base/errs 的分档错误，不是哨兵
//
// 这与同层 base/decimal、currency、enum 用哨兵错误的做法相反，理由是**档位在
// 本包是确定的**：A-8 校验失败恒为「用户输入错误」档（base/errs 的
// KindInvalidInput 注释里就把「A-8 密码不符策略」列为典型场景），不像 enum 的
// 「未知码值」会因调用方不同而分属 400 与 500 两档。既然档位唯一，就该在此定死，
// 免得每个调用方各写一次映射。
//
// 文案键由本包声明（errs 包注释：业务文案键归各业务包，命名 <包>.<场景>），
// 四个键见 Key 常量组。**i18n 的语言文件必须包含这四个键**——这是本包对
// i18n 那一轮的硬性契约，与 base/errs 的五个兜底键同类。
//
// # 占位参数只放计数
//
// 错误会同时流向日志与前端两条路径（base/errs 包注释），故本包的
// errs.Params 里**只放长度与类别的计数**，绝不放明文、也不放明文的任何片段。
// 已实测确认 errs.Error() 会把 Params 逐项打出来，放进去就是两处同时泄露。
// 有 TestNoPasswordInError 逐个错误分支断言这一点。
//
// # 依赖边界
//
// 本包属 L2「纯逻辑、无外部 IO、可脱库单测」：只 import base/pwdhash 与
// base/errs 两个仓库内包（外加标准库 context / unicode / unicode/utf8）。
// 刻意**不**依赖 entity（本包处理的是裸字符串口令，不碰 User 实体——密码哈希与盐
// 的写入归 service/account）、不依赖 currency / enum / calendar / decimal
// （与金额、日期、枚举无关）、不依赖 base/log（见上文「一行日志都不打」）。
// 由 TestDependencyBoundary 以 AST 扫描双向锁死：越界报错，依赖列里的包未被用到
// 也报错。
//
// # 已登记的残留：策略无长度上限
//
// A-8 与分层承载都只规定下限，故本包**不设**长度上限——加上限等于新增一条业务
// 规则，会拒掉一个按 A-8 合法的口令。已知代价：超长口令会让 Argon2id 的单次
// 开销随长度线性上升（base/pwdhash 的参数是 64 MiB / 3 轮），登录接口存在被
// 超长口令放大 CPU 的面。缓解落在别处而非本包：A-8 的「连续失败 5 次锁定 10
// 分钟」（service/auth）与接入层的请求体大小限制。是否需要显式上限已作为待确认项
// 记入本轮 answer，需产品口径确认后再动。
package passwd

import (
	"unicode"
	"unicode/utf8"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// A-8 的两个阈值。按约定 8「业务阈值一律写死在对应包」，它们不进 base/config——
// 终版 A-8 把密码策略定义为业务规则，留一个可配置入口等于留一个能把长度下限
// 调到 1 的入口。
//
// 两者导出是因为确有包外消费方：service/account 在 A-2/A-7/A-8 三处需要引用
// 同一口径，L6 的提示文案也要按同一数值渲染（它们经错误的占位参数下发，
// 见 Check 的 Params）。
const (
	// MinLength 是密码最小长度，取自终版 A-8「长度 ≥ 10」。**按 rune 计**，
	// 不按字节——理由见包注释「长度按 rune 计」。
	MinLength = 10

	// MinCategories 是密码最少需要覆盖的字符类别数，取自终版 A-8
	// 「字母/数字/符号至少含两类」。三类的判据见包注释。
	MinCategories = 2
)

// 本包声明的四个文案键。命名遵 base/errs 包注释的 <包>.<场景> 约定。
//
// **i18n 的语言文件（internal/i18n/locales/）必须包含这四个键**，否则 A-8 的
// 校验失败渲染不出文案。这是本包对 i18n 那一轮的硬性契约。
//
// 四档而非一档笼统的「密码不合规」，是为了让上层给出不同的文案：「请输入密码」
// 与「至少 10 位」是两句不同的话，而「编码非法」根本不该让用户去猜长度。
// 这与 currency 把空值、未知、非候选分三个哨兵是同一取向。
const (
	// KeyEmpty 表示密码为空。占位参数：无。
	KeyEmpty = "passwd.empty"

	// KeyInvalidEncoding 表示密码不是合法 UTF-8。占位参数：无。
	//
	// 单独成档的理由见包注释——非法编码下 rune 计数已不可靠，且该口令一旦落库
	// 会导致账户永久无法登录。
	KeyInvalidEncoding = "passwd.invalid_encoding"

	// KeyTooShort 表示长度不足。占位参数：Min（下限）、Actual（实际 rune 数）。
	KeyTooShort = "passwd.too_short"

	// KeyTooFewCategories 表示字符类别数不足。
	// 占位参数：Min（下限）、Actual（实际类别数）。
	KeyTooFewCategories = "passwd.too_few_categories"
)

// Check 校验密码是否符合 A-8 策略：长度 ≥ MinLength 且字母/数字/符号至少含
// MinCategories 类。
//
// 返回值语义：
//   - nil                成功；
//   - 非 nil             base/errs 的 KindInvalidInput 档错误，带本包的文案键与
//     占位参数，由 api/http/middleware 经 i18n 渲染给用户。
//
// 返回类型是 error 而非 *errs.Error：这是刻意的，避免「类型化的 nil 指针」——
// 若声明为 *errs.Error，成功路径上 `var err error = Check(p)` 会得到一个接口值
// 非 nil、内部指针为 nil 的 error，使调用方的 `if err != nil` 恒为真
// （base/errs 的 From 注释记录了这个陷阱的实测后果）。
//
// 四项检查按「空 → 编码 → 长度 → 类别」顺序，遇到第一项不满足即返回，不汇总。
// 编码检查必须在长度之前：非法 UTF-8 下每个坏字节各算一个 rune，长度判定的
// 结果没有意义，先报长度会把真正的问题盖掉。
//
// 本函数**不区分**「新设密码」与「登录时输入的密码」——A-8 是设密时的策略，
// 登录校验走 base/pwdhash.Verify，不该再过本函数（否则策略一旦收紧，
// 存量用户会在登录环节被自己的旧密码挡在外面）。调用方仅 service/account 的
// A-2/A-7/A-8 三条设密链路。
func Check(password string) error {
	if password == "" {
		return errs.NewInvalidInput(KeyEmpty, nil)
	}

	//必须先判编码：非法 UTF-8 会让下面的 rune 计数与类别计数都失去意义，
	//而这种口令一旦通过校验落库，账户将永久无法登录（见包注释的实测形态）
	if !utf8.ValidString(password) {
		return errs.NewInvalidInput(KeyInvalidEncoding, nil)
	}

	length := utf8.RuneCountInString(password)
	if length < MinLength {
		//占位参数只放计数，绝不放明文或明文片段——错误同时流向日志与前端两条路径
		return errs.NewInvalidInput(KeyTooShort, errs.Params{"Min": MinLength, "Actual": length})
	}

	categories := countCategories(password)
	if categories < MinCategories {
		return errs.NewInvalidInput(KeyTooFewCategories, errs.Params{"Min": MinCategories, "Actual": categories})
	}

	return nil
}

// countCategories 数出密码覆盖了字母/数字/符号中的几类，取值恒在 [0, 3]。
//
// 三类的判据见包注释。三类之外的字符（空白、控制字符、组合音标、½ 这类 No 数字）
// 既不计入任何类别，也不影响其它字符的计数——「不计类别」与「禁止出现」是两件事，
// 混为一谈会把 "hello world1" 这类合法 passphrase 拒掉。
//
// 不导出：本包只应有 Check 一个校验入口。导出它等于开第二个入口，
// 调用方可以绕过长度检查只看类别数（准则 1「一条不变式只有一处实现」的取向）。
func countCategories(password string) int {
	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			hasSymbol = true
		}
		//三类齐了即可提前退出：后面的字符不可能再增加类别数
		if hasLetter && hasDigit && hasSymbol {
			return 3
		}
	}

	var count int
	for _, has := range []bool{hasLetter, hasDigit, hasSymbol} {
		if has {
			count++
		}
	}
	return count
}
