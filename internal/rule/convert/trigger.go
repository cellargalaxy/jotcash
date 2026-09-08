package convert

import "fmt"

// Trigger 是终版 8.1「折算联动（6 种触发，三字段同进退）」中的一种触发。
//
// 零值不代表任何一种触发，传零值即报 ErrTrigger：忘传参数必须响亮失败，
// 不能静默按某一种触发把钱算错。取向与 decimal.RoundMode 一致——那里零值
// 同样不默许任何一种舍入方式。
//
// # 为什么触发要成为一等概念，而不是传一个 bool
//
// 六种触发之间的**唯一**差异是「折算本位币币种要不要置为当前本位币」，
// 而这个差异不体现在任何一个数值上：算错了不报错、金额也照样对，只有到
// 下一次 I-7 重算时才会暴露成「手填汇率被覆盖」或「未收敛行永远收敛不了」
// （终版 8.1 末段「规则一句话」）。
//
// 若把它做成 `Apply(in Input, adoptBase bool)`，调用点就是一个裸 bool，
// 传反了编译器不会有任何意见。做成具名触发后，调用点写的是
// TriggerAmountChanged 这种自解释的名字，且六种触发可被穷尽测试
// （见 TestTriggerMatrixMatchesSpec 对 8.1 表格的逐行比对）。
type Trigger uint8

const (
	// TriggerAmountChanged 改支出金额（汇率不动）——终版 8.1 第 1 行。
	//
	// **六种触发中唯一不改 折算本位币币种 的一种**，因此也是唯一一种
	// 舍入基准取「该行原有的折算本位币币种」而非当前本位币的触发。
	//
	// 保持原口径的理由（8.1 末段）：这一行可能是尚未收敛的行（折算本位币币种
	// ≠ 当前本位币），若在此顺手置为当前本位币，它就再也不会被 I-7 识别为
	// 待重算行——金额按旧汇率算、口径却标成新本位币，错得不留痕迹。
	TriggerAmountChanged Trigger = iota + 1

	// TriggerRateChanged 改折算汇率——终版 8.1 第 2 行，含两条入口：
	// I-5 用户手填兜底，以及 **D-7 入库首次获取**。
	//
	// 入库首次获取归入本触发，依据是 8.1 末段「重拉汇率的时机仅四处」把它
	// 与日期变更、币种变更、I-7 重算并列——四者都属「汇率按当前本位币重新
	// 拿到」，故一律置 折算本位币币种 = 当前本位币。新建行的 PriorBaseCurrency
	// 为零值，本触发不读它，因此不受影响。
	TriggerRateChanged

	// TriggerSpendDateChanged 改支出日期——终版 8.1 第 3 行。
	//
	// 调用方须注意：本触发**同时**触发不变式 1b（刷新摊分起止月）。
	// 分层 §九 不变式 1b 行明写「1a 与 1b 必须由 service/expense /
	// service/intake 在同一次保存内一并调用」——本包只管 1a，摊分起止月
	// 归 rule/amortize，两者都调是调用方的责任。
	TriggerSpendDateChanged

	// TriggerCurrencyChanged 改支出币种——终版 8.1 第 4 行。
	TriggerCurrencyChanged

	// TriggerRecalc I-7 重算 / 切换本位币——终版 8.1 第 5 行。
	//
	// 三字段同事务更新（不变式 2：重算的原子粒度是单笔）。
	// 「折算本位币币种 = 当前本位币」的行由调用方 service/fx 先行跳过，
	// 本包不做该判定——它需要读 User.本位币，属服务层职责。
	TriggerRecalc

	// TriggerAmortizeMonthsChanged 改摊分月数——终版 8.1 第 6 行。
	//
	// 三字段**全部不动**，本包是纯直通：仅刷新摊分结束月，而那归
	// rule/amortize。之所以仍把它列为一种触发而不是「不调用本包」，
	// 是为了让 8.1 的六行在本包内穷尽可查——调用方按触发查表即可，
	// 不必自行记住「这一种不用调」，漏调与错调都无从发生。
	//
	// 本触发**不做任何校验、恒不返回错误**：它不产生新数值，
	// 没有可算错的东西，对一行合法数据施加校验只会凭空造出失败路径。
	//
	// 输出是**零值 Output 且 Changed 为 false**，调用方据此跳过写回。
	// 不回传入参凑一个三元组：本包收不到该行现有的本位币金额，凑出来的
	// 三元组会被「整体写回」的调用方当真（见 Output.Changed 的注释）。
	TriggerAmortizeMonthsChanged
)

// behaviour 是一种触发对三字段的作用，即 8.1 表格的一行。
//
// 表格只有两列有实质差异，其余都是这两列的推论：
//
//	recalc    是否重算本位币金额
//	adoptBase 是否把 折算本位币币种 置为当前本位币
//
// 「舍入基准取哪个币种」不单列一列——它恒等于**输出三元组中的
// 折算本位币币种**，由 adoptBase 唯一决定（见 Apply 的注释）。
type behaviour struct {
	recalc    bool
	adoptBase bool
	name      string // 中文名，仅供日志与错误信息（约定 3）
}

// triggerTable 是 8.1 六种触发的完整矩阵，逐行对应终版 8.1 的表格。
//
// 这张表是不变式 1a 中「三字段联动」那一半的全部内容，另一半（恒等式本身）
// 在 Apply。表初始化后只读，无写入口，可并发使用。
var triggerTable = map[Trigger]behaviour{
	// 触发                          重算  置本位币  名称
	TriggerAmountChanged:         {true, false, "改支出金额"},
	TriggerRateChanged:           {true, true, "改折算汇率"},
	TriggerSpendDateChanged:      {true, true, "改支出日期"},
	TriggerCurrencyChanged:       {true, true, "改支出币种"},
	TriggerRecalc:                {true, true, "本位币重算"},
	TriggerAmortizeMonthsChanged: {false, false, "改摊分月数"},
}

// triggers 按声明序列出六种触发。
//
// 顺序即终版 8.1 表格的行序，供 Triggers() 与穷尽性测试使用。
// 不由 triggerTable 遍历生成——map 遍历顺序随机，测试报错时的行号会跳动。
var triggers = []Trigger{
	TriggerAmountChanged,
	TriggerRateChanged,
	TriggerSpendDateChanged,
	TriggerCurrencyChanged,
	TriggerRecalc,
	TriggerAmortizeMonthsChanged,
}

// Triggers 返回六种触发的副本，按终版 8.1 的表格行序。
//
// 供上层做穷尽性处理：service/expense 的编辑链路需要按「用户改了哪个字段」
// 分派到对应触发，有了本函数即可用单测锁死「六种触发都有分派分支」，
// 而不是漏掉一种在运行期静默走默认分支。
//
// 返回副本而非共享切片：调用方改动不会污染包状态。
func Triggers() []Trigger {
	out := make([]Trigger, len(triggers))
	copy(out, triggers)
	return out
}

// Valid 报告是否为六种触发之一。零值与任何越界值返回 false。
func (t Trigger) Valid() bool {
	_, ok := triggerTable[t]
	return ok
}

// String 返回触发的中文名，**仅供日志与排错**（约定 3）。
//
// 非法触发返回带数值的 unknown 形态而非空串：String 会被 %v 隐式调用，
// 空串会让日志里出现看不见的字段，反而更难排查。取向同 base/errs 的
// Kind.String 与 decimal.RoundMode.String。
func (t Trigger) String() string {
	if b, ok := triggerTable[t]; ok {
		return b.name
	}
	return fmt.Sprintf("未知触发(%d)", uint8(t))
}
