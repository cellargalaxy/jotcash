package fxrate

import (
	"context"
	"fmt"
	"sort"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// Registry 是「汇率源标识 → 实现」的注册表，即承载第二项「按 currency 的汇率源
// 标识路由的注册表（与 parser 注册表同构，注册项由 app 从 L4 注入）」。
//
// # 为什么没有 Register 方法
//
// 注册表是**构造时一次性填好、之后只读**的：NewRegistry 收下映射的副本，
// 此后无任何写入口。这不是风格取舍，而是 currency 那一轮**实测过的一条故障**
// 的直接结论——currency/currency.go 的包注释与 rule/convert 的包注释都记着
// 否决 bojanz/currency 作为运行时依赖的理由，其中第一条正是
// 「Register 改包级 map、与读并发即 DATA RACE」。
//
// 本包若提供 Register，同一个形态就会在自己身上重演：I-7 逐笔重算会以每笔明细
// 一次的频率并发调 Fetch（读 map），任何一次运行期 Register（写 map）都足以
// 让进程直接崩在 runtime 的并发写检测上。加锁能消除 race，但换来的是每笔重算
// 都要过一次锁，且「运行期换源」本身并无需求——分层 §六 L4 写的是
// 「注册项由 app 从 L4 注入」，app 在启动装配时注入一次即可。
//
// 于是本包用**结构**表达这条约束：没有写入口，就没有并发写。
// 这与 currency / enum 的包级表在 init 后只读是同一手法（准则 6）。
//
// # 与 parser 注册表同构
//
// 承载明写两者同构。同构点是「按枚举里的标识路由到 L4 实现，注册项由 app 注入」；
// 差异在于键的来源：parser 的键是 enum.ParserType（用户在 D-2 上传页显式选择），
// 本包的键是 currency.RateSource（**用户不可见**，由币种表决定）。这个差异带来
// 一条 parser 没有的要求——全覆盖校验，见 NewRegistry。
//
// 零值 Registry 不可用（Fetch 恒返回 ErrSourceUnregistered），必须经 NewRegistry
// 构造。用指针类型即为此：零值 *Registry 是 nil，误用会当场 panic 而不是静默
// 返回一个「查不到任何源」的空表——后者会让每一笔折算都走 I-5 兜底，
// 表现为「所有明细的汇率都要手填」，排错时很难联想到注册表没构造。
type Registry struct {
	// fetchers 是标识 → 实现的映射，构造后只读。
	//
	// 不导出且构造时深拷贝（见 NewRegistry），调用方持有的原 map 后续改动
	// 不会影响已构造的注册表——否则「构造后只读」只是口头约定。
	fetchers map[currency.RateSource]Fetcher
}

// NewRegistry 按「标识 → 实现」构造注册表，并校验**全部已知币种**都能路由到实现。
//
// 由 app 在启动装配时调用一次（分层 §六 L7 app 行：「按币种把 fxrate/<源>
// 注册进 fxrate 注册表」）。
//
// # 三项校验，全部在构造时完成
//
//	① 映射非空、每个标识非空串、每个实现非 nil
//	② currency.All() 的每一个币种，其 RateSource() 都能在映射中找到实现
//	③ 映射中不得有多余项（注册了却没有任何币种指向它）
//
// 校验②是本注册表与 parser 注册表最实质的差别，也是唯一必须在构造时做的一项。
// 理由是**汇率源标识对用户不可见**：D-2 的解析器类型是用户从下拉里选的，选到
// 一个未注册的类型会当场报错、影响面止于那一次上传；而汇率源由币种表决定，
// 漏注册一个源不会有任何征兆，直到某个用户第一次记一笔该币种的支出——那时它
// 表现为 I-5「汇率转必填」，与「网络不通」的表象完全一致，会被当成外部故障查很久。
//
// 含**已停用**币种（currency.All 的语义即含停用项）：终版 I-7 明写重算含已软删除
// 明细，而历史明细可能引用已停用的币种，它们同样要能取到汇率。
//
// 校验③拦的是相反的错误——app 注册了一个没有任何币种指向的源。它不会造成
// 运行期故障，但意味着「注册了却不生效」，多半是标识写错了一个字母
// （比如把 "default" 写成 "defualt"，此时②会先失败，③兜住的是两个源同时错配
// 而②恰好被另一个源覆盖过去的情形）。
//
// 三项任一不满足即返回错误，由 app 在启动时终止。不 panic：app 有权决定启动
// 失败时怎么退（打日志、返回码），端口包不替它做这个决定。
//
// # 参数
//
// fetchers 为「标识 → 实现」映射，本函数**深拷贝一份**，调用方后续改动原 map
// 不影响已构造的注册表。
func NewRegistry(fetchers map[currency.RateSource]Fetcher) (*Registry, error) {
	if len(fetchers) == 0 {
		return nil, fmt.Errorf("fxrate: 注册表为空，至少需注册一个汇率源实现")
	}

	cloned := make(map[currency.RateSource]Fetcher, len(fetchers))
	for src, f := range fetchers {
		if src == "" {
			return nil, fmt.Errorf("fxrate: 汇率源标识为空串")
		}
		//nil 实现必须在构造时拦下：放行则 Fetch 时 f.Fetch 直接 panic，
		//而那是在某笔明细折算的中途，离接线错误的现场已经很远
		if f == nil {
			return nil, fmt.Errorf("fxrate: 汇率源 %q 的实现为 nil", string(src))
		}
		cloned[src] = f
	}

	//② 全覆盖校验：每个已知币种（含已停用）都必须能路由到实现
	var missing []string
	used := make(map[currency.RateSource]bool, len(cloned))
	for _, c := range currency.All() {
		src := c.RateSource()
		if _, ok := cloned[src]; !ok {
			missing = append(missing, fmt.Sprintf("%s(%s)", c.String(), string(src)))
			continue
		}
		used[src] = true
	}
	if len(missing) > 0 {
		//排序后输出：currency.All() 已按代码字典序，此处再排一次是为了让
		//「同一份错误配置每次报出同一条信息」不依赖上游的顺序承诺
		sort.Strings(missing)
		return nil, fmt.Errorf("fxrate: 以下币种的汇率源未注册: %v", missing)
	}

	//③ 反向核对：注册了却没有任何币种指向的源，多半是标识写错
	var unused []string
	for src := range cloned {
		if !used[src] {
			unused = append(unused, string(src))
		}
	}
	if len(unused) > 0 {
		sort.Strings(unused)
		return nil, fmt.Errorf("fxrate: 以下汇率源已注册但无任何币种指向: %v", unused)
	}

	return &Registry{fetchers: cloned}, nil
}

// Sources 返回已注册的汇率源标识，按字典序。
//
// 供 app 在启动日志里记录装配结果，也供测试断言。返回新切片，调用方改动不会
// 污染注册表。
func (r *Registry) Sources() []currency.RateSource {
	if r == nil || len(r.fetchers) == 0 {
		return nil
	}
	out := make([]currency.RateSource, 0, len(r.fetchers))
	for src := range r.fetchers {
		out = append(out, src)
	}
	//map 遍历顺序随机，排序后才能让日志与用例断言稳定
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Fetcher 返回该标识对应的实现，第二个返回值表示是否存在。
//
// 供 app 与测试按标识取回实现；业务链路一律用 Fetch，不必自行路由——
// 自行路由就等于在本包之外复制一份「按 From 的汇率源选源」的规则。
func (r *Registry) Fetcher(src currency.RateSource) (Fetcher, bool) {
	if r == nil {
		return nil, false
	}
	f, ok := r.fetchers[src]
	return f, ok
}

// Fetch 按 q.From 的汇率源路由到对应实现并取回日汇率，是 service/fx 的唯一入口。
//
// 返回「1 单位 q.From 可兑换的 q.To 数量」，**精度不设限**（T39：规整到 8 位
// 由 rule/convert 统一完成，本包不规整）。
//
// # 四步，顺序不可调换
//
//	① 校验入参三字段非零（Query.Validate）
//	② 按 q.From.RateSource() 取实现，取不到即 ErrSourceUnregistered
//	③ 调用实现，出错即包成 ErrFetch
//	④ 校验返回值为正数，非正即 ErrRateNotPositive
//
// ①必须先于②：零值 currency.Code 的 RateSource() 返回空串，跳过①会把「币种
// 没填」报成「汇率源未注册」（见 ErrFromCurrencyEmpty 的注释）。
//
// ④必须存在且必须在此处：汇率源返回 0 时 err 为 nil，那条路径上没有任何其他
// 关卡（见 ErrRateNotPositive 的注释）。
//
// # 四条失败路径全部返回错误，没有第五条
//
// 终版 I-5 要求「不静默按 1:1 处理」。本方法**不存在任何一条返回 1 的路径**，
// 也不存在「出错却返回 nil error」的路径：出错时第一个返回值恒为
// decimal.Decimal 零值（即 0），而 0 在 entity.Expense.Rate 上的语义就是
// 「未填」——它会被 rule/convert 的 ErrRateNotPositive 再拦一道，
// 双重保证不会有一笔按 0 或按 1 折算出来的金额落库。
//
// # 同币种不短路
//
// q.From == q.To 时本方法**照常路由给汇率源**，既不返回 1 也不报错。
// I-4 的短路判据在 service/fx（分层 §四 修正 ②），理由见包注释
// 「不做 I-4 的同币种短路」。
//
// # 并发
//
// 只读 map、无状态，可并发调用；实现方的并发安全由 Fetcher 接口契约第 4 条要求。
func (r *Registry) Fetch(ctx context.Context, q Query) (decimal.Decimal, error) {
	if r == nil {
		//零值 *Registry 是接线错误（app 没构造就注入了）。返回错误而非 panic：
		//一次接线错误不该崩掉整个进程，但也绝不能静默取到汇率
		return decimal.Decimal{}, fmt.Errorf("%w: 注册表未构造, %s", ErrSourceUnregistered, q)
	}

	//① 入参校验先行，理由见 ErrFromCurrencyEmpty 的注释
	if err := q.Validate(); err != nil {
		return decimal.Decimal{}, err
	}

	//② 按原币的汇率源标识路由（为什么取 From 一侧，见包注释）
	src := q.From.RateSource()
	fetcher, ok := r.fetchers[src]
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("%w: %s, 标识=%q", ErrSourceUnregistered, q, string(src))
	}

	//③ 调用实现。源的原始错误以第二个 %w 包住，errors.Is 对 ErrFetch 与源的
	//哨兵同时成立——service/fx 据前者走 I-5 兜底，据后者区分超时与限流
	rate, err := fetcher.Fetch(ctx, q)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("%w: %s: %w", ErrFetch, q, err)
	}

	//④ 非正数一律拦下。这条路径上 err 为 nil，不判就会静默把钱算成 0 或翻转符号
	if !rate.IsPositive() {
		return decimal.Decimal{}, fmt.Errorf("%w: %s, 得 %s", ErrRateNotPositive, q, rate)
	}

	//不规整精度：T39 规定规整点唯一，在 rule/convert（见包注释）
	return rate, nil
}
