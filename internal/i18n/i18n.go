// Package i18n 承载 jotcash 的界面文案与错误文案取词（L3 边界层）。
//
// # 本包装什么
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md 的 L3 表 i18n 行，本包是
// **边界包**（实现无选型分歧、内联包内、无 L4 子包），承载四件事：
//
//	① 按「语言 + 文案键」取词并填充占位参数        Render / MustRender
//	② 可用语言列表与语言键合法性校验              Languages / ParseLang
//	③ 整包语言文件下发（T34④，供前端取词）        Messages / MessageKeys
//	④ 语言文件随包 embed（T37）                   locales/
//
// 外加一件由 api/http/middleware 的承载反向要求、必须落在本包的能力：
//
//	⑤ Accept-Language 回落                        Match
//
// middleware 的语言解析链是「ctxs.界面语言 → 请求头 Accept-Language → i18n 默认
// 语言」（分层 §六 L6 middleware 行 ①）。第二段需要按用户偏好列表在**已装载的
// 语言**中挑一个最合适的，这件事只有持有语言集合的本包做得了。
//
// # 界面文案与错误文案共用同一份语言文件
//
// 分层 §六 L3 i18n 行明写「K-4 界面文案与 base/errs 错误文案**共用同一份语言
// 文件**」。因此本包不区分「界面文案」与「错误文案」两个命名空间——base/errs
// 的五档兜底键、rule/passwd 的策略键、currency 的币种名称键，与将来 L6 的界面
// 文案键，全部平铺在同一个键空间里，按 <包>.<场景> 命名（base/errs 包注释）。
//
// 单一事实来源同样是 T34④ 的要求：前端不另持一份文案，由 handler/i18n 下发本
// 包的整包语言文件。否则 K-4「加语种只加配置」在前端侧失效。
//
// # 不含数据内容翻译
//
// 终版 K-4 明写多语言「**不含数据内容**（支出类型名称等）的翻译」。因此支出类型
// 名称、解析器类型名称（enum/file.go:112 已写明「不走 i18n」）、币种符号一律不
// 进语言文件。币种**名称**进语言文件（currency.NameKey()），因为它是枚举项的
// 界面标签而非用户数据。
//
// # 取词是总函数，但同时返回失败原因
//
// middleware 拿到 base/errs 的三件套后必须产出一个字符串塞进 HTTP 响应体，对
// 「取词失败」无法做任何有意义的处置。故 Render 的**第一个返回值恒非空**：缺键
// 或渲染失败时回落到文案键本身，调用方可直接使用。
//
// 但纯总函数会把问题咽掉，而本包**不 import base/log、一行日志都不打**（见下文
// 依赖边界），失败便无处可记。故 Render 同时返回 error：发生回落时非 nil，供
// middleware 打日志、供单测断言。两个消费方各取所需，「静默」与「无处可记」两个
// 问题因此同时解决。
//
// # 语言文件的错误在启动时就炸，不留到运行期
//
// Render 的回落是最后一道保险，不是常态。真正的防线在 init：语言文件的非法 JSON、
// 坏模板、tag 不一致、语言文件为空一律 panic 于包初始化——与 currency.init 对
// 枚举表自检失败的处置一致（「文件本身写错了」属编码错误，让它在进程启动时当场
// 炸掉，远好过带着一份坏文案跑到某次请求才出错）。
//
// 已实测：go-i18n **不在解析期校验模板语法**，`{{.Unclosed` 这类坏模板要到取词
// 时才报错。故本包在装载时**主动预编译每一条文案**，把坏模板从「某个用户某次
// 请求时才暴露」提前到启动失败。
//
// 「37 个业务键是否齐备」不在生产代码里校验——键的声明方分散在 base/errs(L0)、
// rule/passwd(L2)、currency(L1.0)，全部 import 会让本包耦合到每个声明键的业务包，
// 且 rule/passwd 在 L2、本包在 L3，import 它并不成环但毫无必要。该校验落在
// consumer_check_test.go：测试文件 import 三个包、拿其导出的键常量逐一断言语言
// 文件覆盖。这是 currency 已用过的手法（生产代码零仓库内依赖，测试文件 import
// base/decimal 做消费方走查）。
//
// # 第三方库：采用 nicksnyder/go-i18n，但包一层挡住四个陷阱
//
// 采用它的决定性理由是 K-4 本身：K-4 要求「新增语种只需增加配置、**无需改动
// 代码**」。中文无复数形态，本轮手写取词完全够用；但英文有复数，一旦加入英文，
// 「1 item / 2 items」在手写实现下必须回头改 Go 代码，K-4 当场失效。该库内置
// CLDR 200+ 语言的复数规则，这正是 K-4 唯一真正需要第三方库的地方。
//
// 实测查出四个陷阱，其中三个静默出错，全部由本包挡住（各有回归用例）：
//
//   - **占位参数缺失不报错**：默认 missingkey=default 会渲染出 "<no value>" 且
//     err 为 nil。本包一律以 missingkey=error 覆盖。
//   - **默认 tag 与文件 tag 不一致会让回落全盘失效**：NewBundle(SimplifiedChinese)
//     配 zh-CN.json 会产生两个 tag，其中 zh-Hans 一条文案都没有，于是
//     Accept-Language 为 en-US / zh-TW / 空串时全部取词失败——登录页与 A-8 失败
//     提示恰在这条路上。本包令默认 tag 与文件 tag 严格一致，并在装载时断言。
//   - **LanguageTags() 返回内部切片本身而非副本**（库缺陷）：对返回值排序会改写
//     bundle 内部 matcher 的顺序，双语言下取词完全错配（实测用户选中文得到英文、
//     选英文得到中文，err 恒为 nil）。本包从不对外暴露该切片，装载时一次性拷出。
//   - **MessageFile.Messages 顺序不稳定**（源自 map 遍历）：整包下发与键列表
//     一律自行排序。
//
// # 密码红线
//
// Params 是 map[string]any，语法上拦不住调用方把密码塞进来，而文案会同时流向
// 日志与前端两条路径。故定死：**任何 Params 中不得出现密码明文、密码哈希与盐**
// （§九 红线行）。本包无编译期兜底，与 base/errs.Params 同类处理，靠评审兜底。
//
// # 依赖边界
//
// 本包的依赖列是 `—`（分层 §六 L3 表），**零仓库内依赖**：不 import base/errs、
// base/log 或任何业务包。两个直接后果：
//
//   - **错误用哨兵而非 base/errs 的分档错误**。同一个「语言键非法」，经 dto 进来
//     是用户输入错误（前端提交了非法值），经 store 出来是系统错误（库里存了不
//     存在的语言键，属数据损坏）。本包无从判断被谁调用，一旦在此选定档位必有
//     一方是错的。故由调用方 errors.Is 判定哨兵后自行套档位与文案键——与
//     currency、enum、base/decimal、base/calendar 的一致取向。
//   - **一行日志都不打**，取词失败经返回值上报（见上文）。
//
// 哨兵一律用**标准库 errors.New** 构造，不用 github.com/pkg/errors：后者在包级
// 变量初始化时抓栈，会让哨兵永久携带一份指向 i18n.init 的栈，base/errs.Wrap 的
// hasStack 随即跳过补栈，middleware 打 %+v 时栈顶变成 i18n.init 而非真实出错点。
// 该约束已由 currency 那一轮写成可执行断言，本包同步照做。
//
// 因 T37 定 embed，本包**无外部参数、不需要 app 注入**（分层 §六 L3 表头）。
// 全部语言文件在包初始化时装载完成，此后只读、可并发使用。
//
// # 已登记的代价
//
// 加语种 = 增加一个 locales/<tag>.json + **重新构建二进制**，满足终版 K-4「无需
// 改动代码」的字面要求，但不满足「放个文件就生效」——终版未要求后者，T37 已显式
// 接受（分层 约定 4）。原因是 //go:embed 不能跨父目录，语言文件必须物理位于本包
// 目录之下。
package i18n

import "errors"

// 四个哨兵错误。调用方以 errors.Is 判定后自行套 base/errs 的档位与文案键——
// 本包不选档位，理由见包注释「依赖边界」。
//
// 一律用标准库 errors.New 构造：pkg/errors.New 会在 init 阶段抓栈，使
// base/errs 跳过补栈、middleware 的 %+v 栈顶变成 i18n.init（见包注释）。
var (
	// ErrEmptyLang 表示语言键为空串。
	//
	// 与 ErrUnknownLang 分开是因为两者对用户是两句不同的话：「请选择语言」与
	// 「不支持该语言」。service/preference 的 K-1 校验据此给不同文案键。
	ErrEmptyLang = errors.New("i18n: 语言键为空")

	// ErrMalformedLang 表示语言键不是合法的 BCP 47 语言标签。
	//
	// 与 ErrUnknownLang 分开是因为两者的成因不同：本档是**格式**就不对
	// （如 " zh-CN "、"!!bad!!"，实测 language.Parse 直接拒绝），
	// ErrUnknownLang 是格式合法但本仓库未提供该语种。
	ErrMalformedLang = errors.New("i18n: 语言键格式非法")

	// ErrUnknownLang 表示语言键格式合法，但不在已装载的语言集合内。
	//
	// K-1 的界面语言切换据此拒绝非候选值（分层 §六 L5.4 preference 行：
	// 「界面语言走 i18n 语言键校验」，两者都不接受任意值）。
	ErrUnknownLang = errors.New("i18n: 语言未提供")

	// ErrMissingKey 表示文案键在该语言下取不到词。
	//
	// 它是 Render 第二个返回值的取值之一，**不影响第一个返回值可用**——
	// 那里回落为文案键本身。正常情况下不应出现：语言文件的键齐备性由
	// consumer_check_test.go 逐键断言。
	ErrMissingKey = errors.New("i18n: 文案键未找到")

	// ErrRenderFailed 表示取到了词但填充占位参数失败。
	//
	// 最常见的成因是调用方少传了一个占位参数。本包用 missingkey=error 把它变成
	// 错误而非静默渲染出 "<no value>"（见包注释陷阱一）。同样不影响第一个返回值。
	ErrRenderFailed = errors.New("i18n: 文案渲染失败")
)

// Params 是文案键的具名占位参数，与 base/errs.Params 一一对应。
//
// 声明为**类型别名**而非定义类型，是为了让 middleware 能把 errs.ParamsOf(err)
// 的返回值直接传进来，不必写 i18n.Params(...) 做一次显式转换。本包不能 import
// base/errs（依赖列为 `—`），只能自己声明一个，但可以让它在类型上完全相容。
//
// 用具名 map 而非位置参数是 K-4 的要求（base/errs 包注释）：语序是语言属性，
// 位置参数在跨语种调序时必须回改 Go 代码。
//
// **禁止放入任何密码值**（明文、哈希、盐）——§九 红线行。文案会同时流向日志与
// 前端两条路径，放进来等于两处同时泄露。
type Params = map[string]any

// Lang 是一个已装载的语言。
//
// 与 currency.Code 同构：**字段私有**，包外无法构造，故不存在「语言键合法但
// 本仓库没有这个语种」的 Lang 值——取值域在编译期即由 locales/ 目录固定。
// 这与 enum 的 7 个 string 型枚举取向相反（那些必须在四条出入口逐一校验码值），
// 原因是本包的非法输入只有一个来源：外部传进来的字符串，而它恰好被 ParseLang 拦掉。
//
// 零值表示「未指定」，IsZero 为 true，Render 遇零值一律按默认语言处理——A-6
// 之前 ctxs 尚未写入，登录页与 A-8 登录失败提示正是走这条路（分层 §三 问题 5）。
//
// 可比较、可作 map 键（同一语言的 Lang 值共享同一个 *langEntry）。
type Lang struct {
	entry *langEntry
}

// IsZero 报告是否为零值（未指定语言）。
func (l Lang) IsZero() bool { return l.entry == nil }

// String 返回规范化的语言键（如 zh-CN），即 entity.User.界面语言 的落库值。
//
// 零值返回空串——与 entity.User.Language 的 string 零值对应，表示「未指定」。
// 注意本方法返回的是**机器可读的语言键**，不是给用户看的语言名称；后者是
// DisplayName。
func (l Lang) String() string {
	if l.entry == nil {
		return ""
	}
	return l.entry.tag
}

// DisplayName 返回该语言的**自称**（如 zh-CN → 简体中文），供 K-1 的界面语言
// 下拉作标签使用。
//
// 用自称而非按当前界面语言翻译，是语言选择器的通行做法，也是唯一自洽的做法：
// 用户之所以要切语言，往往正因为当前界面的语言他读不懂——把「英语」这一项写成
// 中文，看不懂中文的用户就找不到它。这也让本项无需进语言文件：加语种时它随
// 语言文件的 display_name 字段一并给出，不必回改任何既有语种的文件。
//
// 零值返回空串。
func (l Lang) DisplayName() string {
	if l.entry == nil {
		return ""
	}
	return l.entry.displayName
}

// Equal 报告两个语言是否相同。
//
// 与 currency.Code.Equal 同构：Lang 本可用 == 直接比较（结构体只含一个指针），
// 但显式提供 Equal 让调用点意图更清楚，也为将来 Lang 增加字段留出余地。
func (l Lang) Equal(other Lang) bool { return l.entry == other.entry }

// langEntry 是一个已装载语言的内部条目。
//
// 全部字段私有且装载后只读，故包外既无法构造 Lang、也无法改动其内容——
// 这与 currency.entry 是同一个手法。
type langEntry struct {
	tag         string // 规范化的语言键，如 zh-CN
	displayName string // 该语言的自称，如 简体中文
}
