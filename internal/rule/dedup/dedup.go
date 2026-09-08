// Package dedup 是「记账系统功能与模型」8.3 判重的唯一实现处（分层 L2 规则层）。
//
// # 承载
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L2 表
// rule/dedup 行，本包承载：
//
//	8.3 判重：四要素（归属用户 + 支出日期 + 支出金额 + 支出币种）等值匹配、
//	编辑态排除自身、本次录入内部互查；**输出「命中行 → 冲突明细集合」映射**
//	（供 D-6 并列展示），区分「库内冲突」与「本次录入内部冲突」，只标记不处置
//
// 依赖恰为 entity 一个仓库内包，与承载表依赖列逐字一致。
//
// # 四要素，一项不多一项不少
//
// 终版 E-1 把判重口径定死为四个**必有字段**的等值匹配，并明确排除其余字段：
//
//	归属用户   OwnerUserID   跨用户绝不比对（前提 3 纯个人数据隔离）
//	支出日期   SpendDate     字面值，无时区换算（前提 7）
//	支出金额   Amount        原币金额，允许为负（退款、冲正）
//	支出币种   Currency      枚举代码
//
// **不纳入**卡片三属性、对手方、备注、支出类型、摊分月数、汇率等非必有字段
// （E-1 原文：「不设指纹属性，不纳入卡片、对手方、备注等非必有字段」）。理由是
// 终版 §一 定位「以银行账单导入为主要数据来源」，同一份账单重复导入时这些字段
// 完全相同、不提供任何区分度；而手工录入时它们又多半为空。把它们纳入只会让
// 「同一笔交易的两次录入」因为一个备注差异而漏判。
//
// 也**不存指纹字段**：E-1 明写「不设指纹属性」，故本包不在 entity 上要求任何
// 新列，判重键是每次比对时**现算**的派生值，不落库。这条与场景 A（分层 §十二
// 「接结构化数据源、加银行流水号」）的扩展方式一致——将来若真有流水号，是加一条
// **硬去重前置**，而不是改本包的四要素。
//
// # 只标记不处置
//
// E-2 定死「系统只标记『疑似重复』，是否真重复一律人工判断，**只提示不阻断**，
// 不做任何自动处置」。故本包：
//
//   - 只返回冲突映射，**不修改**任何输入（Check 的入参是只读的）；
//   - 不返回 error 表达「有重复」——重复不是错误，是一条待人工裁定的提示；
//   - 不提供「自动去重」「保留哪一条」之类的入口。想自动处置也没有可调的方法。
//
// # 两类冲突分开返回，不合并
//
// E-2 要求「既查本次录入内部重复，也查与已入库明细的重复」。二者在 D-6 预览页
// 上的形态不同：库内冲突要**并列展示那条已入库明细**供人工比对（D-6 原文），
// 内部冲突则是「本次这批里有两行一样」，展示的是同批的另一行。故 Result 把它们
// 放在两个字段里，由 L6 分别渲染，不合并成一个集合。
//
// # 比对范围由调用方给定，本包不查库
//
// E-1 的「比对范围为在库且未删除的明细」由 store 的「查重候选批量匹配」口径
// 落实（分层 §六 L3 store ④：「按『(日期, 金额, 币种)』元组集合一次取回在库
// 未删除候选」），本包只接收候选切片。这是 L2「纯逻辑、无外部 IO、可脱库单测」
// 的直接要求——本包不 import store，也没有任何 IO。
//
// 为此本包对外提供 Keys：由待查行算出**去重后的**候选键集合，供调用方一次性
// 向 store 取数，避免逐行往返。
//
// # 编辑态排除自身
//
// 8.3 明写「F-5 逐笔编辑时排除被编辑的这笔自身，否则必然自报重复」。本包用
// Request.ExcludeID 表达：候选中 ID 等于它的行一律跳过。
//
// 三种录入链路（文件导入 / 手工录入 / 复制已删除明细）传零值即可——它们的行
// 尚未入库、没有 ID，不存在自身。复制链路尤其不需要排除被复制的原明细：8.3
// 的注已写明「被复制的原明细已删除、不在比对范围内，因此不会与自身误报」。
//
// # 为什么用键索引而不是两两比较
//
// 朴素实现是双重循环逐对比较，O(n×m)。本包改为按四要素算一个规范字符串键、
// 用 map 分组，整体 O(n+m)。这依赖一条已实测的性质：
//
//	decimal.Decimal 的 String() 是数值的**规范形式**——
//	a.Equal(b) 当且仅当 a.String() == b.String()
//
// 该性质来自 decimal.Parse 与 FromUnits 在返回前都会剥掉尾随零（见 decimal
// 包注释「规范化」一节），因此 100.50 / 100.5 / 100.500 / +100.50 / 1.005e2
// 五种写法得到完全一致的内部表示与文本。keyer_test.go 的
// TestAmountKeyIsCanonical 用 Equal 与键相等的**穷举交叉**把这条性质钉死：
// 一旦 decimal 改动破坏了它，本包测试当场失败，而不是在某次导入中静默漏判。
//
// 不用 == 比较金额是编译期强制的：decimal.Decimal 刻意不可比较（decimal 包
// 注释「为什么类型不可比较」），含它的 entity.Expense 整体也不可比较，故
// map[entity.Expense]T 与 a.Amount == b.Amount 都是编译错误。本包的键是
// 字符串，绕开这一限制的同时**保持了 Equal 的语义**——这正是上面那条性质的价值。
//
// # 不做什么
//
//   - **不做模糊匹配 / 相似度**。E-1 是「等值匹配」，不是近似。金额差一分、
//     日期差一天都不是重复，交由人工在明细列表里判断。
//   - **不查库、不落库、不打日志**。L2 纯逻辑层，无 IO。
//   - **不判定「哪一条该保留」**。E-2 一律人工裁定。
//   - **不校验业务合法性**。空币种、零日期这类非法输入由 dto 与 service 层拦截；
//     本包对它们一视同仁地算键与分组（零值自成一组），不额外报错——在此报错
//     等于开第二处校验入口，违反准则 1。
//
// # 依赖边界
//
// 只 import entity 与标准库。特别地**不 import** base/errs（本包无错可返，
// 见「只标记不处置」）、不 import currency 与 base/decimal（四要素的类型由
// entity 字段自带，本包只调它们已有的 String()，不需要各自的包级函数）。
// consumer_check_test.go 的 TestDependencyBoundary 以 AST 扫描双向锁死。
package dedup

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cellargalaxy/jotcash/internal/entity"
)

// keySep 是四要素拼接判重键时的分隔符。
//
// 取 U+001F（单元分隔符）而非逗号或竖线：四要素中**没有**自由文本字段
// （日期定长 10 字符、币种定长 3 字母、金额只含数字与 . -），因此任何可打印
// 字符都不会被取值本身包含。选一个控制字符是为了让这条性质对将来也成立——
// 若哪天有人往键里加了自由文本字段，用逗号会立刻产生歧义
// （"a,b" + "c" 与 "a" + "b,c" 撞键），用 U+001F 则几乎不可能。
//
// TestKeySeparatorNoCollision 用「刻意构造的边界取值」验证不会串键。
const keySep = "\x1f"

// Request 是一次判重请求，覆盖 8.3 的四条链路。
//
// 四条链路的差异只体现在两个字段的取值上，逻辑完全共用——这是 8.3
// 「四条链路共用同一套逻辑」的结构表达：
//
//	文件导入        Rows = 解析出的 N 行     ExcludeID = 0
//	手工录入        Rows = 用户填的 1~N 行   ExcludeID = 0
//	复制已删除明细  Rows = 复制出的 1 行     ExcludeID = 0（原明细已删除，不在候选内）
//	F-5 逐笔编辑    Rows = 编辑后的 1 行     ExcludeID = 被编辑明细的 ID
type Request struct {
	// Rows 是本次待查的行，按用户所见顺序排列。
	//
	// 录入态是预览页上的 N 行（D-4），编辑态是单行（F-5 复用同一页面的单行形态）。
	// 这些行**尚未入库**，其 ID 字段对本包无意义（录入态为零值，编辑态虽有 ID
	// 但排除自身走的是 ExcludeID 而非它）。
	//
	// 本包不修改 Rows，也不持有它的引用——Result 中回传的是下标而非指针。
	Rows []entity.Expense

	// Candidates 是**在库且未删除**的候选明细（E-1 比对范围）。
	//
	// 由调用方经 store 的「查重候选批量匹配」口径取得（分层 §六 L3 store ④），
	// 通常以 Keys 的输出作为查询条件，故其中绝大多数行都会命中。
	//
	// 「未删除」这一条由 store 的查询保证，**本包不再过滤 DeletedAt**：
	// 若在此重复过滤，比对范围就有了两处实现，两处口径将来必然漂移。
	// TestCandidatesRangeIsCallerDuty 把这个分工写成断言。
	Candidates []entity.Expense

	// ExcludeID 是编辑态要排除的自身明细ID（8.3）；录入态传零值。
	//
	// 排除只作用于 Candidates——Rows 里的行是待查项，不是候选。
	ExcludeID int64
}

// Conflict 是一条冲突记录：某个待查行与哪些明细四要素相同。
//
// 按 D-6「并列展示与之冲突的已入库明细供人工比对」，两个集合都给出**完整明细**
// 而非仅 ID：前端要展示对手方、备注、卡号等字段供人工判断，若只给 ID 就得再回
// 一趟接口。
type Conflict struct {
	// RowIndex 是待查行在 Request.Rows 中的下标。
	//
	// 用下标而非 ID：录入态的行尚未入库、没有 ID，下标是它们此刻唯一的标识，
	// 也正是 D-6 高亮预览页某一行所需要的定位方式。
	RowIndex int

	// InStore 是与该行四要素相同的**已入库**明细（E-2 的「与已入库明细的重复」）。
	//
	// 顺序与 Request.Candidates 中的出现顺序一致，保证同一批输入的输出稳定
	// （前端并列展示的顺序不应每次刷新都变）。
	InStore []entity.Expense

	// InBatch 是**本次录入内部**与该行四要素相同的其他行（E-2 的「本次录入内部重复」）。
	//
	// 不含该行自身。顺序与 Request.Rows 中的出现顺序一致。
	//
	// 与 InStore 分开而不合并：D-6 对二者的展示形态不同（前者并列已入库明细，
	// 后者指向同批的另一行），且「取消勾选」的处置方式也不同。
	InBatch []entity.Expense

	// InBatchIndexes 是 InBatch 各元素在 Request.Rows 中的下标，一一对应。
	//
	// 单独给出是因为预览页要高亮的是「行」：仅有明细内容无法定位到第几行，
	// 而同批内两行四要素相同时它们的内容本就高度相似，无法反查。
	InBatchIndexes []int
}

// HasConflict 报告该行是否存在任何冲突（两类之一即可）。
//
// 提供它是为了让调用方不必写 len(c.InStore) > 0 || len(c.InBatch) > 0——
// 漏掉后半个条件会让「本次录入内部重复」在界面上完全不提示，而这类错误
// 不会有任何报错。
func (c Conflict) HasConflict() bool {
	return len(c.InStore) > 0 || len(c.InBatch) > 0
}

// Result 是判重结果：命中行 → 冲突明细集合的映射（分层 §六 L2 承载原文）。
//
// **只含命中行**：无冲突的行不出现在映射中。这样调用方遍历 Result 即是遍历
// 「需要高亮的行」，不必逐行判空。
type Result struct {
	// Conflicts 以待查行下标为键，值为该行的冲突记录。
	//
	// 只含 HasConflict() 为真的行。
	Conflicts map[int]Conflict
}

// HasAny 报告本次判重是否命中任何冲突。
func (r Result) HasAny() bool { return len(r.Conflicts) > 0 }

// RowIndexes 返回全部命中行的下标，**升序**。
//
// map 遍历顺序随机，直接遍历 Conflicts 会让 D-6 的高亮顺序、审计摘要里的行号
// 每次都不同。需要按顺序处理时用本方法取序，不要自行遍历 map。
func (r Result) RowIndexes() []int {
	out := make([]int, 0, len(r.Conflicts))
	for idx := range r.Conflicts {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

// Check 执行 8.3 判重，返回命中行到冲突集合的映射。
//
// # 算法
//
// 三趟线性扫描，整体 O(n+m)（n = 待查行数，m = 候选数）：
//
//	① 按判重键给 Candidates 建索引（跳过 ExcludeID 那条）
//	② 按判重键给 Rows 建索引（用于内部互查）
//	③ 逐行查两个索引，组装 Conflict
//
// 不用双重循环两两比较（O(n×m)）：一次银行账单导入常有数百行，库内同期候选
// 也是同一量级，平方级比较在预览页上是可感知的等待。
//
// # 不返回 error
//
// 重复不是错误，是待人工裁定的提示（E-2「只提示不阻断」）。本函数没有任何
// 失败路径：键的构造只调用各字段已有的 String()，不做解析、不做算术。
//
// # 入参不被修改
//
// req 按值传入，其中的两个切片本函数只读不写，Result 中回传的明细是**元素副本**
// （entity.Expense 是纯值结构体，无指针字段除 File.Content 外——而 Expense
// 根本没有切片字段）。调用方拿到结果后修改它不会影响原始输入。
func Check(req Request) Result {
	result := Result{Conflicts: make(map[int]Conflict)}
	if len(req.Rows) == 0 {
		// 没有待查行时直接返回空结果。这不是异常：F-5 编辑态若因上层校验
		// 未通过而没有行可查，走的就是这条路径。
		return result
	}

	// ① 候选索引：键 → 该键下的候选明细（保持原有顺序）
	//
	// 这里就把 ExcludeID 过滤掉，而不是等到第 ③ 趟再判——否则每个待查行都要
	// 重复判一次，且容易在某个分支上漏判。
	storeIndex := make(map[string][]entity.Expense, len(req.Candidates))
	for _, candidate := range req.Candidates {
		if req.ExcludeID != 0 && candidate.ID == req.ExcludeID {
			continue // 8.3：编辑态排除被编辑的这笔自身，否则必然自报重复
		}
		key := rowKey(candidate)
		storeIndex[key] = append(storeIndex[key], candidate)
	}

	// ② 本批索引：键 → 该键下的行下标（保持原有顺序）
	batchIndex := make(map[string][]int, len(req.Rows))
	rowKeys := make([]string, len(req.Rows))
	for i, row := range req.Rows {
		key := rowKey(row)
		rowKeys[i] = key
		batchIndex[key] = append(batchIndex[key], i)
	}

	// ③ 逐行组装
	for i := range req.Rows {
		key := rowKeys[i]

		var conflict Conflict
		conflict.RowIndex = i

		// 库内冲突：直接取索引。复制切片而非共享底层数组——调用方若对某一行的
		// InStore 做 append，不应影响另一行的同名切片。
		if matched := storeIndex[key]; len(matched) > 0 {
			conflict.InStore = make([]entity.Expense, len(matched))
			copy(conflict.InStore, matched)
		}

		// 本批内部冲突：同键的其他行，排除自身下标
		for _, j := range batchIndex[key] {
			if j == i {
				continue // 自己不与自己重复
			}
			conflict.InBatch = append(conflict.InBatch, req.Rows[j])
			conflict.InBatchIndexes = append(conflict.InBatchIndexes, j)
		}

		if conflict.HasConflict() {
			result.Conflicts[i] = conflict
		}
	}
	return result
}

// Key 是四要素的规范键，即 store 做「查重候选批量匹配」时的查询元组。
//
// 四个字段与 8.3 的四要素一一对应，均为**可比较**类型，故 Key 本身可比较、
// 可作 map 键——这正是 currency.Code 与 calendar.Date 刻意保持可比较、而
// decimal.Decimal 刻意不可比较的分工：前三者是标称值，金额不是（100.50 与
// 100.5 内部表示不同而数值相同）。金额因此以**规范文本**形态放在这里。
type Key struct {
	// OwnerUserID 归属用户ID。跨用户绝不比对（前提 3）。
	OwnerUserID int64

	// SpendDate 支出日期的定长文本（2006-01-02），零值为空串。
	//
	// 用文本而非 calendar.Date：store 侧要把它拼进 SQL 的元组条件，
	// 而 calendar.Date 落库形态本就是这个文本（其 Value() 的返回值）。
	SpendDate string

	// Amount 支出金额的规范文本，剥掉尾随零，如 100.5、-33.34、0。
	//
	// **不是**定点文本：定点文本要先定位数，而 8.3 的金额是**原币**金额，
	// 位数随支出币种而定——KWD 是 3 位，按本位币存储的 2 位会直接报错。
	// 规范文本对任意位数都成立，且已实测满足「文本相等 ⟺ 数值相等」。
	Amount string

	// Currency 支出币种代码，如 CNY；零值为空串。
	Currency string
}

// String 返回该键的字符串形态，即 map 索引所用的键。
//
// 顺序与 8.3 列出四要素的顺序一致（归属用户 → 日期 → 金额 → 币种），
// 便于排错时把日志里的键与规则原文对上。
func (k Key) String() string {
	var b strings.Builder
	// 预估容量：ID 最长 19 位 + 日期 10 + 金额 ~20 + 币种 3 + 3 个分隔符
	b.Grow(64)
	// 用 strconv 而非 fmt：判重键在一次导入中会被构造上千次，fmt 的反射路径
	// 在此是纯粹的浪费；且 fmt 对负数与大整数的行为随动词而变，而键必须逐字节确定。
	b.WriteString(strconv.FormatInt(k.OwnerUserID, 10))
	b.WriteString(keySep)
	b.WriteString(k.SpendDate)
	b.WriteString(keySep)
	b.WriteString(k.Amount)
	b.WriteString(keySep)
	b.WriteString(k.Currency)
	return b.String()
}

// KeyOf 由一条明细算出它的四要素键。
//
// 导出它是为了让 store 与 service 层能用同一套口径构造查询条件——四要素判定
// 若在 store 侧另写一遍，两处口径将来必然漂移（一处改了金额的规范化方式而另一处
// 没改，症状是「预览页提示重复但查不出那条明细」）。
func KeyOf(e entity.Expense) Key {
	return Key{
		OwnerUserID: e.OwnerUserID,
		SpendDate:   e.SpendDate.String(),
		// decimal.String() 是规范形式：Parse 与 FromUnits 都在返回前剥掉尾随零，
		// 故 a.Equal(b) ⟺ a.String() == b.String()（keyer_test.go 穷举验证）。
		Amount:   e.Amount.String(),
		Currency: e.Currency.String(),
	}
}

// Keys 返回待查行去重后的四要素键集合，供调用方一次性向 store 取候选。
//
// 对应分层 §六 L3 store ④「按『(日期, 金额, 币种)』元组集合**一次取回**在库
// 未删除候选」——一次导入几百行若逐行查库就是几百次往返，这是把它压成一次的
// 前置条件。
//
// 顺序与 rows 中**首次出现**的顺序一致，重复键只保留一个。保持确定顺序是为了
// 让生成的 SQL 参数顺序稳定，便于日志比对与查询计划缓存。
func Keys(rows []entity.Expense) []Key {
	out := make([]Key, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := KeyOf(row)
		text := key.String()
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, key)
	}
	return out
}

// rowKey 是 KeyOf(e).String() 的简写，包内热路径使用。
func rowKey(e entity.Expense) string { return KeyOf(e).String() }
