package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件验证查询条件类型（承载 ④）的归一化与守卫逻辑。这些是本包**唯一有可执行
// 行为**的部分（其余是接口声明），故也是唯一能出运行时缺陷的部分。

// TestPageNormalize 验证分页归一化。
//
// 重点是「非法取值一律按最接近的合法值处理，不报错」这条取向：页码越界（删到最后
// 一页只剩 3 条）是正常操作，做成错误会让界面报错。
func TestPageNormalize(t *testing.T) {
	cases := []struct {
		in         Page
		wantNum    int
		wantSize   int
		wantOffset int
		desc       string
	}{
		{Page{}, 1, DefaultPageSize, 0, "零值：第 1 页 + 默认页大小"},
		{Page{Number: 0, Size: 0}, 1, DefaultPageSize, 0, "显式零值同上"},
		{Page{Number: -5, Size: -1}, 1, DefaultPageSize, 0, "负值归一到合法下界"},
		{Page{Number: 3, Size: 10}, 3, 10, 20, "正常取值原样保留"},
		{Page{Number: 1, Size: MaxPageSize}, 1, MaxPageSize, 0, "恰好上限"},
		{Page{Number: 1, Size: MaxPageSize + 1}, 1, MaxPageSize, 0, "超上限截到上限"},
		{Page{Number: 1, Size: 1000000}, 1, MaxPageSize, 0, "巨大页大小被截（防绕开导出审计）"},
		{Page{Number: 9999, Size: 20}, 9999, 20, 199960, "页码越界是合法请求，结果为空列表"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := c.in.Normalize()
			if got.Number != c.wantNum || got.Size != c.wantSize {
				t.Fatalf("Normalize(%+v) = %+v, 期望 {Number:%d Size:%d}",
					c.in, got, c.wantNum, c.wantSize)
			}
			if off := c.in.Offset(); off != c.wantOffset {
				t.Fatalf("Offset(%+v) = %d, 期望 %d", c.in, off, c.wantOffset)
			}
			if lim := c.in.Limit(); lim != c.wantSize {
				t.Fatalf("Limit(%+v) = %d, 期望 %d", c.in, lim, c.wantSize)
			}
		})
	}
}

// TestPageSizeCapBlocksExportBypass 验证页大小上限确实存在且不宽松。
//
// 上限的作用不是性能，而是防止列表接口被当成免审计的全量导出通道：
// K-3 与 F-7 是唯一记「数据导出」审计的读操作（B-4 边界①）。
func TestPageSizeCapBlocksExportBypass(t *testing.T) {
	if MaxPageSize <= 0 {
		t.Fatal("MaxPageSize 必须为正，否则分页无上限")
	}
	if MaxPageSize > 1000 {
		t.Fatalf("MaxPageSize = %d 过大：列表接口会成为绕开「数据导出」审计的全量取数通道",
			MaxPageSize)
	}
	if DefaultPageSize > MaxPageSize {
		t.Fatalf("DefaultPageSize(%d) 不得大于 MaxPageSize(%d)", DefaultPageSize, MaxPageSize)
	}
}

// TestExpenseSortNormalizeAlwaysHasUniqueTiebreaker 验证排序**恒有唯一兜底键**。
//
// 这是分页正确性的前提：按支出日期排序时同日多笔之间没有确定顺序，引擎不保证稳定
// 顺序，于是第 1 页与第 2 页可能含同一条明细，另一条从未出现。这个缺陷在数据量小时
// 几乎不可见，上线后表现为「某笔明细在列表里找不到，但筛选能筛出来」。
func TestExpenseSortNormalizeAlwaysHasUniqueTiebreaker(t *testing.T) {
	inputs := []ExpenseSort{
		{},                                          //零值
		{Field: SortBySpendDate},                    //日期升序
		{Field: SortBySpendDate, Desc: true},        //日期降序
		{Field: SortByAmount},                       //金额
		{Field: SortByBaseAmount, Desc: true},       //本位币金额
		{Field: SortByCreatedAt},                    //创建时间（注意它不唯一）
		{Field: ExpenseSortField("bogus")},          //非法字段
		{Field: ExpenseSortField("id; DROP TABLE")}, //注入形态
	}
	for _, in := range inputs {
		keys := in.Normalize()
		if len(keys) == 0 {
			t.Fatalf("Normalize(%+v) 返回空排序键，L4 将无 ORDER BY 可拼", in)
		}
		// 每个键都必须是白名单内的合法字段（否则 L4 拼进 ORDER BY 即注入）
		for _, k := range keys {
			if !k.Field.Valid() {
				t.Fatalf("Normalize(%+v) 产出非法字段 %q，L4 拼进 ORDER BY 即注入", in, k.Field)
			}
		}
		// 末键必须是唯一键（ID），否则分页可能重复或漏行
		last := keys[len(keys)-1]
		if last.Field != SortByID {
			t.Fatalf("Normalize(%+v) 的末键为 %q，必须是 %q（唯一兜底键）；"+
				"否则同值行的顺序不确定，分页会重复或漏行", in, last.Field, SortByID)
		}
	}
}

// TestExpenseSortNormalizeDefaults 验证默认排序与兜底键方向。
func TestExpenseSortNormalizeDefaults(t *testing.T) {
	// 未指定 → 支出日期降序（F-3 明细列表，最近的在最前）
	keys := ExpenseSort{}.Normalize()
	if keys[0].Field != SortBySpendDate || !keys[0].Desc {
		t.Fatalf("零值排序 = %+v, 期望按支出日期降序", keys[0])
	}

	// 兜底键方向跟随主键，否则「日期降序」下同日多笔会变成旧的在前
	for _, desc := range []bool{true, false} {
		keys := ExpenseSort{Field: SortBySpendDate, Desc: desc}.Normalize()
		if len(keys) != 2 {
			t.Fatalf("期望 2 个排序键，实际 %d", len(keys))
		}
		if keys[1].Desc != desc {
			t.Fatalf("主键 Desc=%v 时兜底键 Desc=%v，方向应一致", desc, keys[1].Desc)
		}
	}

	// 主键已是唯一键时不重复追加
	keys = ExpenseSort{Field: SortByID, Desc: true}.Normalize()
	if len(keys) != 1 {
		t.Fatalf("按 ID 排序时应只有 1 个键，实际 %d 个：%+v", len(keys), keys)
	}
}

// TestExpenseSortFieldWhitelist 验证排序字段是闭集，且非法值一律被拒。
//
// 排序字段无法用 SQL 占位符（? 只能占值不能占列名），白名单是 ORDER BY 注入的
// 唯一防线。
func TestExpenseSortFieldWhitelist(t *testing.T) {
	for _, f := range ExpenseSortFields() {
		if !f.Valid() {
			t.Fatalf("ExpenseSortFields 返回的 %q 未通过 Valid，两处口径不一致", f)
		}
		if f.String() != string(f) {
			t.Fatalf("String() 与字面值不一致: %q vs %q", f.String(), string(f))
		}
		// 列名必须是安全标识符——直接拼进 ORDER BY 不能带任何特殊字符
		for _, r := range f.String() {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				t.Fatalf("排序字段 %q 含非安全字符 %q，不可直接拼进 ORDER BY", f, r)
			}
		}
	}

	invalid := []string{
		"", "bogus", "amount; DROP TABLE expense", "amount --", "AMOUNT",
		"amount desc", "1", "spend_date, amount", "owner_user_id",
	}
	for _, s := range invalid {
		if ExpenseSortField(s).Valid() {
			t.Fatalf("非法排序字段 %q 通过了 Valid，存在 ORDER BY 注入风险", s)
		}
	}
}

// TestExpenseSortFieldsStableOrder 验证清单顺序稳定且不泄漏内部切片。
//
// 顺序供 L6 下发给前端做排序下拉，每次刷新换一次顺序是可见缺陷。
func TestExpenseSortFieldsStableOrder(t *testing.T) {
	first, second := ExpenseSortFields(), ExpenseSortFields()
	if len(first) != len(second) {
		t.Fatal("两次调用长度不一致")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("第 %d 项两次调用不一致: %q vs %q", i, first[i], second[i])
		}
	}
	// 改动返回值不得影响后续调用（必须是副本）
	if len(first) > 0 {
		first[0] = ExpenseSortField("tampered")
		if ExpenseSortFields()[0] == ExpenseSortField("tampered") {
			t.Fatal("ExpenseSortFields 返回了内部切片，调用方可篡改白名单")
		}
	}
}

// TestMonthlyModeValid 验证口径零值**不合法**——调用方必须显式表态。
//
// 零值合法会让「忘了设口径」默认落到某一个口径上，而 J-1 的两个口径结果差异巨大
// （未摊分把整笔计入支出月，摊分把它分摊到 N 个月），静默取默认值等于给出错误报表。
func TestMonthlyModeValid(t *testing.T) {
	var zero MonthlyMode
	if zero.Valid() {
		t.Fatal("MonthlyMode 零值不应合法：调用方必须显式选择口径，否则报表口径由默认值决定")
	}
	if !MonthlyModeUnamortized.Valid() || !MonthlyModeAmortized.Valid() {
		t.Fatal("两个具名口径都应合法")
	}
	if MonthlyMode(99).Valid() {
		t.Fatal("未知取值不应合法")
	}
	// String 对每个取值都要有确定输出（供日志排错）
	if MonthlyModeUnamortized.String() == MonthlyModeAmortized.String() {
		t.Fatal("两个口径的 String 不应相同")
	}
	if zero.String() == "" || MonthlyMode(99).String() == "" {
		t.Fatal("非法取值的 String 也应有确定输出，便于日志排错")
	}
}

// TestMonthlyQueryHasNoShowDeletedField 验证 MonthlyQuery **没有** ShowDeleted 字段。
//
// 终版 J 域末注：「已软删除的明细不参与 J 域任何统计」。这条在类型层面就不可绕过
// ——没有开关可打开。若有人加了这个字段，本用例失败。
//
// 用反射按字段名检查而非 AST：字段可能被重命名，但只要语义是「显示已删除」，
// 名字里大概会有 Deleted。
func TestMonthlyQueryHasNoShowDeletedField(t *testing.T) {
	assertNoFieldContaining(t, MonthlyQuery{}, "delet",
		"J 域一律排除已软删除明细（终版 J 域末注），不得提供开关")
}

// TestExpenseFilterHasNoOwnerField 验证 ExpenseFilter **没有**归属用户字段。
//
// 归属必须是方法签名的参数（红线 1）：结构体字段漏赋值不会被编译器发现，
// 而签名漏传即编译错误。若归属能从结构体传入，就存在「构造了 filter 但忘了设
// 归属」这条路径，后果是跨用户数据泄漏（前提 3）。
func TestExpenseFilterHasNoOwnerField(t *testing.T) {
	assertNoFieldContaining(t, ExpenseFilter{}, "owner",
		"归属必须走方法签名（红线 1），放进筛选结构体则漏赋值不报错，会跨用户泄漏")
	assertNoFieldContaining(t, AuditFilter{}, "owner",
		"归属必须走方法签名（红线 1）")
	assertNoFieldContaining(t, MonthlyQuery{}, "owner",
		"归属必须走方法签名（红线 1）")
}

// TestFilterZeroValueMeansDefaultSafe 验证零值筛选的语义是「不筛 + 不显示已删除」。
//
// ShowDeleted 零值必须是 false：反过来会让任何忘记设置该字段的调用点默认把已删除
// 明细混进列表与合计，而 F-6 的「列表所见 = 合计所指」正是批次撤销前核对的依据。
func TestFilterZeroValueMeansDefaultSafe(t *testing.T) {
	var f ExpenseFilter
	if f.ShowDeleted {
		t.Fatal("ExpenseFilter 零值的 ShowDeleted 必须为 false（F-4「默认不显示已删除」）")
	}
	// 金额区间靠配对标志区分「不限」与「限定为 0」
	if f.AmountFromSet || f.AmountToSet {
		t.Fatal("零值筛选不应带金额区间")
	}
	// 零值的日期与月份都应是「未指定」
	if !f.DateFrom.IsZero() || !f.DateTo.IsZero() {
		t.Fatal("零值筛选不应带日期区间")
	}
	if !f.AmortizeCovers.IsZero() {
		t.Fatal("零值筛选不应带摊分穿刺月")
	}
}

// TestAmountRangeNeedsExplicitFlag 验证金额区间必须靠配对标志表达。
//
// decimal.Decimal 的零值就是 0，而 0 是合法的筛选边界（「金额 ≥ 0」用于筛出
// 非退款行）。靠零值无法区分「不限」与「限定为 0」，故必须有布尔标志。
func TestAmountRangeNeedsExplicitFlag(t *testing.T) {
	// 「限定为 ≥ 0」：值是零值但标志为真
	f := ExpenseFilter{AmountFromSet: true}
	if !f.AmountFrom.IsZero() {
		t.Fatal("前提不成立：未赋值的 AmountFrom 应为零值")
	}
	if !f.AmountFromSet {
		t.Fatal("AmountFromSet 应为真，用于表达「限定为 ≥ 0」这个合法条件")
	}
	// 反例：不带标志时，同样的零值表示「不限」
	var unset ExpenseFilter
	if unset.AmountFromSet {
		t.Fatal("未设标志时应表示不限")
	}
}

// TestMonthRangeInvariant 验证 MonthlyQuery 依赖的区间性质确实由 calendar 保证。
//
// MonthlyQuery.Range 的注释声称「非零值必然满足起始月 ≤ 结束月」，故 L4 不必再校验
// 区间方向。这条性质属于 calendar，本用例确认它成立——若不成立，L4 就必须自己校验。
func TestMonthRangeInvariant(t *testing.T) {
	start := calendar.MustParseMonth("2026-03")
	end := calendar.MustParseMonth("2026-01")
	if _, err := calendar.NewMonthRangeBetween(start, end); err == nil {
		t.Fatal("calendar.NewMonthRangeBetween 接受了倒置区间，" +
			"则 MonthlyQuery.Range 的「方向已保证」不成立，L4 必须自行校验区间方向")
	}
	// 正序可用，且穿刺判据可用
	r, err := calendar.NewMonthRangeBetween(end, start)
	if err != nil {
		t.Fatalf("正序区间构造失败: %v", err)
	}
	if !r.Contains(calendar.MustParseMonth("2026-02")) {
		t.Fatal("区间应包含中间月，摊分穿刺依赖这条")
	}
}

// TestCurrencyTotalUsesComparableCode 验证币种可作分组键。
//
// ExpenseSummary.ByCurrency 与 MonthlyBucket 都按币种分组，L4 实现会用
// map[currency.Code] 聚合。currency.Code 必须可比较——若它变成不可比较类型
// （像 decimal.Decimal 那样），这些聚合代码会编译失败。
func TestCurrencyTotalUsesComparableCode(t *testing.T) {
	m := map[currency.Code]int64{}
	m[currency.MustParse("CNY")] = 1
	m[currency.MustParse("JPY")] = 2
	if len(m) != 2 {
		t.Fatal("currency.Code 作 map 键异常")
	}
	if currency.MustParse("CNY") != currency.MustParse("CNY") {
		t.Fatal("currency.Code 应支持 == 比较，聚合分组依赖这条")
	}
}

// assertNoFieldContaining 断言结构体没有名字包含给定子串（忽略大小写）的字段。
func assertNoFieldContaining(t *testing.T, v any, substr, reason string) {
	t.Helper()
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if strings.Contains(strings.ToLower(name), strings.ToLower(substr)) {
			t.Fatalf("%s 含字段 %s（匹配 %q）：%s", rt.Name(), name, substr, reason)
		}
	}
}
