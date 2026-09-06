package calendar

// Date 的白盒单测：构造与解析的错误分档、定长文本与字典序、比较与天数加减、
// 序列化与落库往返，以及**前提 7 的跨时区不漂移**（§十一 阶段 1 点名要求的用例）。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pkg/errors"
)

// mustDate 构造合法日期，失败即测试失败。
func mustDate(t *testing.T, year int, month time.Month, day int) Date {
	t.Helper()
	d, err := NewDate(year, month, day)
	if err != nil {
		t.Fatalf("构造日期 %04d-%02d-%02d 意外失败: %+v", year, month, day, err)
	}
	return d
}

func TestNewDate(t *testing.T) {
	cases := []struct {
		name    string
		year    int
		month   time.Month
		day     int
		wantErr error // nil 表示应当成功
	}{
		{"常规日期", 2026, time.January, 31, nil},
		{"闰年2月29日", 2024, time.February, 29, nil},
		{"世纪闰年2000", 2000, time.February, 29, nil},
		{"年份下界", MinYear, time.January, 1, nil},
		{"年份上界", MaxYear, time.December, 31, nil},

		// 「写法对但日期不存在」——ErrInvalidDate
		{"平年2月29日", 2026, time.February, 29, ErrInvalidDate},
		{"2月30日", 2026, time.February, 30, ErrInvalidDate},
		{"非世纪闰年1900", 1900, time.February, 29, ErrInvalidDate},
		{"4月31日", 2026, time.April, 31, ErrInvalidDate},
		{"日为0", 2026, time.January, 0, ErrInvalidDate},
		{"日为32", 2026, time.January, 32, ErrInvalidDate},

		// 月份越界单独成档，供 parser 区分 D-3 的错误类型
		{"月份为0", 2026, time.Month(0), 1, ErrInvalidMonth},
		{"月份为13", 2026, time.Month(13), 1, ErrInvalidMonth},

		// 年份越界优先于月份日期校验
		{"年份0", 0, time.January, 1, ErrYearOutOfRange},
		{"年份负", -1, time.January, 1, ErrYearOutOfRange},
		{"年份10000", 10000, time.January, 1, ErrYearOutOfRange},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := NewDate(c.year, c.month, c.day)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("期望成功，实际报错: %+v", err)
				}
				if d.Year() != c.year || d.Month() != c.month || d.Day() != c.day {
					t.Fatalf("字段不符: 期望 %04d-%02d-%02d，实际 %s", c.year, c.month, c.day, d)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("错误分档不符: 期望 %v，实际 %+v", c.wantErr, err)
			}
			// 失败时必须返回零值，调用方忽略 error 也拿不到脏值
			if !d.IsZero() {
				t.Fatalf("失败时应返回零值，实际 %s", d)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct {
		text    string
		wantErr error
	}{
		{"2026-01-05", nil},
		{"2026-12-31", nil},
		{"2024-02-29", nil},
		{"0001-01-01", nil},
		{"9999-12-31", nil},

		// 写法不对 → ErrTextMalformed
		{"2026-1-5", ErrTextMalformed},
		{"2026/01/05", ErrTextMalformed},
		{"20260105", ErrTextMalformed},
		{"2026-01-05T00:00:00Z", ErrTextMalformed},
		{"2026-01-05Z", ErrTextMalformed},
		{" 2026-01-05", ErrTextMalformed},
		{"2026-01-05 ", ErrTextMalformed},
		{"", ErrTextMalformed},
		{"abcd-ef-gh", ErrTextMalformed},
		// 对抗性输入：文本自身含 "out of range"，不得被误判成值越界
		{"out of range", ErrTextMalformed},
		{"2026-01-05 out of range", ErrTextMalformed},
		{"2026-01-05out of range", ErrTextMalformed},

		// 写法对但日期不存在 → ErrInvalidDate
		{"2026-02-30", ErrInvalidDate},
		{"2026-02-29", ErrInvalidDate},
		{"2026-01-32", ErrInvalidDate},
		{"2026-01-00", ErrInvalidDate},
		{"2026-13-01", ErrInvalidDate},
		{"2026-00-01", ErrInvalidDate},

		// 能解析但年份越界：不放行「能算出来但存不回来」的值
		{"0000-01-01", ErrYearOutOfRange},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			d, err := ParseDate(c.text)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("期望成功，实际报错: %+v", err)
				}
				// 解析后回写必须与原文逐字节相同（无损往返）
				if got := d.String(); got != c.text {
					t.Fatalf("往返不一致: 原文 %q，回写 %q", c.text, got)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("错误分档不符: 期望 %v，实际 %+v", c.wantErr, err)
			}
			if !d.IsZero() {
				t.Fatalf("失败时应返回零值，实际 %s", d)
			}
		})
	}
}

// TestParseDateErrorKindsAreDistinct 锁住「形态不符」与「日期不存在」两档**互不混淆**。
// 这是 parser 能给对 D-3 文案的前提：两者都失败还不够，必须能分开。
func TestParseDateErrorKindsAreDistinct(t *testing.T) {
	_, malformedErr := ParseDate("2026/01/05")
	if errors.Is(malformedErr, ErrInvalidDate) {
		t.Fatalf("形态不符被误判为日期不存在: %+v", malformedErr)
	}
	_, invalidErr := ParseDate("2026-02-30")
	if errors.Is(invalidErr, ErrTextMalformed) {
		t.Fatalf("日期不存在被误判为形态不符: %+v", invalidErr)
	}
}

// TestDateStringLexicalOrderEqualsChronological 锁住「字典序 = 时间序」。
// store/sqlite 的 F-3 排序与 F-4 日期区间筛选直接依赖它——若不成立，
// 库里按 TEXT 列排出来的顺序就是错的，且不会有任何报错。
func TestDateStringLexicalOrderEqualsChronological(t *testing.T) {
	// 覆盖同月内、跨月、跨年、跨世纪，以及补零位（9月 vs 10月是最容易错的一处）
	ordered := []Date{
		mustDate(t, 1, time.January, 1),
		mustDate(t, 1999, time.December, 31),
		mustDate(t, 2026, time.January, 5),
		mustDate(t, 2026, time.January, 10),
		mustDate(t, 2026, time.February, 1),
		mustDate(t, 2026, time.September, 1),
		mustDate(t, 2026, time.September, 30),
		mustDate(t, 2026, time.October, 1),
		mustDate(t, 2026, time.December, 31),
		mustDate(t, 2027, time.January, 1),
		mustDate(t, 9999, time.December, 31),
	}
	for i := 0; i+1 < len(ordered); i++ {
		lo, hi := ordered[i], ordered[i+1]
		if !(lo.String() < hi.String()) {
			t.Fatalf("字典序与时间序不符: %q 应 < %q", lo.String(), hi.String())
		}
		if lo.Compare(hi) != -1 {
			t.Fatalf("Compare 不符: %s 应早于 %s", lo, hi)
		}
		if !lo.Before(hi) || !hi.After(lo) {
			t.Fatalf("Before/After 不符: %s 与 %s", lo, hi)
		}
	}
}

// TestDateComparable 锁住 Date **可比较**、可作 map 键。
// 8.3 判重要按「(日期, 金额, 币种)」元组批量取候选（store ④），元组要能进 map；
// 这与 base/decimal.Decimal 刻意不可比较正好相反，故必须显式钉住。
func TestDateComparable(t *testing.T) {
	viaFields := mustDate(t, 2026, time.January, 31)
	viaParse, err := ParseDate("2026-01-31")
	if err != nil {
		t.Fatalf("解析失败: %+v", err)
	}
	if viaFields != viaParse {
		t.Fatalf("经不同路径构造的同一日期应 == 相等")
	}
	if !viaFields.Equal(viaParse) {
		t.Fatalf("Equal 应为真")
	}
	counter := map[Date]int{}
	counter[viaFields]++
	counter[viaParse]++
	if len(counter) != 1 || counter[viaFields] != 2 {
		t.Fatalf("作 map 键应归并为同一项: len=%d, count=%d", len(counter), counter[viaFields])
	}
}

func TestDateZeroValue(t *testing.T) {
	var zero Date
	if !zero.IsZero() {
		t.Fatalf("零值 IsZero 应为真")
	}
	if zero.Year() != 0 || zero.Month() != 0 || zero.Day() != 0 {
		t.Fatalf("零值字段应全为 0，实际 %d-%d-%d", zero.Year(), zero.Month(), zero.Day())
	}
	// 零值文本刻意非法：肉眼可辨，且无法被 ParseDate 读回（不会悄悄变成合法日期）
	if got := zero.String(); got != "0000-00-00" {
		t.Fatalf("零值文本应为 0000-00-00，实际 %q", got)
	}
	if _, err := ParseDate(zero.String()); err == nil {
		t.Fatalf("零值文本不应能被解析回来")
	}

	// 零值在要求真实日期的出口一律报 ErrZeroValue，而非静默产出非法形态
	if _, err := zero.MarshalText(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值序列化应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.Value(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值落库应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.AddDays(1); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值加减天数应报 ErrZeroValue，实际 %+v", err)
	}
}

func TestDateAddDays(t *testing.T) {
	cases := []struct {
		from string
		n    int
		want string
	}{
		{"2026-01-31", 1, "2026-02-01"},   // 跨月
		{"2026-12-31", 1, "2027-01-01"},   // 跨年
		{"2026-02-28", 1, "2026-03-01"},   // 平年 2 月末
		{"2024-02-28", 1, "2024-02-29"},   // 闰年 2 月末
		{"2024-02-29", 1, "2024-03-01"},   // 闰日次日
		{"2026-03-01", -1, "2026-02-28"},  // 负向跨月
		{"2024-03-01", -1, "2024-02-29"},  // 负向落到闰日
		{"2026-01-05", 0, "2026-01-05"},   // 零位移
		{"2026-01-01", 365, "2027-01-01"}, // 平年整年
		{"2024-01-01", 366, "2025-01-01"}, // 闰年整年
	}
	for _, c := range cases {
		t.Run(c.from, func(t *testing.T) {
			from, err := ParseDate(c.from)
			if err != nil {
				t.Fatalf("解析失败: %+v", err)
			}
			got, err := from.AddDays(c.n)
			if err != nil {
				t.Fatalf("加减天数失败: %+v", err)
			}
			if got.String() != c.want {
				t.Fatalf("%s 加 %d 天: 期望 %s，实际 %s", c.from, c.n, c.want, got)
			}
		})
	}

	// 越出年份边界即报错，不产出存不回来的值
	upper := mustDate(t, MaxYear, time.December, 31)
	if _, err := upper.AddDays(1); !errors.Is(err, ErrYearOutOfRange) {
		t.Fatalf("越过上界应报 ErrYearOutOfRange，实际 %+v", err)
	}
	lower := mustDate(t, MinYear, time.January, 1)
	if _, err := lower.AddDays(-1); !errors.Is(err, ErrYearOutOfRange) {
		t.Fatalf("越过下界应报 ErrYearOutOfRange，实际 %+v", err)
	}
}

// TestDateNoTimezoneDrift 是 §十一 阶段 1 点名要求的「**跨时区不漂移**」用例（前提 7）。
//
// 判据不是「解析出来的日期对不对」，而是：**同一份账单文本，在任何 TZ 下解析、归月、
// 落库、读回，结果必须逐字节相同**。取的两个时区是最坏情况——UTC+14（Kiritimati）
// 与 UTC−11（Midway）相差 25 小时，任何一处经了 time.Time 中转都必然露出跨日差异。
func TestDateNoTimezoneDrift(t *testing.T) {
	zones := []string{
		"UTC",
		"Asia/Shanghai",       // UTC+8，本项目的主场景
		"America/New_York",    // UTC−5/−4，含夏令时
		"Pacific/Kiritimati",  // UTC+14，最东
		"Pacific/Midway",      // UTC−11，最西
		"Australia/Lord_Howe", // 半小时制夏令时，偏移非整小时
	}
	// 月初、月末、闰日、年末——归月最敏感的四类边界
	texts := []string{"2026-01-01", "2026-01-31", "2024-02-29", "2026-12-31"}

	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("加载时区 %s 失败: %+v", zone, err)
		}
		// 把进程默认时区切到该区：本包若在任何一处读了 time.Local 或经 time.Time 中转，
		// 下面的断言就会失败。t.Setenv 会在子测试结束后自动还原。
		t.Setenv("TZ", zone)
		_ = loc

		for _, text := range texts {
			d, err := ParseDate(text)
			if err != nil {
				t.Fatalf("[%s] 解析 %q 失败: %+v", zone, text, err)
			}
			if got := d.String(); got != text {
				t.Fatalf("[%s] 文本漂移: 原文 %q，回写 %q", zone, text, got)
			}
			// 归月结果也不得随时区变化
			m, err := MonthOf(d)
			if err != nil {
				t.Fatalf("[%s] 归月失败: %+v", zone, err)
			}
			if want := text[:7]; m.String() != want {
				t.Fatalf("[%s] 归月漂移: 日期 %q 应归入 %q，实际 %q", zone, text, want, m)
			}
			// 落库 → 读回 一轮
			stored, err := d.Value()
			if err != nil {
				t.Fatalf("[%s] 落库失败: %+v", zone, err)
			}
			var readBack Date
			if err := readBack.Scan(stored); err != nil {
				t.Fatalf("[%s] 读回失败: %+v", zone, err)
			}
			if readBack != d {
				t.Fatalf("[%s] 落库往返不一致: 原值 %s，读回 %s", zone, d, readBack)
			}
		}
	}
}

// TestTodayRequiresExplicitLocation 锁住前提 7 拦截①：不替调用方选时区。
func TestTodayRequiresExplicitLocation(t *testing.T) {
	if _, err := Today(nil); !errors.Is(err, ErrNilLocation) {
		t.Fatalf("nil 时区应报 ErrNilLocation，实际 %+v", err)
	}
	// 显式传时区则可用，且结果与该时区的当天一致
	for _, zone := range []string{"UTC", "Asia/Shanghai", "Pacific/Kiritimati"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("加载时区失败: %+v", err)
		}
		got, err := Today(loc)
		if err != nil {
			t.Fatalf("[%s] 取当天失败: %+v", zone, err)
		}
		now := time.Now().In(loc)
		want := mustDate(t, now.Year(), now.Month(), now.Day())
		if got != want {
			t.Fatalf("[%s] 当天不符: 期望 %s，实际 %s", zone, want, got)
		}
	}
	// 同一时刻在最东与最西两个时区取当天，通常相差一天——正是「必须显式选时区」的理由
	east, _ := time.LoadLocation("Pacific/Kiritimati")
	west, _ := time.LoadLocation("Pacific/Midway")
	de, err1 := Today(east)
	dw, err2 := Today(west)
	if err1 != nil || err2 != nil {
		t.Fatalf("取当天失败: %+v / %+v", err1, err2)
	}
	if diff := de.Compare(dw); diff < 0 {
		t.Fatalf("最东时区的当天不应早于最西时区: %s vs %s", de, dw)
	}
}

// TestDateScanRejectsTimeTime 锁住前提 7 拦截②：Scan 拒收 time.Time。
//
// 附带证明「为什么必须拒」：同一瞬间在不同时区取日期会**跨月**，
// 若放行则 J 域归月会随部署机器的 TZ 漂移。
func TestDateScanRejectsTimeTime(t *testing.T) {
	instant := time.Date(2026, time.January, 31, 20, 0, 0, 0, time.UTC)

	// 先证明危害真实存在：同一瞬间在两个时区下的日历日期确实跨月
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %+v", err)
	}
	if utcDay, cstDay := instant.In(time.UTC).Day(), instant.In(shanghai).Day(); utcDay == cstDay {
		t.Fatalf("用例前提失效: 该瞬间在两时区下应跨日，实际同为 %d 号", utcDay)
	}
	if utcMonth, cstMonth := instant.In(time.UTC).Month(), instant.In(shanghai).Month(); utcMonth == cstMonth {
		t.Fatalf("用例前提失效: 该瞬间在两时区下应跨月，实际同为 %d 月", utcMonth)
	}

	// 再断言本包一律拒收，而不是「挑一个时区」
	var d Date
	for _, src := range []any{instant, instant.In(shanghai)} {
		if err := d.Scan(src); !errors.Is(err, ErrScanUnsupported) {
			t.Fatalf("应拒收 time.Time，实际 %+v", err)
		}
		if !d.IsZero() {
			t.Fatalf("拒收后不应写入接收者，实际 %s", d)
		}
	}
}

func TestDateScan(t *testing.T) {
	t.Run("接受文本与字节", func(t *testing.T) {
		for _, src := range []any{"2026-01-31", []byte("2026-01-31")} {
			var d Date
			if err := d.Scan(src); err != nil {
				t.Fatalf("扫描 %T 失败: %+v", src, err)
			}
			if d.String() != "2026-01-31" {
				t.Fatalf("扫描结果不符: %s", d)
			}
		}
	})
	t.Run("拒收NULL与其他类型", func(t *testing.T) {
		for _, src := range []any{nil, int64(20260131), 20260131.0, true} {
			var d Date
			if err := d.Scan(src); !errors.Is(err, ErrScanUnsupported) {
				t.Fatalf("应拒收 %T，实际 %+v", src, err)
			}
		}
	})
	t.Run("非法文本按解析错误分档", func(t *testing.T) {
		var d Date
		if err := d.Scan("2026/01/31"); !errors.Is(err, ErrTextMalformed) {
			t.Fatalf("应报 ErrTextMalformed，实际 %+v", err)
		}
		if err := d.Scan("2026-02-30"); !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("应报 ErrInvalidDate，实际 %+v", err)
		}
		// []byte 分支的失败路径同样要走到，且不写坏接收者
		if err := d.Scan([]byte("2026/01/31")); !errors.Is(err, ErrTextMalformed) {
			t.Fatalf("[]byte 分支应报 ErrTextMalformed，实际 %+v", err)
		}
		if !d.IsZero() {
			t.Fatalf("失败时不应改写接收者，实际 %s", d)
		}
	})
}

// TestDateUnmarshalTextErrors 覆盖 UnmarshalText 的失败路径，并确认失败时不写坏接收者。
func TestDateUnmarshalTextErrors(t *testing.T) {
	original := mustDate(t, 2026, time.January, 31)
	for _, tc := range []struct {
		text    string
		wantErr error
	}{
		{"2026/01/31", ErrTextMalformed},
		{"", ErrTextMalformed},
		{"2026-02-30", ErrInvalidDate},
		{"2026-13-01", ErrInvalidDate},
		{"0000-01-01", ErrYearOutOfRange},
	} {
		d := original
		if err := d.UnmarshalText([]byte(tc.text)); !errors.Is(err, tc.wantErr) {
			t.Fatalf("解析 %q 应报 %v，实际 %+v", tc.text, tc.wantErr, err)
		}
		if d != original {
			t.Fatalf("失败时不应改写接收者: 原值 %s，实际 %s", original, d)
		}
	}
}

// TestIsOutOfRangeErrOnNonParseError 锁住判据对**非 time.ParseError** 的处理。
// ParseDate 内部只会遇到标准库的解析错误，但该判据若被后人复用到别处，
// 传入无关错误时必须返回 false（判为「形态不符」），而不是误判成值越界。
func TestIsOutOfRangeErrOnNonParseError(t *testing.T) {
	// 文本恰好含 "out of range" 的普通错误，不得被判为字段越界
	if isOutOfRangeErr(errors.New("some day out of range")) {
		t.Fatalf("非 *time.ParseError 不应被判为字段越界")
	}
	if isOutOfRangeErr(nil) {
		t.Fatalf("nil 不应被判为字段越界")
	}
}

// TestDateJSON 验证 dto 契约：序列化为 JSON **字符串**（而非对象或数字）。
func TestDateJSON(t *testing.T) {
	type payload struct {
		ExpenseDate Date `json:"expenseDate"`
	}
	in := payload{ExpenseDate: mustDate(t, 2026, time.January, 31)}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("序列化失败: %+v", err)
	}
	if want := `{"expenseDate":"2026-01-31"}`; string(raw) != want {
		t.Fatalf("JSON 形态不符: 期望 %s，实际 %s", want, raw)
	}
	var out payload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("反序列化失败: %+v", err)
	}
	if out.ExpenseDate != in.ExpenseDate {
		t.Fatalf("往返不一致: %s vs %s", in.ExpenseDate, out.ExpenseDate)
	}

	// 非法输入必须报错且可判档，供 dto 换 base/errs 文案键
	for _, body := range []string{`{"expenseDate":"2026-02-30"}`, `{"expenseDate":"2026/01/31"}`, `{"expenseDate":""}`} {
		var bad payload
		if err := json.Unmarshal([]byte(body), &bad); err == nil {
			t.Fatalf("非法 JSON %s 应报错", body)
		}
	}
}
