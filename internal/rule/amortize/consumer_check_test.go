package amortize

import (
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件是「下游包会怎么用 rule/amortize」的可执行走查，与 base/calendar、
// base/decimal、currency、entity 各包的 consumer_check_test.go 同构。
//
// 每个用例对应分层设计里一处明确的承载，目的是在 L2 阶段就把上层的用法钉死，
// 避免写到 L5 时才发现口径不合。走查的四个消费方（分层 §六 与 §八 H 域行）：
//
//	service/expense(L5.3)  F-5 编辑刷新起止月 + H-4 派生预览入口
//	service/intake(L5.4)   D-7 提交入库时计算摊分起止月
//	service/stat(L5.4)     J-1/J-5 摊分口径份额、J-6 未来待摊分段
//	store(L3)              区间穿刺的两个端点由本包派生

// TestConsumerIntakeDeriveOnSubmit 走查 service/intake（L5.4）的 D-7 提交入库：
// 「解析 → 折算 → 查重 → **摊分起止月派生** → 提交入库」。
//
// 要求本包提供的是：从 entity.Expense 的两个字段（支出日期、摊分月数）
// 直接派生出另两个字段（摊分起始月、摊分结束月），一行赋值即可写回。
func TestConsumerIntakeDeriveOnSubmit(t *testing.T) {
	//模拟 service/intake 把一行原始交易映射为待入库明细后的派生步骤。
	//这里只列本包会用到的四个字段，其余 18 个字段与摊分无关。
	type expense struct {
		SpendDate      calendar.Date  // F-1 用户可编辑
		AmortizeMonths int            // F-1 用户可编辑，默认 1
		AmortizeStart  calendar.Month // F-2 只读派生，前提 6 不接受上游传入值
		AmortizeEnd    calendar.Month // F-2 只读派生
	}

	//从账单解析出的一行：2026-01-15 的支出，用户在预览页填了摊 3 个月
	row := expense{
		SpendDate:      calendar.MustParseDate("2026-01-15"),
		AmortizeMonths: 3,
	}

	//派生并写回——这就是 service/intake 需要写的全部代码
	r, err := Derive(row.SpendDate, row.AmortizeMonths)
	if err != nil {
		t.Fatalf("派生摊分起止月失败: %v", err)
	}
	row.AmortizeStart, row.AmortizeEnd = r.Start(), r.End()

	if row.AmortizeStart.String() != "2026-01" {
		t.Errorf("摊分起始月 = %s, 期望 2026-01", row.AmortizeStart)
	}
	if row.AmortizeEnd.String() != "2026-03" {
		t.Errorf("摊分结束月 = %s, 期望 2026-03", row.AmortizeEnd)
	}

	//默认不摊分（H-1：摊分月数默认 1）：起止月同月，仍必须显式派生而非留空——
	//终版 §四 3 把这两项列为**必填**的只读派生项
	single := expense{SpendDate: calendar.MustParseDate("2026-01-15"), AmortizeMonths: 1}
	r1, err := Derive(single.SpendDate, single.AmortizeMonths)
	if err != nil {
		t.Fatalf("不摊分派生失败: %v", err)
	}
	if !r1.Start().Equal(r1.End()) {
		t.Errorf("不摊分时起止月应相同, 实际 %s", r1)
	}
	if r1.Start().IsZero() {
		t.Error("不摊分时起始月仍须有值（终版 §四 3 必填），不得留零值")
	}
}

// TestConsumerExpenseEditRefresh 走查 service/expense（L5.3）的 F-5 逐笔编辑：
// 「改动日期/摊分月数时刷新摊分起止月」（终版 F-5、8.1 第 3 行与第 6 行）。
//
// 分层 §九 不变式 1b 明写「1a 与 1b 必须由 service/expense / service/intake
// 在同一次保存内一并调用」——本用例走查 1b 那一半。
func TestConsumerExpenseEditRefresh(t *testing.T) {
	//编辑前：2026-01-15，摊 3 个月 → 2026-01 ~ 2026-03
	before, err := Derive(calendar.MustParseDate("2026-01-15"), 3)
	if err != nil {
		t.Fatalf("编辑前派生失败: %v", err)
	}

	t.Run("改支出日期须刷新起止月（8.1 第 3 行）", func(t *testing.T) {
		//日期从 1 月改到 3 月，起止月整体后移
		after, err := Derive(calendar.MustParseDate("2026-03-20"), 3)
		if err != nil {
			t.Fatalf("派生失败: %v", err)
		}
		if after.Start().Equal(before.Start()) {
			t.Error("改支出日期后起始月未变，F-5 要求刷新")
		}
		if after.String() != "2026-03~2026-05" {
			t.Errorf("区间 = %s, 期望 2026-03~2026-05", after)
		}
	})

	t.Run("改摊分月数只动结束月（8.1 第 6 行）", func(t *testing.T) {
		//月数从 3 改到 6，起始月不动、结束月后移
		after, err := Derive(calendar.MustParseDate("2026-01-15"), 6)
		if err != nil {
			t.Fatalf("派生失败: %v", err)
		}
		if !after.Start().Equal(before.Start()) {
			t.Errorf("改摊分月数不应改变起始月: %s → %s", before.Start(), after.Start())
		}
		if after.End().Equal(before.End()) {
			t.Error("改摊分月数后结束月未变")
		}
		if after.End().String() != "2026-06" {
			t.Errorf("结束月 = %s, 期望 2026-06", after.End())
		}
	})
}

// TestConsumerExpenseH4Preview 走查 service/expense（L5.3）暴露的
// **摊分派生预览入口**（准则 7，供 H-4 录入态与编辑态共用，不落库）。
//
// 分层 §八 H 域行：「H-4 即时预览在前端，录入态与编辑态共用
// service/expense 的派生入口（准则 7，不跨层调 L2）」。
// 本用例模拟该入口的实现：收一行明细的六个字段，回一张摊分表。
func TestConsumerExpenseH4Preview(t *testing.T) {
	//模拟 service/expense.PreviewAmortize 的实现体
	preview := func(spendDate calendar.Date, months int,
		amount decimal.Decimal, code currency.Code,
		baseAmount decimal.Decimal, baseCode currency.Code) ([]Row, calendar.MonthRange, error) {
		s, err := NewSchedule(Input{
			SpendDate:    spendDate,
			Months:       months,
			Amount:       amount,
			Currency:     code,
			BaseAmount:   baseAmount,
			BaseCurrency: baseCode,
		})
		if err != nil {
			return nil, calendar.MonthRange{}, err
		}
		return s.Rows(), s.Range(), nil
	}

	rows, r, err := preview(
		calendar.MustParseDate("2026-01-15"), 3,
		decimal.MustParse("100.00"), currency.MustParse("CNY"),
		decimal.MustParse("712.35"), currency.MustParse("CNY"),
	)
	if err != nil {
		t.Fatalf("H-4 预览失败: %v", err)
	}

	//「提示本次修改将影响的月份范围」
	if r.String() != "2026-01~2026-03" {
		t.Errorf("影响月份范围 = %s, 期望 2026-01~2026-03", r)
	}
	//「即时展示覆盖月份区间与每月分摊金额」
	if len(rows) != 3 {
		t.Fatalf("预览行数 = %d, 期望 3", len(rows))
	}
	for i, row := range rows {
		if row.Month.IsZero() {
			t.Errorf("第 %d 行月份为零值", i+1)
		}
	}
	//预览是纯派生、不落库（H-3）：同样的入参再调一次结果必须一致
	rows2, _, err := preview(
		calendar.MustParseDate("2026-01-15"), 3,
		decimal.MustParse("100.00"), currency.MustParse("CNY"),
		decimal.MustParse("712.35"), currency.MustParse("CNY"),
	)
	if err != nil {
		t.Fatalf("二次预览失败: %v", err)
	}
	for i := range rows {
		if !rows[i].Equal(rows2[i]) {
			t.Errorf("第 %d 行两次预览结果不一致", i+1)
		}
	}
}

// TestConsumerStatPierceAndAggregate 走查 service/stat（L5.4）的 J-1 摊分口径：
// 「摊分口径用区间穿刺取行后经 rule/amortize 派生份额再聚合」（分层 §六 L5.4）。
//
// 分工：store 下推 SQL 做穿刺（起始月 ≤ M 且 结束月 ≥ M）取回候选行，
// 本包的 Schedule.At 回答「这一行在该月摊多少」，service/stat 负责求和。
func TestConsumerStatPierceAndAggregate(t *testing.T) {
	//三笔明细，都用 CNY，本位币也是 CNY（简化聚合的验算）
	cny := currency.MustParse("CNY")
	inputs := []Input{
		//甲：2026-01 起摊 3 个月，100.00 → 每月 33.33，末月 33.34
		{calendar.MustParseDate("2026-01-10"), 3, decimal.MustParse("100.00"), cny,
			decimal.MustParse("100.00"), cny},
		//乙：2026-02 不摊分，50.00 → 只在 2026-02 出现
		{calendar.MustParseDate("2026-02-05"), 1, decimal.MustParse("50.00"), cny,
			decimal.MustParse("50.00"), cny},
		//丙：2025-11 起摊 6 个月，600.00 → 每月 100.00
		{calendar.MustParseDate("2025-11-20"), 6, decimal.MustParse("600.00"), cny,
			decimal.MustParse("600.00"), cny},
	}

	schedules := make([]Schedule, len(inputs))
	for i, in := range inputs {
		s, err := NewSchedule(in)
		if err != nil {
			t.Fatalf("第 %d 笔派生失败: %v", i+1, err)
		}
		schedules[i] = s
	}

	//查 2026-02：甲（01~03）、乙（02）、丙（2025-11~2026-04）三笔全部命中
	target := calendar.MustParseMonth("2026-02")
	var hits []decimal.Decimal
	for i, s := range schedules {
		row, ok := s.At(target)
		if !ok {
			t.Errorf("第 %d 笔应命中 %s（区间 %s）", i+1, target, s.Range())
			continue
		}
		hits = append(hits, row.Amount)

		//与 store 侧的 SQL 穿刺判据必须同结论——两条路径分叉就会出现
		//「SQL 取回来的行本包说不在区间内」
		byString := s.Range().Start().String() <= target.String() &&
			s.Range().End().String() >= target.String()
		if !byString {
			t.Errorf("第 %d 笔：SQL 字符串穿刺判定未命中，与本包不一致", i+1)
		}
	}
	if len(hits) != 3 {
		t.Fatalf("2026-02 命中 %d 笔, 期望 3 笔", len(hits))
	}
	//J-1 月度支出总额（摊分口径）= 33.33 + 50.00 + 100.00
	if sum := decimal.Sum(hits...); sum.String() != "183.33" {
		t.Errorf("2026-02 摊分口径总额 = %s, 期望 183.33", sum)
	}

	//查 2026-05：甲（01~03）与乙（02）落空，丙（~2026-04）也落空
	miss := calendar.MustParseMonth("2026-05")
	for i, s := range schedules {
		if _, ok := s.At(miss); ok {
			t.Errorf("第 %d 笔不应命中 %s（区间 %s）", i+1, miss, s.Range())
		}
	}
}

// TestConsumerStatDrilldown 走查 J-5 下钻：「摊分口径下展示**份额金额 + 来源支出**」。
//
// 要求本包能同时给出这两个数——份额金额来自 At，来源支出总额来自 Shares.Total。
func TestConsumerStatDrilldown(t *testing.T) {
	s, err := NewSchedule(Input{
		SpendDate:    calendar.MustParseDate("2026-01-10"),
		Months:       3,
		Amount:       decimal.MustParse("100.00"),
		Currency:     currency.MustParse("CNY"),
		BaseAmount:   decimal.MustParse("1400"),
		BaseCurrency: currency.MustParse("JPY"),
	})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}

	row, ok := s.At(calendar.MustParseMonth("2026-03"))
	if !ok {
		t.Fatal("2026-03 应命中")
	}
	//「份额金额」——该月实际摊到的钱（末月含尾差）
	if row.Amount.String() != "33.34" {
		t.Errorf("份额金额 = %s, 期望 33.34", row.Amount)
	}
	//「来源支出」——这笔支出的原始总额，用于在下钻页并列展示
	if s.AmountShares().Total().String() != "100" {
		t.Errorf("来源支出总额 = %s, 期望 100", s.AmountShares().Total())
	}
	//本位币侧同样可取，且位数与原币不同（JPY 0 位）
	if row.BaseAmount.String() != "468" {
		t.Errorf("本位币份额 = %s, 期望 468", row.BaseAmount)
	}
}

// TestConsumerStatFutureSegment 走查 J-6：「摊分口径下将『已发生（≤ 当前月）』
// 与『未来待摊』分段展示」。
//
// **分段判定归 service/stat**（需要读系统时间，本包无时间依赖，见包注释
// 「不做什么」）。本用例走查的是：本包提供的区间端点与逐月份额，
// 足以让 service/stat 完成分段，且两段之和等于总额。
func TestConsumerStatFutureSegment(t *testing.T) {
	s, err := NewSchedule(Input{
		SpendDate:    calendar.MustParseDate("2026-01-10"),
		Months:       6,
		Amount:       decimal.MustParse("600.00"),
		Currency:     currency.MustParse("CNY"),
		BaseAmount:   decimal.MustParse("600.00"),
		BaseCurrency: currency.MustParse("CNY"),
	})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}

	//模拟 service/stat：以「当前月」为界切两段（当前月由服务层给出，不是本包）
	current := calendar.MustParseMonth("2026-03")
	var occurred, future []decimal.Decimal
	for _, row := range s.Rows() {
		if row.Month.After(current) {
			future = append(future, row.Amount)
		} else {
			occurred = append(occurred, row.Amount)
		}
	}

	//2026-01 ~ 2026-03 已发生共 3 个月，2026-04 ~ 2026-06 未来待摊共 3 个月
	if len(occurred) != 3 {
		t.Errorf("已发生段 = %d 个月, 期望 3", len(occurred))
	}
	if len(future) != 3 {
		t.Errorf("未来待摊段 = %d 个月, 期望 3", len(future))
	}
	//两段之和必须精确等于原额，一分不漏
	all := append(append([]decimal.Decimal{}, occurred...), future...)
	if sum := decimal.Sum(all...); !sum.Equal(decimal.MustParse("600.00")) {
		t.Errorf("两段之和 = %s, 期望 600.00", sum)
	}
}

// TestConsumerStoreRangeEndpoints 走查 store（L3）的摊分区间穿刺端点来源。
//
// 分层 §六 L3 store ④ 的「摊分区间穿刺（起始月 ≤ M 且 结束月 ≥ M）」
// 用的是明细上冗余的两个字段，而那两个字段由本包派生。
// 要求：派生出的端点落库为定长文本后，SQL 侧的字符串比较与本包判定同结论。
func TestConsumerStoreRangeEndpoints(t *testing.T) {
	r, err := Derive(calendar.MustParseDate("2026-01-15"), 3)
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}

	//落库形态：calendar.Month 的 Value() 是定长 7 字符 TEXT
	startVal, err := r.Start().Value()
	if err != nil {
		t.Fatalf("起始月落库失败: %v", err)
	}
	endVal, err := r.End().Value()
	if err != nil {
		t.Fatalf("结束月落库失败: %v", err)
	}
	startText, ok := startVal.(string)
	if !ok {
		t.Fatalf("起始月落库形态 = %T, 期望 string", startVal)
	}
	endText, ok := endVal.(string)
	if !ok {
		t.Fatalf("结束月落库形态 = %T, 期望 string", endVal)
	}
	if len(startText) != 7 || len(endText) != 7 {
		t.Errorf("端点落库文本须定长 7 字符, 实际 %q / %q", startText, endText)
	}

	//SQL 侧字符串穿刺与本包 Contains 必须同结论，否则统计口径分叉
	for _, m := range []string{"2025-12", "2026-01", "2026-02", "2026-03", "2026-04"} {
		target := calendar.MustParseMonth(m)
		bySQL := startText <= m && endText >= m
		byRange := r.Contains(target)
		if bySQL != byRange {
			t.Errorf("%s：SQL 判定 %v 与区间判定 %v 不一致", m, bySQL, byRange)
		}
	}
}

// TestConsumerCrossCheckWithEntityExpectation 与 entity 包 consumer_check_test.go
// 中 TestConsumerAmortizeSplit / TestConsumerAmortizeInvariant1b 的期望值逐条对齐。
//
// 那两个用例是在 L1.1 阶段用「模拟实现」钉下的口径。本用例用**真实实现**
// 跑同一组输入，确保 L2 落地后与 L1.1 阶段的约定完全一致——若不一致，
// 说明两轮之间口径漂移了，而这正是分阶段实现最容易出问题的地方。
func TestConsumerCrossCheckWithEntityExpectation(t *testing.T) {
	t.Run("不变式 1b 三组（对齐 entity 的 TestConsumerAmortizeInvariant1b）", func(t *testing.T) {
		cases := []struct {
			date, start, end string
			months           int
		}{
			{"2026-01-15", "2026-01", "2026-01", 1},
			{"2026-11-30", "2026-11", "2027-10", 12},
			{"2026-01-01", "2026-01", "2075-12", 600},
		}
		for _, c := range cases {
			r, err := Derive(calendar.MustParseDate(c.date), c.months)
			if err != nil {
				t.Fatalf("%s 摊 %d 月派生失败: %v", c.date, c.months, err)
			}
			if r.Start().String() != c.start || r.End().String() != c.end {
				t.Errorf("%s 摊 %d 月：got %s~%s, want %s~%s",
					c.date, c.months, r.Start(), r.End(), c.start, c.end)
			}
		}
	})

	t.Run("8.2 均分两组（对齐 entity 的 TestConsumerAmortizeSplit）", func(t *testing.T) {
		//entity 用例的原始输入：CNY 100.00 摊 3 月、JPY 1400 摊 3 月
		s, err := NewSchedule(Input{
			SpendDate:    calendar.MustParseDate("2026-01-15"),
			Months:       3,
			Amount:       decimal.MustParse("100.00"),
			Currency:     currency.MustParse("CNY"),
			BaseAmount:   decimal.MustParse("1400"),
			BaseCurrency: currency.MustParse("JPY"),
		})
		if err != nil {
			t.Fatalf("派生失败: %v", err)
		}

		//原币：33.33 / 33.33 / 33.34
		amounts := s.AmountShares().All()
		if amounts[0].String() != "33.33" || amounts[2].String() != "33.34" {
			t.Errorf("原币份额: got %s/%s/%s, want 33.33/33.33/33.34",
				amounts[0], amounts[1], amounts[2])
		}
		//本位币：466 / 466 / 468
		bases := s.BaseShares().All()
		if bases[0].String() != "466" || bases[2].String() != "468" {
			t.Errorf("本位币份额: got %s/%s/%s, want 466/466/468",
				bases[0], bases[1], bases[2])
		}
	})

	t.Run("currency 用例的 KWD × CNY 组（对齐 currency 的 TestConsumerAmortizeSplit）", func(t *testing.T) {
		//KWD（3 位）100.000 摊 3 月 → 33.333 余 0.001
		kwd, err := NewShares(decimal.MustParse("100.000"), 3, currency.MustParse("KWD"))
		if err != nil {
			t.Fatalf("KWD 均分失败: %v", err)
		}
		if kwd.Quotient().String() != "33.333" {
			t.Errorf("KWD 商 = %s, 期望 33.333", kwd.Quotient())
		}
		//CNY（2 位）2380.00 摊 3 月 → 793.33 余 0.01
		cny, err := NewShares(decimal.MustParse("2380.00"), 3, currency.MustParse("CNY"))
		if err != nil {
			t.Fatalf("CNY 均分失败: %v", err)
		}
		if cny.Quotient().String() != "793.33" {
			t.Errorf("CNY 商 = %s, 期望 793.33", cny.Quotient())
		}
		//可加性：份额 × 月数 + 尾差 = 原额（currency 用例的断言形式）
		if got := kwd.Quotient().Mul(decimal.FromInt(3)).Add(kwd.Tail()); got.String() != "100" {
			t.Errorf("KWD 份额×3+尾差 = %s, 期望 100.000", got)
		}
		if got := cny.Quotient().Mul(decimal.FromInt(3)).Add(cny.Tail()); got.String() != "2380" {
			t.Errorf("CNY 份额×3+尾差 = %s, 期望 2380.00", got)
		}
	})
}

// TestConsumerUnconvergedRowUsesOwnBaseCurrency 走查不变式 2 相关的一处易错点：
// 未收敛行（折算本位币币种 ≠ 当前本位币）的本位币份额，必须按**该行自己的**
// 折算本位币币种 均分，而不是按当前本位币。
//
// Input.BaseCurrency 的注释解释了理由：未收敛行的 BaseAmount 是按它自己那个
// 币种算出来的，用当前本位币的位数去切它，就是拿 A 币种的位数切 B 币种的钱。
func TestConsumerUnconvergedRowUsesOwnBaseCurrency(t *testing.T) {
	//一行未收敛数据：本位币金额是按 JPY（0 位）算的，而当前本位币已切到 CNY（2 位）
	unconverged := Input{
		SpendDate:    calendar.MustParseDate("2026-01-10"),
		Months:       3,
		Amount:       decimal.MustParse("100.00"),
		Currency:     currency.MustParse("CNY"),
		BaseAmount:   decimal.MustParse("1400"),
		BaseCurrency: currency.MustParse("JPY"), // 该行自己的口径
	}

	s, err := NewSchedule(unconverged)
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}
	//必须按 JPY 的 0 位切，得整数份额
	bases := s.BaseShares().All()
	for i, b := range bases {
		if !b.FitsScale(0) {
			t.Errorf("第 %d 月本位币份额 %s 不是整数——未按该行自己的 JPY 口径均分", i+1, b)
		}
	}
	if bases[0].String() != "466" || bases[2].String() != "468" {
		t.Errorf("本位币份额: got %s/%s/%s, want 466/466/468", bases[0], bases[1], bases[2])
	}
	//基准币种必须是该行自己的 JPY，不是当前本位币
	if s.BaseShares().Currency().String() != "JPY" {
		t.Errorf("本位币份额基准 = %s, 期望 JPY（该行自己的口径）", s.BaseShares().Currency())
	}
}
