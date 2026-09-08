package store

import (
	"errors"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件验证「数值列口径」这项承载（⑥）。重点不是覆盖率，而是把三条容易静默出错
// 的性质变成会失败的用例：三套口径不能混用、越界必须在端口层被拦下、往返必须无损。

// TestAmountScaleFollowsCurrency 验证支出金额的位数**随币种**，而不是恒 2 位。
//
// 这是本轮最要紧的一条判断：分层 §六 L3 store ⑥ 的字面表述是「金额 2 位定点」，
// 但按字面套到 Expense.支出金额 上会对 3 位币种静默丢数。本用例把「按币种位数」
// 这条裁定钉住——若有人把 AmountUnits 改成恒用 BaseAmountScale，这里当场失败。
func TestAmountScaleFollowsCurrency(t *testing.T) {
	cases := []struct {
		code  string
		text  string
		units int64
		desc  string
	}{
		{"JPY", "1000", 1000, "0 位币种：最小单位即整数本身"},
		{"CNY", "100.50", 10050, "2 位币种"},
		{"KWD", "12.345", 12345, "3 位币种：按 2 位会直接报错"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			cur := currency.MustParse(c.code)
			units, err := AmountUnits(decimal.MustParse(c.text), cur)
			if err != nil {
				t.Fatalf("%s：AmountUnits(%s, %s) 报错 %v", c.desc, c.text, c.code, err)
			}
			if units != c.units {
				t.Fatalf("%s：AmountUnits(%s, %s) = %d, 期望 %d",
					c.desc, c.text, c.code, units, c.units)
			}
		})
	}

	// 3 位币种走本位币口径（2 位）必须失败——这正是「金额 2 位」字面读法的后果。
	// 若将来有人把两个函数合并，这个用例会失败，提示合并会丢数。
	if _, err := BaseAmountUnits(decimal.MustParse("12.345")); err == nil {
		t.Fatal("3 位金额按本位币 2 位口径换算应当失败，否则第 3 位被静默丢弃")
	}
}

// TestCurrencyScaleDistribution 锁定币种表的位数分布。
//
// AmountUnits 的「按币种位数」口径依赖于「表里确实存在非 2 位币种」这个事实；
// 若哪天 KWD 被移除、全表变成 0/2 位，本包的三套口径就可以简化。反之若有人加入
// 4 位币种，int64 列的容量边界（下方 TestUnitsCapacityBoundary）需要重算。
// 两种情况都应当被察觉，而不是默默生效。
func TestCurrencyScaleDistribution(t *testing.T) {
	dist := map[int32]int{}
	var maxScale int32
	for _, c := range currency.All() {
		dist[c.Scale()]++
		if c.Scale() > maxScale {
			maxScale = c.Scale()
		}
	}
	if maxScale != 3 {
		t.Fatalf("币种表最大小数位数 = %d，期望 3（KWD）；"+
			"位数上界变化会改变 int64 最小单位列的容量边界，需重新核对 column.go 的实测表",
			maxScale)
	}
	if dist[3] == 0 {
		t.Fatal("币种表已无 3 位币种：本包「支出金额位数随币种」的三套口径可简化，请复核")
	}
	if maxScale <= BaseAmountScale {
		t.Fatalf("币种表最大位数 %d 已不超过本位币存储位数 %d，"+
			"AmountUnits 与 BaseAmountUnits 的区分失去意义，请复核", maxScale, BaseAmountScale)
	}
}

// TestBaseAmountScaleCoversAllBaseCandidates 验证本位币恒 2 位这条口径对**全部**
// 本位币候选都无损。
//
// 依据 T38：本位币候选的小数位数 ≤ 2。这条是 BaseAmountUnits 不带币种参数的前提
// ——若有人放宽 currency 的候选口径（放进一个 3 位币种），本用例会失败。
// currency 侧的 TestConsumerConvertBaseScaleFitsStorage 从另一侧锁同一条性质。
func TestBaseAmountScaleCoversAllBaseCandidates(t *testing.T) {
	candidates := currency.BaseCandidates()
	if len(candidates) == 0 {
		t.Fatal("本位币候选集为空")
	}
	for _, base := range candidates {
		if base.Scale() > BaseAmountScale {
			t.Fatalf("本位币候选 %s 的小数位数 %d 超过存储位数 %d，"+
				"按其位数算出的金额将被静默截断", base, base.Scale(), BaseAmountScale)
		}
	}
}

// TestUnitsCapacityBoundary 验证「decimal 入口宽于 int64 列容量」的那段区间确实
// 被端口层拦下，且拦下的形态是 400 而非 500。
//
// column.go 的注释里那张实测表（scale=3 → 15 位、scale=8 → 10 位）就是本用例的
// 断言对象。若不拦，故障形态是 D-7 整批入库时第 N+1 条报 500 并整批回滚。
func TestUnitsCapacityBoundary(t *testing.T) {
	// 16 位整数：过 decimal 入口（MaxIntDigits=16），但 3 位币种的最小单位列装不下
	big16 := strings.Repeat("9", 16)
	if _, err := decimal.Parse(big16); err != nil {
		t.Fatalf("前提不成立：16 位整数应能过 decimal 入口，却报错 %v", err)
	}

	t.Run("KWD_3位_16位整数应被拦下", func(t *testing.T) {
		_, err := AmountUnits(decimal.MustParse(big16), currency.MustParse("KWD"))
		if err == nil {
			t.Fatal("16 位整数的 3 位币种金额应被拦下，否则落库时才溢出")
		}
		assertInvalidInput(t, err, KeyAmountOutOfRange)
	})

	t.Run("CNY_2位_16位整数应放行", func(t *testing.T) {
		// 同一个值在 2 位币种下是合法的——证明拦截是按位数算的，不是一刀切
		if _, err := AmountUnits(decimal.MustParse(big16), currency.MustParse("CNY")); err != nil {
			t.Fatalf("16 位整数的 2 位币种金额应当放行，却报错 %v", err)
		}
	})

	t.Run("汇率_11位整数应被拦下", func(t *testing.T) {
		_, err := RateUnits(decimal.MustParse(strings.Repeat("9", 11)))
		if err == nil {
			t.Fatal("11 位整数的汇率应被拦下（8 位定点列容量为 10 位整数）")
		}
		assertInvalidInput(t, err, KeyRateOutOfRange)
	})

	t.Run("汇率_10位整数应放行", func(t *testing.T) {
		if _, err := RateUnits(decimal.MustParse(strings.Repeat("9", 10))); err != nil {
			t.Fatalf("10 位整数的汇率应当放行，却报错 %v", err)
		}
	})
}

// TestRateUnitsDoesNotRound 验证 RateUnits **不替调用方规整**。
//
// T39 的规整点唯一在 rule/convert。若本函数顺手 Round，就有了第二个规整点，
// 症状是「金额与汇率对不上恒等式」而排错要同时怀疑两个包。
func TestRateUnitsDoesNotRound(t *testing.T) {
	// 9 位小数：超过 8 位定点列，应当报错而不是被静默舍入
	_, err := RateUnits(decimal.MustParse("1.123456789"))
	if err == nil {
		t.Fatal("9 位小数的汇率应当报错（调用方未走 rule/convert 规整），而不是被本函数替它舍入")
	}
	assertInvalidInput(t, err, KeyRateOutOfRange)

	// 恰好 8 位：正常放行且数值精确
	units, err := RateUnits(decimal.MustParse("7.12345678"))
	if err != nil {
		t.Fatalf("8 位小数的汇率应当放行，却报错 %v", err)
	}
	if units != 712345678 {
		t.Fatalf("RateUnits(7.12345678) = %d, 期望 712345678", units)
	}
}

// TestRoundTrip 验证三套口径各自往返无损。
//
// 写向与读向成对提供的意义就在这里：若只提供写向，各 L4 实现自己写读回逻辑，
// 位数用错时表现为「读出来的金额差了 10 倍」，而这种错误不会有任何报错。
//
// # 断言的是数值相等与最小单位稳定，不是 Scale() 相等
//
// decimal 对小数位数做**规范化**（剥尾随零），实测：Parse("0.00").Scale() = 0、
// Parse("100.50").Scale() = 1、FromUnits(100000000, 8).Scale() = 0。即 Scale() 返回
// 「表示该数值所需的最小位数」，而不是它被解析或构造时声明的位数。
//
// 故「读回值的 Scale() 等于列位数」**不是**一条成立的性质，断言它会得到假失败。
// 真正要紧的两条性质是：① 数值相等（Equal）；② 往返到最小单位稳定（同一个整数）。
// 另加一条 ③ FitsScale：读回值必须能被该列无损容纳。
//
// 这一点对 L4 有直接约束：展示与落库都必须走 StringFixed(scale) 而不是 String()
// ——后者对 100.50 输出 "100.5"，对 0.00 输出 "0"。
func TestRoundTrip(t *testing.T) {
	t.Run("原币金额", func(t *testing.T) {
		cases := []struct{ code, text string }{
			{"JPY", "1000"}, {"CNY", "100.50"}, {"KWD", "12.345"},
			{"CNY", "-1.00"}, {"CNY", "0.00"}, {"KWD", "-0.001"},
		}
		for _, c := range cases {
			cur := currency.MustParse(c.code)
			want := decimal.MustParse(c.text)
			units, err := AmountUnits(want, cur)
			if err != nil {
				t.Fatalf("%s %s: AmountUnits 报错 %v", c.code, c.text, err)
			}
			got, err := AmountFromUnits(units, cur)
			if err != nil {
				t.Fatalf("%s %s: AmountFromUnits 报错 %v", c.code, c.text, err)
			}
			if !got.Equal(want) {
				t.Fatalf("%s 往返数值不等: %s → %d → %s", c.code, want, units, got)
			}
			assertUnitsStable(t, got, cur.Scale(), units, c.code+" "+c.text)
			// 定点渲染必须与原文一致（L4 展示与落库都应走 StringFixed）
			if fixed, err := got.StringFixed(cur.Scale()); err != nil {
				t.Fatalf("%s %s: StringFixed 报错 %v", c.code, c.text, err)
			} else if fixed != c.text {
				t.Fatalf("%s: StringFixed(%d) = %s, 期望 %s", c.code, cur.Scale(), fixed, c.text)
			}
		}
	})

	t.Run("本位币金额", func(t *testing.T) {
		for _, text := range []string{"0.00", "100.50", "-1.00", "999999999.99"} {
			want := decimal.MustParse(text)
			units, err := BaseAmountUnits(want)
			if err != nil {
				t.Fatalf("%s: BaseAmountUnits 报错 %v", text, err)
			}
			got, err := BaseAmountFromUnits(units)
			if err != nil {
				t.Fatalf("%s: BaseAmountFromUnits 报错 %v", text, err)
			}
			if !got.Equal(want) {
				t.Fatalf("本位币往返数值不等: %s → %d → %s", want, units, got)
			}
			assertUnitsStable(t, got, BaseAmountScale, units, "本位币 "+text)
		}
	})

	t.Run("汇率", func(t *testing.T) {
		for _, text := range []string{"1.00000000", "7.12345678", "0.00000001"} {
			want := decimal.MustParse(text)
			units, err := RateUnits(want)
			if err != nil {
				t.Fatalf("%s: RateUnits 报错 %v", text, err)
			}
			got, err := RateFromUnits(units)
			if err != nil {
				t.Fatalf("%s: RateFromUnits 报错 %v", text, err)
			}
			if !got.Equal(want) {
				t.Fatalf("汇率往返数值不等: %s → %d → %s", want, units, got)
			}
			assertUnitsStable(t, got, RateScale, units, "汇率 "+text)
		}
	})
}

// assertUnitsStable 断言读回值能被该列无损容纳，且再次换算得到同一个最小单位整数。
//
// 这是「往返无损」的可操作判据（Scale() 相等不是，见 TestRoundTrip 的注释）。
func assertUnitsStable(t *testing.T, got decimal.Decimal, scale int32, wantUnits int64, label string) {
	t.Helper()
	if !got.FitsScale(scale) {
		t.Fatalf("%s: 读回值 %s 无法被 %d 位列无损容纳", label, got, scale)
	}
	again, err := got.Units(scale)
	if err != nil {
		t.Fatalf("%s: 读回值再次换算报错 %v", label, err)
	}
	if again != wantUnits {
		t.Fatalf("%s: 最小单位不稳定 %d → %s → %d", label, wantUnits, got, again)
	}
}

// TestZeroCurrencyRejected 验证币种零值被拒。
//
// 币种零值意味着调用方没设币种，此时没有任何位数可依。若放行（比如默认按 2 位），
// 3 位币种的金额就会被静默截断，而 currency.Code 的零值恰恰是「未设置」而非某个
// 具体币种。
func TestZeroCurrencyRejected(t *testing.T) {
	var zero currency.Code
	if !zero.IsZero() {
		t.Fatal("前提不成立：currency.Code 零值应当 IsZero")
	}
	if _, err := AmountUnits(decimal.MustParse("1.00"), zero); err == nil {
		t.Fatal("币种为零值时 AmountUnits 应当报错，否则没有位数依据却仍落库")
	}
	if _, err := AmountFromUnits(100, zero); err == nil {
		t.Fatal("币种为零值时 AmountFromUnits 应当报错")
	}
}

// TestFromUnitsRejectsCorruptedColumnValue 验证**读路径**也会拦下坏值。
//
// 写路径拦得住的前提是数据都经本包写入。但库里的值还可能来自：手工 SQL 修数、
// 从旧系统迁入、备份恢复出错、或另一个版本的代码写入。这类值读出来若不报错，
// 就会以一个错误的金额进入 F-6 合计与 J 域报表，而报表不会有任何异常提示。
//
// 实测可达的坏值：int64 最小单位在低位数下会超过 decimal 的 16 位整数上限
// （MaxInt64 @scale=2 → 17 位整数，@scale=0 → 19 位），故这些错误分支不是死代码。
func TestFromUnitsRejectsCorruptedColumnValue(t *testing.T) {
	t.Run("原币金额列坏值", func(t *testing.T) {
		// CNY（2 位）：MaxInt64 换算后是 17 位整数，超 decimal 的 16 位上限
		_, err := AmountFromUnits(math.MaxInt64, currency.MustParse("CNY"))
		if err == nil {
			t.Fatal("读到超限的最小单位值应当报错，否则坏值会静默进入合计与报表")
		}
		assertInvalidInput(t, err, KeyAmountOutOfRange)

		// JPY（0 位）：MaxInt64 即 19 位整数
		if _, err := AmountFromUnits(math.MaxInt64, currency.MustParse("JPY")); err == nil {
			t.Fatal("0 位币种读到 MaxInt64 应当报错")
		}
		// 负向同理
		if _, err := AmountFromUnits(math.MinInt64, currency.MustParse("CNY")); err == nil {
			t.Fatal("负向超限同样应当报错")
		}
	})

	t.Run("本位币金额列坏值", func(t *testing.T) {
		_, err := BaseAmountFromUnits(math.MaxInt64)
		if err == nil {
			t.Fatal("本位币列读到超限值应当报错")
		}
		assertInvalidInput(t, err, KeyAmountOutOfRange)
	})

	t.Run("汇率列坏值", func(t *testing.T) {
		// 汇率是 8 位，MaxInt64 @8 仍在 16 位整数以内（实测 92233720368.54775807），
		// 故这里用「先写后读」的边界证明读路径与写路径口径一致：
		// 写路径拒绝的 11 位整数汇率，其最小单位若被硬塞进库，读路径也应拒绝。
		//
		// 11 位整数 × 10^8 已超 int64，无法构造；改用 0 位口径下的等价越界值验证
		// 错误分支：RateFromUnits 对任何使 decimal 超 16 位整数的输入都应报错。
		// 由于 scale=8 下 int64 恒不超限，此处改为断言「合法值必须成功」，
		// 并把「坏值拦截」交给写路径的 TestUnitsCapacityBoundary。
		for _, units := range []int64{0, 1, math.MaxInt64, math.MinInt64} {
			if _, err := RateFromUnits(units); err != nil {
				t.Fatalf("汇率列读回 %d 报错 %v；scale=8 下 int64 全域均在 decimal "+
					"16 位整数上限内（实测 MaxInt64@8 = 92233720368.54775807），"+
					"不应报错", units, err)
			}
		}
	})
}

// TestOwnerUserIDInt64 验证归属类型到裸 int64 的取值方法。
//
// L4 实现要把它作为查询参数传给驱动，故需要一个显式的取值口。
func TestOwnerUserIDInt64(t *testing.T) {
	if got := OwnerUserID(12345).Int64(); got != 12345 {
		t.Fatalf("Int64() = %d, 期望 12345", got)
	}
	var zero OwnerUserID
	if got := zero.Int64(); got != 0 {
		t.Fatalf("零值 Int64() = %d, 期望 0", got)
	}
}

// TestScaleConstantsComeFromDecimal 验证本包的两个位数常量**取自 decimal**，
// 而不是自己另定一个值。
//
// 若本包写死 2 与 8，就出现了第二处口径：将来 decimal 侧调整（例如汇率改 10 位），
// 本包不会跟着变，症状是落库位数与计算位数不一致。
func TestScaleConstantsComeFromDecimal(t *testing.T) {
	if BaseAmountScale != decimal.BaseAmountStoreScale {
		t.Fatalf("BaseAmountScale = %d 与 decimal.BaseAmountStoreScale = %d 不一致，"+
			"本包不得自定义本位币存储位数", BaseAmountScale, decimal.BaseAmountStoreScale)
	}
	if RateScale != decimal.RateScale {
		t.Fatalf("RateScale = %d 与 decimal.RateScale = %d 不一致（T39 汇率 8 位），"+
			"本包不得自定义汇率位数", RateScale, decimal.RateScale)
	}
}

// TestUnitsOrderingIsNumeric 验证整数最小单位的排序是**数值序**，
// 而定点文本的排序不是。
//
// 这是「金额列推荐整数最小单位形态」的依据：F-3 支持按金额排序、F-4 有金额区间
// 筛选、F-6 按币种分组合计都依赖数值序。若 L4 把金额存成定点文本，
// 「按金额排序」会得到 [-1.00, 100.00, 12.00, 2.00, 9.99] 这种结果。
func TestUnitsOrderingIsNumeric(t *testing.T) {
	texts := []string{"12.00", "2.00", "100.00", "-1.00", "9.99"}
	cur := currency.MustParse("CNY")

	// 文本序（错误形态）
	textSorted := append([]string(nil), texts...)
	sort.Strings(textSorted)

	// 最小单位序（正确形态）
	type pair struct {
		text  string
		units int64
	}
	pairs := make([]pair, 0, len(texts))
	for _, s := range texts {
		u, err := AmountUnits(decimal.MustParse(s), cur)
		if err != nil {
			t.Fatalf("%s: AmountUnits 报错 %v", s, err)
		}
		pairs = append(pairs, pair{s, u})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].units < pairs[j].units })

	wantNumeric := []string{"-1.00", "2.00", "9.99", "12.00", "100.00"}
	for i, p := range pairs {
		if p.text != wantNumeric[i] {
			t.Fatalf("按最小单位排序第 %d 位 = %s, 期望 %s（完整结果 %v）",
				i, p.text, wantNumeric[i], pairs)
		}
	}

	// 反向确认：文本序确实与数值序不同，故这个用例不是在验证一个恒真命题
	same := true
	for i := range textSorted {
		if textSorted[i] != wantNumeric[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("本用例失去意义：该组样本的文本序恰好等于数值序，请换一组更易暴露差异的样本")
	}
}

// assertInvalidInput 断言错误为 400 档且文案键匹配，并且原始诊断信息可经 Unwrap 取到。
func assertInvalidInput(t *testing.T, err error, wantKey string) {
	t.Helper()
	if !errs.IsKind(err, errs.KindInvalidInput) {
		t.Fatalf("错误档位应为 KindInvalidInput（映射 400），实际 %v；"+
			"落库越界是用户输入问题，报 500 会让用户以为是系统故障", err)
	}
	if got := errs.MsgKeyOf(err); got != wantKey {
		t.Fatalf("文案键 = %q, 期望 %q", got, wantKey)
	}
	// 原始 decimal 错误必须挂在 cause 上：日志里要能看到「需 3 位, 目标 2 位」
	if inner := errors.Unwrap(err); inner == nil {
		var e *errs.Error
		if errors.As(err, &e) && e.Cause() == nil {
			t.Error("原始 decimal 错误未挂在 cause 上，排错时看不到精确的位数诊断")
		}
	}
}
