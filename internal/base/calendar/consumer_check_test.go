package calendar

import (
	"errors"
	"os"
	"testing"
	"time"
)

// TestTimezoneNoDrift 是本包存在理由的可执行断言（方案 V1、分层设计阶段 1 的完成标志）。
//
// 前提 7：支出日期以账单字面值为准，不做时区换算。若本包的任何环节掉回带时区的
// 时间类型，同一字面值在不同时区下的「所属月」会整体偏移一天，导致摊分起始月、
// J-1 未摊分口径归月、I-4 日汇率取数三处同时静默出错。
//
// 本测试在 UTC、UTC+8、UTC+14、UTC-11 四个进程时区下，把「解析 → 归月 → 派生区间
// → 穿刺 → 文本往返」整条链路跑一遍，要求结果逐字节相同。
func TestTimezoneNoDrift(t *testing.T) {
	// 取一批容易暴露时区偏移的边界字面值：月首、月末、年末、闰日。
	// 时区偏移一天时，这些值的所属月会立刻变化。
	samples := []string{
		"2026-01-01", // 月首：西向偏移会退到上月
		"2026-01-31", // 月末：东向偏移会进到下月
		"2026-12-31", // 年末：偏移会跨年
		"2027-01-01", // 年首
		"2024-02-29", // 闰日
		"2024-03-01", // 闰月次日
		"2026-06-15", // 月中对照
	}

	type snapshot struct {
		date       string
		month      string
		rangeText  string
		monthsText string
		hitPrev    bool
		hitStart   bool
		hitEnd     bool
		hitNext    bool
		firstDate  string
		lastDate   string
		sqlValue   any
	}

	// run 在当前进程时区下跑完整链路并快照结果。
	run := func(t *testing.T) []snapshot {
		out := make([]snapshot, 0, len(samples))
		for _, s := range samples {
			d, err := ParseDate(s)
			if err != nil {
				t.Fatalf("ParseDate(%q) 报错: %v", s, err)
			}
			m := d.ToMonth()
			// 摊分 3 个月，按不变式 1b 派生区间
			r, err := NewMonthRange(m, 3)
			if err != nil {
				t.Fatalf("NewMonthRange(%s) 报错: %v", m, err)
			}
			prev, err := m.AddMonths(-1)
			if err != nil {
				t.Fatalf("AddMonths(-1) 报错: %v", err)
			}
			next, err := r.End().AddMonths(1)
			if err != nil {
				t.Fatalf("AddMonths(1) 报错: %v", err)
			}
			months := ""
			for _, mm := range r.Months() {
				months += mm.String() + ","
			}
			v, err := d.Value()
			if err != nil {
				t.Fatalf("Value 报错: %v", err)
			}
			out = append(out, snapshot{
				date:       d.String(),
				month:      m.String(),
				rangeText:  r.String(),
				monthsText: months,
				hitPrev:    r.Contains(prev),
				hitStart:   r.Contains(r.Start()),
				hitEnd:     r.Contains(r.End()),
				hitNext:    r.Contains(next),
				firstDate:  m.FirstDate().String(),
				lastDate:   m.LastDate().String(),
				sqlValue:   v,
			})
		}
		return out
	}

	zones := []struct {
		name   string
		loc    *time.Location
		offset int
	}{
		{"UTC", time.UTC, 0},
		{"Asia/Shanghai(+8)", time.FixedZone("CST", 8*3600), 8},
		{"Pacific/Kiritimati(+14)", time.FixedZone("LINT", 14*3600), 14},
		{"Pacific/Midway(-11)", time.FixedZone("SST", -11*3600), -11},
	}

	// 以 UTC 下的结果为基准
	original := time.Local
	defer func() { time.Local = original }()

	var baseline []snapshot
	for i, z := range zones {
		// 直接改 time.Local：进程内一切「本地时间」语义随之变化，
		// 若本包内部有任何一处掉回本地时区，这里必然暴露。
		time.Local = z.loc
		got := run(t)
		if i == 0 {
			baseline = got
			continue
		}
		if len(got) != len(baseline) {
			t.Fatalf("%s 下结果条数变化", z.name)
		}
		for j := range got {
			if got[j] != baseline[j] {
				t.Errorf("时区 %s 下第 %d 条（字面值 %s）结果漂移:\n  实际 %+v\n  基准 %+v",
					z.name, j, samples[j], got[j], baseline[j])
			}
		}
	}

	// 附加断言：基准结果本身必须与字面值一致（防止四个时区「一致地错」）
	wantMonths := []string{"2026-01", "2026-01", "2026-12", "2027-01", "2024-02", "2024-03", "2026-06"}
	for i, want := range wantMonths {
		if baseline[i].date != samples[i] {
			t.Errorf("第 %d 条日期文本 = %s, 期望原样保持 %s", i, baseline[i].date, samples[i])
		}
		if baseline[i].month != want {
			t.Errorf("第 %d 条（%s）所属月 = %s, 期望 %s", i, samples[i], baseline[i].month, want)
		}
	}
}

// TestTimezoneEnvNoDrift 用 TZ 环境变量再验一次：即使进程启动时区不同，
// 同一字面值的解析与归月结果也不变。
//
// 与上一个测试互补：上一个改的是 time.Local 变量，这一个走 os.Setenv + LoadLocation，
// 覆盖「部署环境 TZ 不同」这一真实场景。
func TestTimezoneEnvNoDrift(t *testing.T) {
	originalTZ, hadTZ := os.LookupEnv("TZ")
	originalLocal := time.Local
	defer func() {
		if hadTZ {
			os.Setenv("TZ", originalTZ)
		} else {
			os.Unsetenv("TZ")
		}
		time.Local = originalLocal
	}()

	const literal = "2026-01-01"
	const wantMonth = "2026-01"

	for _, tz := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Pacific/Kiritimati"} {
		os.Setenv("TZ", tz)
		loc, err := time.LoadLocation(tz)
		if err != nil {
			// 容器内可能缺 tzdata，此时跳过该时区而不是让测试失败
			t.Logf("跳过时区 %s（系统无该时区数据）: %v", tz, err)
			continue
		}
		time.Local = loc

		d, err := ParseDate(literal)
		if err != nil {
			t.Fatalf("TZ=%s 下 ParseDate 报错: %v", tz, err)
		}
		if d.String() != literal {
			t.Errorf("TZ=%s 下日期文本 = %s, 期望 %s", tz, d, literal)
		}
		if got := d.ToMonth().String(); got != wantMonth {
			t.Errorf("TZ=%s 下所属月 = %s, 期望 %s", tz, got, wantMonth)
		}
		// 落库形态也不得随时区变化
		v, err := d.Value()
		if err != nil {
			t.Fatalf("TZ=%s 下 Value 报错: %v", tz, err)
		}
		if v != literal {
			t.Errorf("TZ=%s 下落库值 = %v, 期望 %s", tz, v, literal)
		}
	}
}

// TestConsumerAmortizeDerivation 走查 rule/amortize 的用法（方案 §三 #2/#3/#4）：
// 起始月 = 支出日期所属月；结束月 = 起始月 + 月数 − 1（不变式 1b）。
func TestConsumerAmortizeDerivation(t *testing.T) {
	// 模拟一笔 2026-01-15 的支出，摊分 3 个月
	expenseDate := MustParseDate("2026-01-15")
	const amortizeMonths = 3

	start := expenseDate.ToMonth()
	r, err := NewMonthRange(start, amortizeMonths)
	if err != nil {
		t.Fatalf("派生摊分区间报错: %v", err)
	}

	if start.String() != "2026-01" {
		t.Errorf("摊分起始月 = %s, 期望 2026-01", start)
	}
	if r.End().String() != "2026-03" {
		t.Errorf("摊分结束月 = %s, 期望 2026-03", r.End())
	}
	// H-4 覆盖月份区间：份数必须与摊分月数一致，供份额均分
	if r.Len() != amortizeMonths {
		t.Errorf("覆盖月数 = %d, 期望 %d", r.Len(), amortizeMonths)
	}
	if len(r.Months()) != amortizeMonths {
		t.Errorf("覆盖月份列表长度 = %d, 期望 %d", len(r.Months()), amortizeMonths)
	}

	// 默认不摊分（摊分月数 = 1，H-1）：起止月同月
	single, err := NewMonthRange(start, 1)
	if err != nil {
		t.Fatalf("不摊分区间报错: %v", err)
	}
	if !single.Start().Equal(single.End()) {
		t.Errorf("不摊分时起止月应相同, 实际 %s", single)
	}
}

// TestConsumerAmortizePierce 走查 store 的摊分区间穿刺（方案 §三 #5）：
// 「起始月 ≤ M 且 结束月 ≥ M」。同时验证 SQL 侧的字符串比较与本包判定等价。
func TestConsumerAmortizePierce(t *testing.T) {
	// 三笔明细的摊分区间
	type row struct {
		name  string
		start string
		count int
	}
	rows := []row{
		{"甲：2026-01 起摊 3 个月", "2026-01", 3},
		{"乙：2026-02 起不摊分", "2026-02", 1},
		{"丙：2025-11 起摊 6 个月", "2025-11", 6},
	}

	// 查询 2026-02 这个月，期望命中甲（区间内）、乙（自身）、丙（区间内）
	target := MustParseMonth("2026-02")
	for _, r := range rows {
		mr, err := NewMonthRange(MustParseMonth(r.start), r.count)
		if err != nil {
			t.Fatalf("%s 构造区间报错: %v", r.name, err)
		}
		if !mr.Contains(target) {
			t.Errorf("%s 应命中 %s, 实际未命中（区间 %s）", r.name, target, mr)
		}

		// SQL 侧等价性：起始月 <= M AND 结束月 >= M 的字符串比较必须同结论
		byString := mr.Start().String() <= target.String() && mr.End().String() >= target.String()
		if byString != mr.Contains(target) {
			t.Errorf("%s：字符串比较判定 %v 与 Contains %v 不一致", r.name, byString, mr.Contains(target))
		}
	}

	// 查询 2026-05：甲（01~03）与乙（02）应落空，丙（2025-11~2026-04）也应落空
	miss := MustParseMonth("2026-05")
	for _, r := range rows {
		mr, _ := NewMonthRange(MustParseMonth(r.start), r.count)
		if mr.Contains(miss) {
			t.Errorf("%s 不应命中 %s（区间 %s）", r.name, miss, mr)
		}
	}
}

// TestConsumerUnamortizedGrouping 走查 store 的 J-1 未摊分口径归月（方案 §三 #7）：
// 把「某月」翻译成「支出日期 BETWEEN 月首 AND 月末」。
func TestConsumerUnamortizedGrouping(t *testing.T) {
	target := MustParseMonth("2026-02")
	lo, hi := target.FirstDate(), target.LastDate()

	if lo.String() != "2026-02-01" || hi.String() != "2026-02-28" {
		t.Fatalf("2026-02 的区间 = [%s, %s], 期望 [2026-02-01, 2026-02-28]", lo, hi)
	}

	cases := []struct {
		date string
		in   bool
		desc string
	}{
		{"2026-01-31", false, "上月末"},
		{"2026-02-01", true, "本月首（闭合）"},
		{"2026-02-15", true, "本月中"},
		{"2026-02-28", true, "本月末（闭合）"},
		{"2026-03-01", false, "下月首"},
	}
	for _, c := range cases {
		d := MustParseDate(c.date)
		// BETWEEN 是闭区间
		got := !d.Before(lo) && !d.After(hi)
		if got != c.in {
			t.Errorf("%s [%s] 落在 2026-02 = %v, 期望 %v", c.date, c.desc, got, c.in)
		}
		// 与「所属月」判定必须一致——两条路径同结论，否则统计口径会分叉
		if byMonth := d.ToMonth().Equal(target); byMonth != c.in {
			t.Errorf("%s [%s]：区间判定 %v 与所属月判定 %v 不一致", c.date, c.desc, got, byMonth)
		}
		// 字符串比较（SQL 侧形态）同样必须一致
		byString := d.String() >= lo.String() && d.String() <= hi.String()
		if byString != c.in {
			t.Errorf("%s [%s]：字符串区间判定 %v, 期望 %v", c.date, c.desc, byString, c.in)
		}
	}
}

// TestConsumerDateRangeFilter 走查 store 的 F-4 日期区间筛选（方案 §三 #6）：
// 端点可选，零值表示「未指定」。
func TestConsumerDateRangeFilter(t *testing.T) {
	// match 模拟 store 的筛选判定：零值端点视为无约束
	match := func(d, from, to Date) bool {
		if !from.IsZero() && d.Before(from) {
			return false
		}
		if !to.IsZero() && d.After(to) {
			return false
		}
		return true
	}

	d := MustParseDate("2026-02-15")
	var unset Date

	if !match(d, unset, unset) {
		t.Error("两端未指定时应全部命中")
	}
	if !match(d, MustParseDate("2026-01-01"), unset) {
		t.Error("只给起点且满足时应命中")
	}
	if match(d, MustParseDate("2026-03-01"), unset) {
		t.Error("只给起点且早于起点时应落空")
	}
	if !match(d, unset, MustParseDate("2026-12-31")) {
		t.Error("只给终点且满足时应命中")
	}
	if match(d, unset, MustParseDate("2026-01-31")) {
		t.Error("只给终点且晚于终点时应落空")
	}
	if !match(d, MustParseDate("2026-02-15"), MustParseDate("2026-02-15")) {
		t.Error("起止均为当天时应命中（闭区间）")
	}
}

// TestConsumerStatTrend 走查 service/stat 的 J-3 趋势与环比同比（方案 §三 #11）。
func TestConsumerStatTrend(t *testing.T) {
	current := MustParseMonth("2026-03")

	prev, err := current.AddMonths(-1)
	if err != nil {
		t.Fatalf("环比取上月报错: %v", err)
	}
	if prev.String() != "2026-02" {
		t.Errorf("环比上月 = %s, 期望 2026-02", prev)
	}

	yoy, err := current.AddMonths(-12)
	if err != nil {
		t.Fatalf("同比取去年同月报错: %v", err)
	}
	if yoy.String() != "2025-03" {
		t.Errorf("同比去年同月 = %s, 期望 2025-03", yoy)
	}

	// 多月趋势：近 6 个月按正序
	const window = 6
	first, err := current.AddMonths(-(window - 1))
	if err != nil {
		t.Fatalf("趋势起点报错: %v", err)
	}
	trend, err := NewMonthRangeBetween(first, current)
	if err != nil {
		t.Fatalf("趋势区间报错: %v", err)
	}
	if trend.Len() != window {
		t.Errorf("趋势窗口 = %d 个月, 期望 %d", trend.Len(), window)
	}
	want := []string{"2025-10", "2025-11", "2025-12", "2026-01", "2026-02", "2026-03"}
	for i, m := range trend.Months() {
		if m.String() != want[i] {
			t.Errorf("趋势第 %d 月 = %s, 期望 %s", i, m, want[i])
		}
	}
	// 窗口跨年，月数差应等于窗口减一
	if current.MonthsSince(first) != window-1 {
		t.Errorf("窗口首末月数差 = %d, 期望 %d", current.MonthsSince(first), window-1)
	}
}

// TestConsumerStatFutureSplit 走查 service/stat 的 J-6 分段（方案 §三 #12）：
// 摊分口径下把「已发生（≤ 当前月）」与「未来待摊」分开。
func TestConsumerStatFutureSplit(t *testing.T) {
	// 一笔 2026-02 起摊 6 个月的支出：2026-02 ~ 2026-07
	r, err := NewMonthRange(MustParseMonth("2026-02"), 6)
	if err != nil {
		t.Fatalf("构造区间报错: %v", err)
	}
	// 以 2026-04 为「当前月」（时区决策在 service/stat，见方案 §九 C-1）
	current := MustParseMonth("2026-04")

	var happened, future []Month
	for _, m := range r.Months() {
		if m.After(current) {
			future = append(future, m)
		} else {
			happened = append(happened, m)
		}
	}

	// 已发生：02、03、04（当前月闭合计入）
	if len(happened) != 3 {
		t.Errorf("已发生月数 = %d, 期望 3", len(happened))
	}
	// 未来待摊：05、06、07
	if len(future) != 3 {
		t.Errorf("未来待摊月数 = %d, 期望 3", len(future))
	}
	if len(happened) > 0 && happened[len(happened)-1].String() != "2026-04" {
		t.Errorf("已发生末月 = %s, 期望 2026-04（当前月计入）", happened[len(happened)-1])
	}
	if len(future) > 0 && future[0].String() != "2026-05" {
		t.Errorf("未来首月 = %s, 期望 2026-05", future[0])
	}
	// 两段之和必须等于总覆盖月数，不重不漏
	if len(happened)+len(future) != r.Len() {
		t.Errorf("分段之和 %d != 总月数 %d", len(happened)+len(future), r.Len())
	}
}

// TestConsumerFxRateDateKey 走查 service/fx 的 I-4 日汇率取数（方案 §三 #13）：
// 外部汇率源以 2006-01-02 形态的日期为键。
func TestConsumerFxRateDateKey(t *testing.T) {
	d := MustParseDate("2026-09-06")
	key := d.String()
	if key != "2026-09-06" {
		t.Errorf("汇率取数日期键 = %q, 期望 \"2026-09-06\"", key)
	}
	if len(key) != dateLen {
		t.Errorf("日期键长度 = %d, 期望恒为 %d", len(key), dateLen)
	}
	// 键必须能解析回同一日期（供缓存键与回读比对）
	back, err := ParseDate(key)
	if err != nil || !back.Equal(d) {
		t.Errorf("日期键 %q 无法解析回原值: %v", key, err)
	}
}

// TestConsumerEntityFieldTypes 走查 entity 的字段承载（方案 §三 #1）：
// Expense 的支出日期用 Date、摊分起止月用 Month，均为可比较的值类型。
func TestConsumerEntityFieldTypes(t *testing.T) {
	// 模拟 entity.Expense 的三个日期类字段
	type expense struct {
		ExpenseDate   Date  // 支出日期（用户可编辑 8 项之一）
		AmortizeStart Month // 摊分起始月（只读派生）
		AmortizeEnd   Month // 摊分结束月（只读派生）
	}

	e := expense{ExpenseDate: MustParseDate("2026-01-15")}
	// 只读派生字段由规则层覆盖写入，不接受上游传值（前提 6 同构口径）
	r, err := NewMonthRange(e.ExpenseDate.ToMonth(), 3)
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	e.AmortizeStart, e.AmortizeEnd = r.Start(), r.End()

	if e.AmortizeStart.String() != "2026-01" || e.AmortizeEnd.String() != "2026-03" {
		t.Errorf("派生结果 = %s~%s, 期望 2026-01~2026-03", e.AmortizeStart, e.AmortizeEnd)
	}

	// 值类型可直接用 == 比较（结构体可作 map 键、可整体比对）
	same := expense{
		ExpenseDate:   MustParseDate("2026-01-15"),
		AmortizeStart: MustParseMonth("2026-01"),
		AmortizeEnd:   MustParseMonth("2026-03"),
	}
	if e != same {
		t.Error("相同内容的 expense 应可直接用 == 判等")
	}

	// 零值可用（entity 结构体零值不 panic，供 store 扫描前初始化）
	var empty expense
	if !empty.ExpenseDate.IsZero() || !empty.AmortizeStart.IsZero() || !empty.AmortizeEnd.IsZero() {
		t.Error("expense 零值的三个字段应均为零值")
	}
}

// TestConsumerParserSentinelErrors 走查 parser 的错误分类映射（方案 §三 #8、§五 5.6）：
// 本包只给分类（哨兵错误），文案键与错误档位由上层附加，不依赖 base/errs。
func TestConsumerParserSentinelErrors(t *testing.T) {
	// 模拟 parser 把账单里的日期字段交给本包解析后的分类处理：
	// 用 errors.Is 判定哨兵错误，无需 import base/errs（L0 层内零依赖）
	classify := func(raw string) string {
		_, err := ParseDate(raw)
		switch {
		case err == nil:
			return "ok"
		case errors.Is(err, ErrDateFormat):
			return "格式不符" // D-3 五类错误之一
		case errors.Is(err, ErrDateValue), errors.Is(err, ErrMonthValue), errors.Is(err, ErrYearRange):
			return "字段缺失或非法" // D-3 五类错误之一
		default:
			return "其他"
		}
	}

	cases := []struct{ raw, want string }{
		{"2026-01-15", "ok"},
		{"2026/01/15", "格式不符"},
		{"", "格式不符"},
		{"2026-02-30", "字段缺失或非法"},
		{"2026-13-01", "字段缺失或非法"},
	}
	for _, c := range cases {
		if got := classify(c.raw); got != c.want {
			t.Errorf("classify(%q) = %q, 期望 %q", c.raw, got, c.want)
		}
	}
}
