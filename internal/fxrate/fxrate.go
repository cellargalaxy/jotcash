// Package fxrate 是 jotcash 的日汇率获取端口（L3 端口与边界层）。
//
// # 承载
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L3 表
// fxrate 行，本包承载四项：
//
//	日汇率获取接口（按币种对 + 日期）+ 按 currency 的汇率源标识路由的注册表
//	（与 parser 注册表同构，注册项由 app 从 L4 注入）+ 失败语义：失败即返回
//	错误，**禁止静默 1:1**（I-5）。**返回值精度不设限**——规整到 8 位由
//	rule/convert 统一完成（T39，单一规整点）
//
// 四项各自的落点：
//
//	① 获取接口     Fetcher（入参 Query = 币种对 + 日期）
//	② 路由注册表   Registry（NewRegistry 构造，Fetch 按 Query.From 的汇率源路由）
//	③ 失败语义     六个哨兵错误，全部返回错误、无任何一条返回 1 的路径
//	④ 精度不设限   本包对返回值只判「是否为正」，不判位数、不做规整
//
// # 端口包只有接口、类型与注册表
//
// 分层 §六 L3 表头把 store / fxrate / parser 归为**端口包**：「只有接口、
// 类型与注册表，实现在 L4」，§十一 阶段 4 的完成标志同样是「只有接口、类型
// 与注册表，可编译」。故本包**不含任何汇率源实现**，也不含日汇率缓存——
// 缓存是 L4 的承载（分层 §六 L4 fxrate/<源> 行「具体汇率源实现 + 日汇率缓存
// （同日同币种对只取一次）」）。
//
// 本轮随附的 fxrate/fixed 是一个**占位实现**，位置在 L4、必须由 app 显式注册，
// 详见该包的注释。
//
// # 禁止静默 1:1 是本包最硬的一条
//
// 终版 I-5 明写「自动获取或重拉失败时该行汇率变为必填，**未填不可提交 /
// 不可保存，不静默按 1:1 处理**」。落到本包是一条可穷举的结构性要求：
// **本包不存在任何一条返回汇率 1 的代码路径**，也不存在任何一条「出错了但
// 返回 nil error」的路径。四种失败一律返回错误：
//
//	入参不合法（币种为零值、日期为零值） → ErrFromCurrencyEmpty 等
//	汇率源未注册                         → ErrSourceUnregistered
//	汇率源自身失败                       → ErrFetch（包住源的原始错误）
//	汇率源返回非正数                     → ErrRateNotPositive
//
// 最后一条是最容易被漏掉的一条：汇率源返回 0 时 err 为 nil，放行后
// rule/convert 虽有 ErrRateNotPositive 兜底，但错误已离开现场——排错时看到的
// 是「折算失败」而不是「某某汇率源返回了 0」。更坏的情形是某个源以 0 表示
// 「无此币种对」，那时恒等式 `本位币金额 = 支出金额 × 折算汇率` 照样成立，
// 而金额静默变成 0。故在端口边界判掉。
//
// # 不做 I-4 的同币种短路
//
// 终版 I-4「原币 = 本位币时汇率取 1」**不在本包**。分层 §四 修正 ② 写死了
// 它的落点：「`service/fx` 承载补终版 I-4 的短路判据「**支出币种 = 当前本位币
// 时汇率直接取 1，不调 `fxrate`**」」。
//
// 因此本包对 From == To 不做任何特殊处理：既不返回 1，也不拒绝——它只是一个
// 普通的币种对，照常路由给汇率源。两个理由：
//
//   - 在此返回 1 就是**在端口里造出一条返回 1 的路径**，与上一节的结构性要求
//     直接冲突。同币种取 1 是「数学上恰好为 1」，静默 1:1 是「拿不到汇率却
//     假装是 1」，二者数值相同、性质相反，而代码分不清调用它的是哪一种。
//   - 反过来拒绝它同样有害：真出现 From == To（service/fx 的短路失效）时报错，
//     会让一笔本币记账被 I-5 判成「汇率转必填」，把一个服务层的接线错误
//     变成用户面前的表单故障。
//
// # 路由按 Query.From 的汇率源标识
//
// 承载写的是「按 currency 的汇率源标识路由」，而一次查询有两个币种。本包取
// **From（原币 / 支出币种）** 一侧，理由是汇率源的差异来自被定价的那一侧：
// To 恒为本位币，受 T38 约束只能是已启用且小数位 ≤ 2 的候选币种
// （currency.BaseCandidates），而 From 是支出币种、**不受该限制**
// （分层 §六 L1.0「支出币种不受此限」），KWD 这类冷门币种只会出现在 From。
//
// 当前 currency 表 27 项全部指向 currency.RateSourceDefault，两侧取谁在行为上
// 完全一致，故这条选择当前**无法被任何用例区分**。为免它在加第二个汇率源那天
// 被默默地按另一种理解改掉，registry_test.go 的 TestAllCurrenciesShareOneSource
// 会在表里出现第二个汇率源标识时失败，强制那一轮回来重读本段。该项已作为待确认项
// 记入本轮 answer。
//
// # 返回值精度不设限，本包不规整
//
// T39 规定「**规整点唯一**：rule/convert 作为不变式 1a 的唯一实现处，在做乘法前
// 先把入参汇率规整到 8 位」，分层 §六 L3 fxrate 行随即写明「**返回值精度不设限**
// ——规整到 8 位由 rule/convert 统一完成（T39，单一规整点）」。
//
// 故本包**不调用 decimal.RoundRate、不判 FitsScale(RateScale)**。在此顺手规整
// 的后果不是数值出错而是口径出现第二个来源：自动获取的汇率在本包规整一次、
// I-5 手填的汇率在 rule/convert 规整一次，两条入口从此不再共用同一个规整点，
// 而「共用」正是 T39 要解决的问题本身。
//
// 与此一致，本包也**不 import base/decimal 之外的任何精度口径**：只用
// decimal.Decimal 作为汇率的载体，不引用 RateScale。
//
// # 为什么不用第三方汇率库
//
// 优先采用成熟库是本轮的要求，故对 GitHub 上的候选做了实测口径的核对
// （Star 与最近推送取自 GitHub API，2026-09 查询）：
//
//	Rhymond/go-money        1912★  货币计算，无汇率获取能力
//	bojanz/currency          644★  含汇率类型，已在 currency 那一轮被否决为运行时依赖
//	mattevans/dinero          86★  Open Exchange Rates 客户端（单一源、含内存缓存）
//	govalues/money            56★  货币计算，无汇率获取能力
//	openprovider/ecbrates     37★  ECB，最近推送 2016-11
//	peterhellberg/fixer       17★  Fixer.io，最近推送 2020-04
//	wowsignal-io/go-forex     10★  多央行日汇率，最近推送 2024-06
//	jieggii/ecbratex           2★  ECB，作者自述 work in progress
//
// 结论是**本层不引入任何一个**，理由与「库好不好」无关，而在于本包是**端口**：
//
//   - 端口的职责恰是「不绑定选型」。分层 准则 4「端口在下、实现在上……换选型 =
//     加平级子包 + 改 app 一行」，在 L3 引入任何一家源的客户端，等于把选型钉死在
//     换选型时不该动的那一层，L5 也会经本包间接依赖它。
//   - 上表全部候选都是**某一家源的客户端**（ecbrates / fixer / dinero 各对一家），
//     而本包要的是「按币种路由到不同源」的注册表——那正是它们上面的一层，
//     没有一家提供。
//   - 而且**汇率源尚未选定**：分层 §六 L4 写明「`fxrate/<源>` 的具体包名随汇率源
//     选定后确定」，configs/jotcash.example.yaml 的 fx_rate_endpoint /
//     fx_rate_secret 两项当前留空并注明「汇率源尚未选定」。选源之前引入客户端，
//     引进来的多半就是要被换掉的那一个。
//
// 该用则用的对照仍然成立：选定汇率源后，L4 的 fxrate/<源> 里**应当**优先采用
// 该源的成熟客户端库（如选 Open Exchange Rates 则 mattevans/dinero 86★ 值得
// 认真评估，它自带按基准币种的内存缓存，与 L4 承载的「日汇率缓存」正好对齐）。
// 本轮不引入，是因为「引哪一个」取决于还没做的那个决定。
//
// # 不做什么
//
//   - **不实现任何汇率源**，不发 HTTP、不读配置。端点与密钥由 app 注入给 L4
//     （分层 §六 L4 与约定 8）。
//   - **不做日汇率缓存**。那是 L4 的承载（「同日同币种对只取一次」）。放在此处
//     会让每个源被迫接受同一套缓存口径，而各源的更新时点与限流规则并不相同。
//   - **不规整精度、不做舍入**（见上文，T39）。
//   - **不做同币种短路**（见上文，I-4 归 service/fx）。
//   - **不判定 I-7 该重算哪些行**。判据是 `折算本位币币种 ≠ User.本位币`，
//     需要读用户偏好，归 service/fx（不变式 2）。
//   - **不落库、不写审计**。「本位币金额重算」审计由 service/fx 在重算收尾写
//     （分层 §六 L5 5.2）。
//   - **不打日志**。见「不打日志」一节。
//
// # 不打日志
//
// 本包一行日志都不打，也不 import logrus。理由是 I-7 逐笔重算会以**每笔明细
// 一次**的频率调用本包：一个汇率源故障，在端口打一行日志就是 N 行几乎相同的
// 记录，真正需要被看见的那一条根因反而被淹没。失败一律以错误返回，由
// service/fx 在批处理收尾处**汇总**成一条审计（分层 §六 L5 5.2「记成功/失败
// 笔数」）与相应日志。
//
// 例外在 L4：fxrate/fixed 在**构造时**打一条警告，那是每进程一次而非每笔一次。
//
// # 错误是哨兵
//
// 与 base/decimal、base/calendar、currency、enum、rule/convert、rule/amortize
// 一致：错误一律为本包的哨兵错误，**不返回 base/errs 的分档错误**，本包因此
// 不 import base/errs。
//
// 理由与 rule/convert 相同——档位在本包无从判定，且**同一个错误在两条链路上
// 分属不同档位**：
//
//	ErrFetch  经 D-7 录入 / F-5 编辑    → 用户输入错误档，触发 I-5「汇率转必填」
//	          经 I-7 批量重算            → 系统错误档，计入失败笔数、不打扰用户
//	ErrDateEmpty  经 dto 进来           → 用户输入错误档（支出日期必填）
//	              经 I-7 出现            → 系统错误档（库里存着无日期的明细，属数据损坏）
//
// 由 service/fx 用 errors.Is 判定后自行套档位与文案键。
//
// 需要留意本包与同层 parser 在这一点上**刻意不同**：parser 的承载明写「一律
// 返回带文案键的 base/errs」，因为 D-3 已经把五类解析错误的档位定死了；
// 本包的承载只写「失败即返回错误」，未定档位，故按仓库内多数包的取向走哨兵。
//
// # 依赖边界
//
// 依赖恰为 currency（L1.0）、base/calendar 与 base/decimal（L0）三个包，
// 与承载的依赖列「currency、base/*」一致。本包处于 L3，**不得依赖 L4 及以上**
// （fxrate/<源> / service / api / app），否则端口与实现成环；也不依赖同层的
// store / parser / token / i18n（分层 §六「层内规则：同层包互不依赖」）。
// 由 consumer_check_test.go 的 TestDependencyBoundary 以 AST 扫描双向锁死。
//
// 特别地，本包**不依赖 entity**：一次汇率查询只需要「两个币种 + 一个日期」，
// 收 entity.Expense 会把一堆与取汇率无关的字段带进签名，取向同 rule/convert
// 「为什么不收 entity.Expense」。
//
// # 并发
//
// Query 是不可变值类型。Registry 构造后不可变、无写入口，Fetch 只读，
// 可并发使用（见 registry.go 的「为什么没有 Register 方法」）。
// 由 TestRegistryConcurrentFetch 以 -race 锁死。
package fxrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 哨兵错误。上层（service/fx）用 errors.Is 判定后，按自身场景映射到 base/errs
// 的档位与文案键（为什么不在本包分档，见包注释「错误是哨兵」）。
//
// 六个错误覆盖了本包全部的失败路径——**没有第七条路径，也没有任何一条返回 1
// 的路径**，这正是 I-5「禁止静默 1:1」在本包的落地形态。
var (
	// ErrFromCurrencyEmpty 表示原币（支出币种）为零值（未指定）。
	//
	// 必须显式拦下，不能放行：零值 currency.Code 的 RateSource() 返回**空串**
	// （见 currency.Code.RateSource 的注释），拿空串去查注册表必然落空，
	// 于是真实原因「币种没填」会被报成 ErrSourceUnregistered「汇率源未注册」，
	// 把排错引向 app 的注册代码——而那里根本没有问题。
	ErrFromCurrencyEmpty = errors.New("fxrate: 原币币种未指定")

	// ErrToCurrencyEmpty 表示目标币（本位币）为零值（未指定）。
	//
	// 目标币不参与路由，因此不拦也能查出一个「汇率」来——只是不知道它是
	// 兑成什么的汇率。终版 §四 3 把 折算本位币币种 列为必填，一个没有目标币的
	// 汇率无法被任何一方解释，放行等于让后续的 折算本位币币种 无处可取。
	ErrToCurrencyEmpty = errors.New("fxrate: 目标币种未指定")

	// ErrDateEmpty 表示日期为零值（未指定）。
	//
	// 这条拦的是一条**静默取到错值**的路径，而不只是参数校验。终版 I-3/I-4
	// 要的是「按**支出日期**的日汇率」，而绝大多数汇率源在日期缺省时返回的是
	// **最新汇率**（latest 端点）。放行的后果是：一笔三个月前的支出按今天的
	// 汇率折算，金额是错的、err 是 nil、日志里什么也没有。
	//
	// 前提 7 下支出日期是不带时区的字面值（base/calendar.Date），零值意味着
	// 「没填」而不是「今天」——本包不会把它当成今天，那属于替调用方做决定。
	ErrDateEmpty = errors.New("fxrate: 日期未指定")

	// ErrSourceUnregistered 表示该币种的汇率源标识在注册表中没有对应实现。
	//
	// 正常链路走不到这里：NewRegistry 在构造时已按 currency.All() 校验过全覆盖
	// （见 NewRegistry 的注释），未覆盖则进程根本起不来。它拦的是那道校验之后
	// 币种表又被改动的情形，属「本该不可能，发生了就必须响亮失败」。
	//
	// **绝不能退化成返回 1**：那正是 I-5 禁止的静默 1:1，且它会在「某个币种忘了
	// 注册」这种纯接线问题上，把错误的金额写进库里。
	ErrSourceUnregistered = errors.New("fxrate: 该币种的汇率源未注册")

	// ErrFetch 表示汇率源自身获取失败，是 I-5「重拉失败」在本包的载体。
	//
	// 源的原始错误以第二个 %w 包在里面，故 errors.Is 对二者**同时**成立：
	// service/fx 用 errors.Is(err, fxrate.ErrFetch) 判定「该走 I-5 兜底」，
	// 排错时仍可从链上取到源的具体错误（超时、限流、无此币种对）。
	//
	// 这是六个错误里唯一一个**预期会在正常运行中出现**的：网络抖动、源限流、
	// 冷门币种对缺数据都会走到这里，它对应的是一个用户可自行兜底的表单态
	// （I-5 汇率转必填），不是故障。
	ErrFetch = errors.New("fxrate: 汇率源获取失败")

	// ErrRateNotPositive 表示汇率源返回了非正数（零或负数）。
	//
	// 汇率是「1 单位原币可兑换的本位币数量」（终版 §四 3），零与负数都不是
	// 合法取值。**必须在端口边界判掉**，理由是这条路径上 err 为 nil：
	//   - 返回 **0**：某些源以 0 表示「无此币种对」。放行后本位币金额被算成 0，
	//     而恒等式 `本位币金额 = 支出金额 × 折算汇率` 照样成立——钱没了，
	//     不变式却没被打破，任何校验都发现不了。
	//   - 返回 **负数**：符号整体翻转，退款变支出、支出变退款。
	//
	// rule/convert 有同名的哨兵兜底，但那时错误已离开现场：service/fx 看到的是
	// 「折算失败」而不是「某某汇率源返回了 0」，而这两者的处置完全不同
	// （前者查代码，后者换源或报障）。
	ErrRateNotPositive = errors.New("fxrate: 汇率源返回的汇率必须为正数")
)

// Query 是一次日汇率查询的全部入参，即承载所说的「按币种对 + 日期」。
//
// # 为什么是结构体而不是三个散参
//
// 散参签名 Fetch(ctx, from, to currency.Code, date calendar.Date) 有一处**编译器
// 拦不住**的隐患：from 与 to 同类型，写反了照常编译、照常返回一个汇率，只是每一笔
// 折算都取了倒数。一笔 USD→CNY 的支出会按 CNY→USD 的汇率折算，金额差约 50 倍，
// 而且六种触发下每一次重拉都稳定地错成同一个值，看不出任何异常波动。
//
// 具名字段把这个错误挪到了调用点可见的位置：写 Query{From: …, To: …} 时字段名
// 就在眼前。这与 rule/convert 用 Input 三元组、rule/amortize 不收 entity.Expense
// 是同一条准则 6「约束优先用结构表达」。
//
// # 三个字段都必填
//
// 零值不代表任何默认：币种零值不代表本位币、日期零值不代表今天。三者各有一个
// 哨兵错误，见 Validate。
//
// 值类型、不可变，可并发使用。
type Query struct {
	// From 原币，即 Expense.支出币种（entity.Expense.Currency）。
	//
	// **路由依据**：Registry 按 From.RateSource() 选择汇率源（理由见包注释
	// 「路由按 Query.From 的汇率源标识」）。
	//
	// 不受 T38 本位币候选口径限制——支出币种可以是 KWD 这类 3 位小数币种
	// （分层 §六 L1.0「支出币种不受此限」）。
	From currency.Code

	// To 目标币，即折算到的本位币（User.本位币）。
	//
	// 不参与路由。本包**不校验它是否满足 T38 候选口径**：那是
	// service/preference 在 K-1 切换时的职责（currency.ParseBase），
	// 且 rule/convert 另有 ErrBaseCurrencyNotCandidate 兜底。在此重复校验
	// 会让同一条口径有三个执法点，改口径时必漏一个。
	To currency.Code

	// Date 汇率日期，即 Expense.支出日期（终版 I-4「按支出日期的日汇率折算」）。
	//
	// 类型是 base/calendar.Date（不带时区的日历日期），这是前提 7 的要求：
	// 支出日期全程按账单字面值处理、不做时区换算。**任何汇率源实现都不得**
	// 把它转成 time.Time 再按某个时区取「那一天」——那正是前提 7 要杜绝的。
	//
	// I-7 重算取哪一天的汇率（T31）是 service/fx 的决定，本包只接收结果。
	Date calendar.Date
}

// Validate 校验三个字段均已指定，返回对应的哨兵错误。
//
// 校验顺序为 From → To → Date，遇第一项不满足即返回，不汇总——三者都是必填，
// 汇总出的复合错误在任何一个调用点都没有额外用处。
//
// Registry.Fetch **已在路由前调用本方法**，L4 的汇率源实现不必重复调用；
// 导出它是为了让 L4 的用例能以同一套判据构造合法入参，避免各源各写一份。
func (q Query) Validate() error {
	//先判 From：它是路由依据，零值的 RateSource() 是空串（见 currency 该方法的
	//注释），不先拦下就会被报成「汇率源未注册」，指向的原因与事实不符
	if q.From.IsZero() {
		return ErrFromCurrencyEmpty
	}
	if q.To.IsZero() {
		return ErrToCurrencyEmpty
	}
	if q.Date.IsZero() {
		return ErrDateEmpty
	}
	return nil
}

// String 返回形如 "USD→CNY@2026-09-08" 的紧凑描述，供错误信息与排错使用
// （约定 3：注释与日志一律中文；本方法输出的是币种代码与日期，无需翻译）。
//
// 零值字段以 ? 占位而不是留空：错误信息里出现 "?→CNY@2026-09-08" 一眼可见是
// 原币没填，而留空得到 "→CNY@2026-09-08"，缺了什么反而要数箭头。
// 取向同 base/errs 的 Kind.String 与 rule/convert 的 Trigger.String——
// 非法值要打印得出来，不能变成看不见的空串。
func (q Query) String() string {
	code := func(c currency.Code) string {
		if c.IsZero() {
			return "?"
		}
		return c.String()
	}
	date := q.Date.String()
	if date == "" {
		//calendar.Date 零值的 String() 返回空串（它刻意不返回 "0000-00-00"）
		date = "?"
	}
	return fmt.Sprintf("%s→%s@%s", code(q.From), code(q.To), date)
}

// Fetcher 是日汇率获取接口，由 L4 的具体汇率源实现（分层 §六 L4 fxrate/<源>）。
//
// # 契约
//
// 返回「1 单位 Query.From 可兑换的 Query.To 数量」，即终版 §四 3 对
// Expense.折算汇率 的定义。实现方必须遵守四条：
//
//  1. **失败即返回错误，禁止返回 1**（I-5）。拿不到汇率时返回 (零值, err)，
//     不得以 1、以最近一次成功值、以反向汇率的倒数等任何方式代偿——那些都是
//     静默 1:1 的变体，且比返回 1 更难被发现。
//  2. **返回值精度不设限**（T39）。有多少位给多少位，不要自行规整到 8 位：
//     规整点唯一，在 rule/convert。
//  3. **按 Query.Date 取那一天的汇率**，不得退化为最新汇率。日期是不带时区的
//     字面值（前提 7），不得转成 time.Time 后按某时区解释。
//  4. **可并发调用**。service/fx 的 I-7 会连续调用；即便实现是串行的，
//     也不得因并发而返回错值。
//
// 入参 Query 由 Registry 在路由前校验过（Query.Validate），实现方可假定三个
// 字段均非零值。
//
// ctx 用于取消与超时：I-7 是可中断可续跑的批处理（终版 I-7），实现方应当在
// 网络等待处响应 ctx.Done()，否则一次中断要等到全部明细跑完才生效。
//
// # 为什么接口只有一个方法
//
// 「按币种对取某天的汇率」是 service/fx 对汇率源的全部要求：I-3 自动获取、
// I-4 入账折算、I-7 逐笔重算三条链路要的都是这一件事。批量取、区间取、
// 列支持币种等方法都能由它组合或由 L4 内部缓存解决，放进接口只会抬高「换一家
// 汇率源」的门槛——而降低那个门槛正是端口存在的理由（准则 4）。
type Fetcher interface {
	// Fetch 返回 1 单位 q.From 可兑换的 q.To 数量。失败返回错误，**不得返回 1**。
	Fetch(ctx context.Context, q Query) (decimal.Decimal, error)
}
