package calendar_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 calendar_test）模拟本包的真实调用方
// ——`rule/amortize` 的不变式 1b 与 8.2 区间穿刺、`parser` 的原始交易行日期解析与 D-3 错误分档、
// `store/sqlite` 的 TEXT 列往返与排序、`service/stat` 的 J 域按月聚合、`api/http/dto` 的 JSON 契约。
//
// 另有两条**独立校验路径**（不复用本包的任何判断，否则是自己校验自己）：
//   ① 闰年与月长：用「/4 − /100 + /400」公式与月长表独立回算，与本包逐年逐月比对；
//   ② 月份算术：用 (年, 月) 两元组独立做进位/借位，与 Month.AddMonths 逐项比对。
//
// 文件命名与 `base/decimal`、`base/config`、`base/pwdhash` 既有的 zz_consumer_check_test.go 约定一致。

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/pkg/errors"
)

// ---------- 独立回算基准（不使用本包的任何判断） ----------

// isLeapByFormula 按公历闰年定义独立判定：能被 4 整除且（不能被 100 整除或能被 400 整除）。
func isLeapByFormula(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// daysInMonthByTable 按月长表独立给出某年某月的天数，2 月按 isLeapByFormula 取 28/29。
func daysInMonthByTable(year int, month time.Month) int {
	table := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	days := table[int(month)-1]
	if month == time.February && isLeapByFormula(year) {
		days = 29
	}
	return days
}

// addMonthsByTuple 在 (年, 月) 两元组上独立做月份加减，显式处理进位与借位。
// 刻意不压成月序号——那正是本包的实现方式，用同一手法就验不出问题。
func addMonthsByTuple(year int, month time.Month, n int) (int, time.Month) {
	m := int(month) + n
	for m > 12 {
		m -= 12
		year++
	}
	for m < 1 {
		m += 12
		year--
	}
	return year, time.Month(m)
}

// TestCalendarRulesAgainstIndependentFormula 用独立公式回算闰年与月长，
// 与本包的 NewDate 校验、Month.LastDate 逐年逐月比对（1~9999 年全量）。
//
// 这条是本包「历法委托标准库、不自行实现闰年规则」这一选型的正面证据：
// 若标准库的规范化语义与公历定义有任何偏差，此处会立刻暴露。
func TestCalendarRulesAgainstIndependentFormula(t *testing.T) {
	leapCount := 0
	for year := calendar.MinYear; year <= calendar.MaxYear; year++ {
		if isLeapByFormula(year) {
			leapCount++
		}
		for month := time.January; month <= time.December; month++ {
			wantDays := daysInMonthByTable(year, month)

			// ① 本包认为该月末日是几号（经 Month.LastDate）
			m, err := calendar.NewMonth(year, month)
			if err != nil {
				t.Fatalf("构造年月 %04d-%02d 失败: %+v", year, month, err)
			}
			last, err := m.LastDate()
			if err != nil {
				t.Fatalf("取 %s 末日失败: %+v", m, err)
			}
			if last.Day() != wantDays {
				t.Fatalf("%s 末日不符: 公式 %d 号，本包 %d 号", m, wantDays, last.Day())
			}

			// ② 该月末日必须可构造，末日 +1 必须不可构造
			if _, err := calendar.NewDate(year, month, wantDays); err != nil {
				t.Fatalf("%04d-%02d-%02d 应为合法日期: %+v", year, month, wantDays, err)
			}
			if _, err := calendar.NewDate(year, month, wantDays+1); !errors.Is(err, calendar.ErrInvalidDate) {
				t.Fatalf("%04d-%02d-%02d 应为非法日期，实际 %+v", year, month, wantDays+1, err)
			}
		}
	}
	// 闰年密度交叉核对：公历闰年数 = /4 − /100 + /400
	wantLeap := calendar.MaxYear/4 - calendar.MaxYear/100 + calendar.MaxYear/400
	if leapCount != wantLeap {
		t.Fatalf("闰年数不符: 实测 %d，公式 %d", leapCount, wantLeap)
	}
	// 逐年核对 2 月 29 日的可构造性与公式一致
	for year := calendar.MinYear; year <= calendar.MaxYear; year++ {
		_, err := calendar.NewDate(year, time.February, 29)
		if got := err == nil; got != isLeapByFormula(year) {
			t.Fatalf("%d 年 2 月 29 日可构造=%v，公式判闰=%v", year, got, isLeapByFormula(year))
		}
	}
}

// TestMonthArithmeticAgainstIndependentTuple 用 (年, 月) 两元组独立回算月份加减，
// 与 Month.AddMonths 逐项比对。这是「起始月 + N − 1」这条承载的独立校验。
func TestMonthArithmeticAgainstIndependentTuple(t *testing.T) {
	starts := []struct {
		year  int
		month time.Month
	}{
		{2026, time.January}, {2026, time.February}, {2026, time.June},
		{2026, time.November}, {2026, time.December},
		{2000, time.January}, {1999, time.December}, {2024, time.February},
	}
	// 覆盖 0、正负、跨年、跨多年、H-1 的大月数
	offsets := []int{0, 1, 2, 3, 11, 12, 13, 23, 24, 36, 120, 1200,
		-1, -2, -11, -12, -13, -24, -120}

	for _, s := range starts {
		start, err := calendar.NewMonth(s.year, s.month)
		if err != nil {
			t.Fatalf("构造起始月失败: %+v", err)
		}
		for _, n := range offsets {
			wantYear, wantMonth := addMonthsByTuple(s.year, s.month, n)
			got, err := start.AddMonths(n)

			// 独立回算若越界，本包也必须报越界，不得静默产出值
			if wantYear < calendar.MinYear || wantYear > calendar.MaxYear {
				if !errors.Is(err, calendar.ErrYearOutOfRange) {
					t.Fatalf("%s 加 %d 月应越界报错（独立回算得 %d 年），实际 %+v", start, n, wantYear, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s 加 %d 月失败: %+v", start, n, err)
			}
			if got.Year() != wantYear || got.Month() != wantMonth {
				t.Fatalf("%s 加 %d 月: 独立回算 %04d-%02d，本包 %s",
					start, n, wantYear, wantMonth, got)
			}
		}
	}
}

// ---------- 模拟真实调用方 ----------

// TestAmortizeFlowAsRuleAmortize 模拟 `rule/amortize`（不变式 1b + 8.2）的完整链路：
// 支出日期 → 所属月（= 起始月，H-2）→ 起始月 + N − 1（= 结束月）→ 逐月穿刺派生份额。
//
// 用例特意取**月末日**作支出日期——这正是 civil.AddMonths 会算错的输入
// （实测 2026-01-31 加 1 月得 2026-03-03，所属月错成 3 月）。
func TestAmortizeFlowAsRuleAmortize(t *testing.T) {
	cases := []struct {
		expenseDate string
		months      int
		wantStart   string
		wantEnd     string
	}{
		{"2026-01-31", 1, "2026-01", "2026-01"}, // 不摊分，月末日
		{"2026-01-31", 2, "2026-01", "2026-02"}, // ★ civil 会算成 2026-03
		{"2026-01-31", 3, "2026-01", "2026-03"},
		{"2026-01-29", 2, "2026-01", "2026-02"}, // ★ 同上
		{"2026-01-30", 2, "2026-01", "2026-02"}, // ★ 同上
		{"2026-03-31", 2, "2026-03", "2026-04"}, // ★ civil 会算成 2026-05
		{"2026-05-31", 2, "2026-05", "2026-06"}, // ★
		{"2026-08-31", 2, "2026-08", "2026-09"}, // ★
		{"2026-10-31", 2, "2026-10", "2026-11"}, // ★
		{"2024-02-29", 2, "2024-02", "2024-03"}, // 闰日
		{"2026-12-31", 2, "2026-12", "2027-01"}, // 跨年
		{"2026-11-15", 4, "2026-11", "2027-02"}, // 跨年多月
		{"2026-01-01", 12, "2026-01", "2026-12"},
	}
	for _, c := range cases {
		t.Run(c.expenseDate, func(t *testing.T) {
			// parser 交来的原始交易行日期
			expenseDate, err := calendar.ParseDate(c.expenseDate)
			if err != nil {
				t.Fatalf("解析支出日期失败: %+v", err)
			}
			// 不变式 1b：起始月 = 支出日期所属月（H-2「以支出日期所属月为第 1 个摊分月」）
			start, err := calendar.MonthOf(expenseDate)
			if err != nil {
				t.Fatalf("归月失败: %+v", err)
			}
			if start.String() != c.wantStart {
				t.Fatalf("起始月不符: 期望 %s，实际 %s", c.wantStart, start)
			}
			// 不变式 1b：结束月 = 起始月 + 摊分月数 − 1
			r, err := calendar.NewMonthRange(start, c.months)
			if err != nil {
				t.Fatalf("构造摊分区间失败: %+v", err)
			}
			end, err := r.EndMonth()
			if err != nil {
				t.Fatalf("取结束月失败: %+v", err)
			}
			if end.String() != c.wantEnd {
				t.Fatalf("结束月不符: 期望 %s，实际 %s", c.wantEnd, end)
			}

			// 8.2：逐月派生份额，份数必须恰等于摊分月数，且末月可判
			months, err := r.Months()
			if err != nil {
				t.Fatalf("展开摊分月失败: %+v", err)
			}
			if len(months) != c.months {
				t.Fatalf("摊分份数不符: 期望 %d，实际 %d", c.months, len(months))
			}
			lastMonthCount := 0
			for _, m := range months {
				// 「尾差归末月」的判据：序号 == 月数
				if r.IndexOf(m) == r.Count() {
					lastMonthCount++
					if m != end {
						t.Fatalf("末月判据不一致: %s vs %s", m, end)
					}
				}
			}
			if lastMonthCount != 1 {
				t.Fatalf("末月应恰有 1 个，实际 %d 个", lastMonthCount)
			}
		})
	}
}

// TestIntervalPierceAsStore 模拟 `store` 的「摊分区间穿刺」（起始月 ≤ M 且 结束月 ≥ M）：
// 用**起止月两列的文本比较**（即 SQL 侧的做法）与 MonthRange.Contains（内存侧）逐项比对。
//
// 两侧必须同口径，否则 J 域摊分口径下「派生出的份额」与「库里穿刺出的行数」对不上
// ——这类不一致不会报错，只会让报表数字悄悄错掉。
func TestIntervalPierceAsStore(t *testing.T) {
	type row struct {
		startText string
		endText   string
		rng       calendar.MonthRange
	}
	var rows []row
	for _, spec := range []struct {
		start  string
		months int
	}{
		{"2025-11", 1}, {"2025-12", 2}, {"2026-01", 1}, {"2026-01", 3},
		{"2026-01", 12}, {"2026-06", 2}, {"2026-11", 4}, {"2026-12", 1},
		{"2027-01", 6},
	} {
		start, err := calendar.ParseMonth(spec.start)
		if err != nil {
			t.Fatalf("解析失败: %+v", err)
		}
		r, err := calendar.NewMonthRange(start, spec.months)
		if err != nil {
			t.Fatalf("构造区间失败: %+v", err)
		}
		end, err := r.EndMonth()
		if err != nil {
			t.Fatalf("取末月失败: %+v", err)
		}
		// 模拟落库：起止月各存一个 TEXT 列
		startVal, err := start.Value()
		if err != nil {
			t.Fatalf("起始月落库失败: %+v", err)
		}
		endVal, err := end.Value()
		if err != nil {
			t.Fatalf("结束月落库失败: %+v", err)
		}
		rows = append(rows, row{startText: startVal.(string), endText: endVal.(string), rng: r})
	}

	// 逐个查询月做穿刺，两侧结果必须逐行一致
	for offset := -3; offset <= 18; offset++ {
		base, err := calendar.ParseMonth("2026-01")
		if err != nil {
			t.Fatalf("解析失败: %+v", err)
		}
		queryMonth, err := base.AddMonths(offset)
		if err != nil {
			t.Fatalf("构造查询月失败: %+v", err)
		}
		queryVal, err := queryMonth.Value()
		if err != nil {
			t.Fatalf("查询月落库形态失败: %+v", err)
		}
		queryText := queryVal.(string)

		for _, rw := range rows {
			// SQL 侧：TEXT 列的字典序比较（起始月 <= M AND 结束月 >= M）
			bySQL := rw.startText <= queryText && rw.endText >= queryText
			// 内存侧
			byMemory := rw.rng.Contains(queryMonth)
			if bySQL != byMemory {
				t.Fatalf("穿刺口径不一致: 区间 %s（列 %s~%s），查询月 %s，SQL=%v 内存=%v",
					rw.rng, rw.startText, rw.endText, queryText, bySQL, byMemory)
			}
		}
	}
}

// TestParseFlowAsParser 模拟 `parser` 读账单原始交易行的日期字段，
// 确认 D-3 的「格式不符」与「内容有误」两类**可分别判定**——这是能给对文案键的前提。
func TestParseFlowAsParser(t *testing.T) {
	// 各家银行 CSV 里常见的写法与常见脏数据
	cases := []struct {
		raw      string
		wantKind string // "ok" / "malformed" / "invalid"
	}{
		{"2026-01-05", "ok"},
		{"2026-1-5", "malformed"},            // 非定长
		{"2026/01/05", "malformed"},          // 分隔符不符
		{"20260105", "malformed"},            // 无分隔符
		{"05/01/2026", "malformed"},          // 日月年序
		{"2026-01-05 00:00:00", "malformed"}, // 带时刻
		{"", "malformed"},                    // 字段缺失
		{"  ", "malformed"},                  // 空白（trim 责任在 parser，本包不代劳）
		{"2026-02-30", "invalid"},            // 写法对但日期不存在
		{"2026-02-29", "invalid"},            // 平年闰日
		{"2026-13-01", "invalid"},            // 月份越界
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			d, err := calendar.ParseDate(c.raw)
			switch c.wantKind {
			case "ok":
				if err != nil {
					t.Fatalf("应解析成功: %+v", err)
				}
				if d.IsZero() {
					t.Fatalf("成功时不应为零值")
				}
			case "malformed":
				if !errors.Is(err, calendar.ErrTextMalformed) {
					t.Fatalf("应判为格式不符，实际 %+v", err)
				}
				// 必须与「日期不存在」互不混淆
				if errors.Is(err, calendar.ErrInvalidDate) {
					t.Fatalf("格式不符不应同时命中日期不存在")
				}
			case "invalid":
				if !errors.Is(err, calendar.ErrInvalidDate) && !errors.Is(err, calendar.ErrInvalidMonth) {
					t.Fatalf("应判为日期/月份不存在，实际 %+v", err)
				}
				if errors.Is(err, calendar.ErrTextMalformed) {
					t.Fatalf("日期不存在不应同时命中格式不符")
				}
			}
		})
	}
}

// TestStoreRoundTripAndOrderAsSQLite 模拟 `store/sqlite`：日期与年月经 TEXT 列往返，
// 且**用 SQL 侧的字典序排序**必须得到与时间序一致的结果（F-3 按日期排序、F-4 区间筛选）。
func TestStoreRoundTripAndOrderAsSQLite(t *testing.T) {
	texts := []string{
		"2026-10-01", "2026-01-05", "2026-09-30", "2027-01-01",
		"2026-01-10", "2026-02-01", "0001-01-01", "9999-12-31", "2024-02-29",
	}
	var dates []calendar.Date
	var stored []string
	for _, text := range texts {
		d, err := calendar.ParseDate(text)
		if err != nil {
			t.Fatalf("解析 %q 失败: %+v", text, err)
		}
		val, err := d.Value()
		if err != nil {
			t.Fatalf("落库失败: %+v", err)
		}
		text2, ok := val.(string)
		if !ok {
			t.Fatalf("落库形态应为 string，实际 %T", val)
		}
		// 读回（驱动送 []byte 与 string 两种形态都要能吃）
		var back1, back2 calendar.Date
		if err := back1.Scan(text2); err != nil {
			t.Fatalf("按 string 读回失败: %+v", err)
		}
		if err := back2.Scan([]byte(text2)); err != nil {
			t.Fatalf("按 []byte 读回失败: %+v", err)
		}
		if back1 != d || back2 != d {
			t.Fatalf("往返不一致: 原值 %s，读回 %s / %s", d, back1, back2)
		}
		dates = append(dates, d)
		stored = append(stored, text2)
	}

	// SQL 侧：按 TEXT 列字典序排
	sqlSorted := append([]string(nil), stored...)
	sort.Strings(sqlSorted)
	// 内存侧：按时间序排
	memSorted := append([]calendar.Date(nil), dates...)
	sort.Slice(memSorted, func(i, j int) bool { return memSorted[i].Before(memSorted[j]) })

	for i := range sqlSorted {
		if sqlSorted[i] != memSorted[i].String() {
			t.Fatalf("第 %d 位排序不一致: SQL 侧 %q，内存侧 %q", i, sqlSorted[i], memSorted[i])
		}
	}
}

// TestMonthlyAggregateAsServiceStat 模拟 `service/stat` 的 J 域未摊分口径：
// 按支出日期归月后分组合计，并用「月首日 ≤ 支出日期 ≤ 月末日」的区间条件
// （即 store 下推给 SQL 的等价形态）交叉核对归月结果。
func TestMonthlyAggregateAsServiceStat(t *testing.T) {
	expenseDates := []string{
		"2026-01-01", "2026-01-15", "2026-01-31", // 1 月 3 笔（含月首与月末）
		"2026-02-01", "2026-02-28", // 2 月 2 笔
		"2024-02-29", // 闰日 1 笔
		"2026-12-31", // 年末 1 笔
	}
	// 按月分组：Month 可作 map 键正是这里的刚需
	grouped := map[calendar.Month][]calendar.Date{}
	for _, text := range expenseDates {
		d, err := calendar.ParseDate(text)
		if err != nil {
			t.Fatalf("解析失败: %+v", err)
		}
		m, err := calendar.MonthOf(d)
		if err != nil {
			t.Fatalf("归月失败: %+v", err)
		}
		grouped[m] = append(grouped[m], d)
	}

	wantCounts := map[string]int{"2026-01": 3, "2026-02": 2, "2024-02": 1, "2026-12": 1}
	if len(grouped) != len(wantCounts) {
		t.Fatalf("分组数不符: 期望 %d，实际 %d", len(wantCounts), len(grouped))
	}
	for m, ds := range grouped {
		if want := wantCounts[m.String()]; len(ds) != want {
			t.Fatalf("%s 笔数不符: 期望 %d，实际 %d", m, want, len(ds))
		}
		// 交叉核对：归入本月的每一笔，都必须落在 [月首日, 月末日] 区间内
		first, err := m.FirstDate()
		if err != nil {
			t.Fatalf("取首日失败: %+v", err)
		}
		last, err := m.LastDate()
		if err != nil {
			t.Fatalf("取末日失败: %+v", err)
		}
		for _, d := range ds {
			if d.Before(first) || d.After(last) {
				t.Fatalf("%s 不在 %s 的 [%s, %s] 区间内", d, m, first, last)
			}
		}
		// 反向：区间外的日期不得归入本月
		if outside, err := last.AddDays(1); err == nil {
			om, err := calendar.MonthOf(outside)
			if err != nil {
				t.Fatalf("归月失败: %+v", err)
			}
			if om == m {
				t.Fatalf("末日次日 %s 不应仍归入 %s", outside, m)
			}
		}
	}
}

// TestDTOContractAsAPIHTTPDTO 模拟 `api/http/dto` 的 JSON 契约：
// 前提 6 要求本位币金额等只读派生字段不出现在写请求里，而**摊分起止月同属只读派生**
// ——本用例确认这两个类型在响应侧序列化为字符串、在请求侧能拒掉非法值。
func TestDTOContractAsAPIHTTPDTO(t *testing.T) {
	// 响应侧：F-1 八项里的支出日期 + 两项只读派生的摊分起止月
	type expenseResp struct {
		ExpenseDate calendar.Date  `json:"expenseDate"`
		StartMonth  calendar.Month `json:"amortizeStartMonth"`
		EndMonth    calendar.Month `json:"amortizeEndMonth"`
	}
	date, err := calendar.ParseDate("2026-01-31")
	if err != nil {
		t.Fatalf("解析失败: %+v", err)
	}
	start, err := calendar.MonthOf(date)
	if err != nil {
		t.Fatalf("归月失败: %+v", err)
	}
	rng, err := calendar.NewMonthRange(start, 3)
	if err != nil {
		t.Fatalf("构造区间失败: %+v", err)
	}
	end, err := rng.EndMonth()
	if err != nil {
		t.Fatalf("取末月失败: %+v", err)
	}

	raw, err := json.Marshal(expenseResp{ExpenseDate: date, StartMonth: start, EndMonth: end})
	if err != nil {
		t.Fatalf("序列化失败: %+v", err)
	}
	want := `{"expenseDate":"2026-01-31","amortizeStartMonth":"2026-01","amortizeEndMonth":"2026-03"}`
	if string(raw) != want {
		t.Fatalf("JSON 契约不符:\n期望 %s\n实际 %s", want, raw)
	}

	// 请求侧：写请求只含支出日期（前提 6：派生字段在写请求结构里根本不定义）
	type expenseWriteReq struct {
		ExpenseDate calendar.Date `json:"expenseDate"`
	}
	t.Run("合法请求", func(t *testing.T) {
		var req expenseWriteReq
		if err := json.Unmarshal([]byte(`{"expenseDate":"2026-01-31"}`), &req); err != nil {
			t.Fatalf("反序列化失败: %+v", err)
		}
		if req.ExpenseDate != date {
			t.Fatalf("值不符: %s", req.ExpenseDate)
		}
	})
	t.Run("非法请求一律拒收", func(t *testing.T) {
		for _, body := range []string{
			`{"expenseDate":"2026-02-30"}`, // 日期不存在
			`{"expenseDate":"2026/01/31"}`, // 形态不符
			`{"expenseDate":"0000-01-01"}`, // 年份越界
			`{"expenseDate":""}`,
			`{"expenseDate":"2026-01-31T00:00:00Z"}`, // 带时区的形态一律不收（前提 7）
		} {
			var req expenseWriteReq
			if err := json.Unmarshal([]byte(body), &req); err == nil {
				t.Fatalf("非法请求 %s 应被拒收", body)
			}
		}
	})
}

// TestNoTimezoneTypeInPublicAPI 锁住前提 7 的落点：本包对外**不接收也不产出** time.Time。
//
// §九 前提 7 行要求「任何包不得引入带时区的时间类型参与支出日期的解析、存储与归月」。
// 唯一允许出现 *time.Location 的入口是 Today（且强制显式传、不接受 nil）——
// 因为「今天是几号」本身必须先选时区才有答案。
func TestNoTimezoneTypeInPublicAPI(t *testing.T) {
	// Scan 是最可能被驱动喂进 time.Time 的入口，两个类型都必须拒收
	instant := time.Now()
	var d calendar.Date
	if err := d.Scan(instant); !errors.Is(err, calendar.ErrScanUnsupported) {
		t.Fatalf("Date.Scan 应拒收 time.Time，实际 %+v", err)
	}
	var m calendar.Month
	if err := m.Scan(instant); !errors.Is(err, calendar.ErrScanUnsupported) {
		t.Fatalf("Month.Scan 应拒收 time.Time，实际 %+v", err)
	}

	// 落库产物必须是文本，不能是 time.Time——否则驱动会按连接时区再转一次
	date, err := calendar.ParseDate("2026-01-31")
	if err != nil {
		t.Fatalf("解析失败: %+v", err)
	}
	val, err := date.Value()
	if err != nil {
		t.Fatalf("落库失败: %+v", err)
	}
	if _, isTime := val.(time.Time); isTime {
		t.Fatalf("落库形态不得为 time.Time")
	}
	if _, isString := val.(string); !isString {
		t.Fatalf("落库形态应为 string，实际 %T", val)
	}
}
