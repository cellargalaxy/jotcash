package parser

import (
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/enum"
)

// Registry 是按解析器类型键路由的解析器注册表，即承载所说「按解析器类型键的
// 注册表（D-1）」。
//
// # 为什么是实例类型，而不是包级可变全局表
//
// 常见形态是「包级 map + 包级 Register(pt, p)，各实现在 init 里自注册」。
// 本包否决它，理由已在 currency 那一轮被实测登记过（currency 包注释
// 「为什么不直接用第三方库」记录了第三方库的包级 Register 与读并发即
// DATA RACE）。三项对比：
//
//	问题                          包级可变表        本包的实例表
//	任何 import 者都能改路由       能（可静默换实现） 不能，无导出写入口
//	注册与读并发                  不安全            构造后只读，天然安全、无需锁
//	注册顺序依赖 init 与导入副作用  是                否，装配点唯一且显式
//
// 第一项是最要紧的：解析器决定账单里的每一笔钱怎么读出来，一个能被任意包改写
// 的全局路由表意味着「谁都可以静默改数」。
//
// 于是本包不提供任何 Register / Unregister 方法：注册项由 app 在启动时经
// NewRegistry 一次性注入（约定 8「所需外部参数由 app 注入」、分层 §六 L3 表头
// 「端口包……注册项由 app 从 L4 注入」），构造完成即只读。
//
// # 并发
//
// 构造后只读，可并发调用全部方法，无需锁。这条性质靠「内部 map 在 NewRegistry
// 返回后不再被写」保证，由 TestRegistryConcurrentRead 在 -race 下钉住。
//
// # 零值可用
//
// 零值 Registry（未经 NewRegistry）是一个**空表**：Get 一律返回
// KeyNotRegistered（500），Has 一律 false，Types 返回空切片，Len 为 0。
// 不 panic——空表与「漏注册」是同一种故障，走同一条报错路径即可。
type Registry struct {
	// parsers 是类型键 → 实现的映射。构造后不再写入。
	//
	// 不导出且不提供访问器：导出它（或提供返回 map 的方法）等于把只读性
	// 交给调用方自律——拿到 map 的人可以直接改。要遍历用 Types()，它返回副本。
	parsers map[enum.ParserType]Parser
}

// NewRegistry 用给定的解析器实现构造注册表，是本包唯一的注册入口。
//
// 唯一调用方是 app（约定 8）。典型形态：
//
//	reg, err := parser.NewRegistry(csv.New())
//
// # 构造期自校验四条，任一不满足即失败
//
//  1. **实现非 nil**。nil 实现会在 Parse 时 panic，而那时离装配点已经很远。
//  2. **Type() 是合法枚举键**（enum.ParserType.Valid，含已停用）。允许已停用
//     的键注册：终版 §五 2 规定枚举项「只能停用不能删除」，历史 File 行仍存着
//     停用键、仍需能重新解析；若这里按 Enabled 卡，那些历史文件就再也读不出来。
//     「停用项不得被**新选择**」是 C-1 的业务规则，归 service/file。
//  3. **键不重复**。两个实现抢同一个键时，map 赋值会让后者静默覆盖前者——
//     上传同一份账单，行为取决于 app 里两行代码的先后顺序。必须响亮失败。
//  4. （另出方法，见 CheckEnabledCoverage）**启用项全有实现**。这条是软校验，
//     不在构造期强制，理由见该方法。
//
// 返回的错误是 fmt.Errorf 包装的普通错误，**不带 base/errs 档位与文案键**：
// 装配失败发生在启动阶段，没有 HTTP 请求、没有界面语言，也没有用户能看到它
// ——它只会进启动日志并让进程退出。给它套一个面向用户的文案键，反而会让人
// 以为这是一条能回给前端的错误。这与包内其余错误的取向不同，是刻意的。
//
// 允许零个实现（NewRegistry() 得一个空表且 err 为 nil）：那是「本轮还没有任何
// L4 实现」的合法状态，正是当前阶段 4 的实际情形（parser/csv 属阶段 6）。
// 空表的漏配由 CheckEnabledCoverage 报告，而不是在此处一律拒绝——否则本包在
// 阶段 4 连自己的测试都构造不出一个合法实例。
func NewRegistry(parsers ...Parser) (*Registry, error) {
	m := make(map[enum.ParserType]Parser, len(parsers))
	for i, p := range parsers {
		//带上下标：nil 实现取不到任何可辨识信息，下标是唯一能指回 app 里
		//那一行的线索
		if p == nil {
			return nil, fmt.Errorf("parser: 第 %d 个解析器为 nil", i)
		}
		pt := p.Type()
		if !pt.Valid() {
			return nil, fmt.Errorf("parser: 第 %d 个解析器的类型键非法: %q", i, pt.Code())
		}
		if _, dup := m[pt]; dup {
			return nil, fmt.Errorf("parser: 解析器类型键重复注册: %q", pt.Code())
		}
		m[pt] = p
	}
	return &Registry{parsers: m}, nil
}

// Get 按解析器类型键取实现，是 service/intake 走进解析前的唯一取用入口。
//
// 返回值语义：
//   - (实现, nil)  命中；
//   - (nil, 400)   键非法或为零值 → KeyTypeUnsupported（用户没选或选错）；
//   - (nil, 500)   键合法但没有对应实现 → KeyNotRegistered（app 漏注册）。
//
// 两类失败**分属两档**，理由见 KeyNotRegistered 的注释。先判合法性再查表：
// 反过来的话，一个非法键会因查表未命中而被报成 500，把用户的输入错误说成
// 系统故障。
//
// 接收者是值而非指针，因此 nil 的 *Registry 调用它会 panic——这与 Go 的惯例
// 一致（nil 指针解引用），且 NewRegistry 永不返回 nil 与 nil 错误的组合。
// 需要一个「什么都没有」的表时用零值 Registry{}，它是可用的空表。
func (r Registry) Get(pt enum.ParserType) (Parser, error) {
	if !pt.Valid() {
		return nil, NewTypeUnsupportedError(pt.Code())
	}
	p, ok := r.parsers[pt]
	if !ok {
		return nil, NewNotRegisteredError(pt)
	}
	return p, nil
}

// Has 报告该键是否有对应实现。
//
// 供 service/file 在 C-1 上传校验里做「这个类型现在能用吗」的判定，
// 不必构造错误。非法键与零值一律 false。
func (r Registry) Has(pt enum.ParserType) bool {
	if !pt.Valid() {
		return false
	}
	_, ok := r.parsers[pt]
	return ok
}

// Len 返回已注册的实现个数。供启动日志与自检使用。
func (r Registry) Len() int {
	return len(r.parsers)
}

// Types 返回已注册的全部类型键，**按 enum.ParserTypes() 的声明序**。
//
// 顺序必须稳定：D-2 的上传页下拉直接由它驱动，用 map 遍历序会让下拉每次刷新
// 都换一次顺序。声明序而非字典序，是因为 enum 那一侧已经按「先常用、后小众」
// 的意图排好了，字典序会把这个意图抹掉。
//
// 返回新切片，调用方改它不影响本表。
func (r Registry) Types() []enum.ParserType {
	types := make([]enum.ParserType, 0, len(r.parsers))
	//以 enum 的声明序为基准过滤，而不是遍历自己的 map 再排序：
	//这样顺序口径与 D-2 下拉、enum.EnabledParserTypes() 完全一致
	for _, pt := range enum.ParserTypes() {
		if _, ok := r.parsers[pt]; ok {
			types = append(types, pt)
		}
	}
	return types
}

// CheckEnabledCoverage 报告是否每个**已启用**的解析器类型都有对应实现，供 app
// 在启动时自检（建议：报错即让启动失败，或至少打一条告警日志）。
//
// # 为什么单独出一个方法，而不并进 NewRegistry
//
// 这条校验的判据是 enum 侧的「是否启用」，与注册表自身的自洽性不同：
//
//   - NewRegistry 校验的是**这次装配本身**有没有写错（nil、非法键、重键），
//     那些无论何时都是错的；
//   - 本方法校验的是**枚举表与装配的匹配度**，它在开发过程中会合法地不满足
//     ——当前阶段 4 就是如此：enum 已声明 generic_csv，而 parser/csv 属阶段 6
//     还没写。若把它并进构造期，本包在阶段 4 连一个合法的 Registry 都构造不出。
//
// 拆开之后，两件事各自在该失败的时候失败：装配写错 → 构造即失败；枚举与实现
// 对不上 → app 自行决定是拒绝启动还是告警放行（正式上线时应当拒绝）。
//
// 返回值是缺失的键列表（按 enum 声明序）与错误：
//   - 全覆盖 → (nil, nil)；
//   - 有缺失 → (缺失键列表, 500 档错误 KeyNotRegistered，参数 ParserType 为
//     缺失键的逗号连接)。
//
// 返回列表**与**错误，而不只返回其中一个：app 需要列表去打一条能直接照着改的
// 日志（缺了哪几个），也需要一个能直接往上抛的错误。
//
// 只查已启用项：停用项没有实现是正常状态（停用往往正是因为实现被移除了）。
func (r Registry) CheckEnabledCoverage() ([]enum.ParserType, error) {
	var missing []enum.ParserType
	for _, pt := range enum.EnabledParserTypes() {
		if _, ok := r.parsers[pt]; !ok {
			missing = append(missing, pt)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	codes := make([]string, 0, len(missing))
	for _, pt := range missing {
		codes = append(codes, pt.Code())
	}
	return missing, NewNotRegisteredError(enum.ParserType(joinCodes(codes)))
}

// joinCodes 用「, 」连接码值，供 CheckEnabledCoverage 的错误参数使用。
//
// 不引 strings.Join 是为了让本文件的 import 只有 fmt 与 enum——本包是端口层，
// import 列表本身就是给读者看的依赖声明，越短越好读。连接逻辑三行写完，
// 与标准库行为一致（空切片得空串、单元素不加分隔符）。
func joinCodes(codes []string) string {
	out := ""
	for i, c := range codes {
		if i > 0 {
			out += ", "
		}
		out += c
	}
	return out
}
