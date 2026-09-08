package convert

import (
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件把三个上游包**已经写好**的消费方走查用例，原样喂给本包的真实 Apply。
//
// base/decimal、currency、entity 三个包各自留了一份「模拟 rule/convert」的
// 走查（它们的 consumer_check_test.go），里面手写了折算该怎么算、期望值是多少。
// 那些是本包动工前就定下的契约，且由不同的人在不同时间写成——若本包的实现与
// 它们对不上，说明至少有一方理解错了不变式 1a。
//
// 上游用的是自己手写的 convert 闭包，验证的是「本包提供的原料够不够用」；
// 这里换成真实的 Apply 跑同一批数据，验证的是「真实实现与契约一致」。
// 两者都跑通，才说明契约没有在落地时被悄悄改掉。

// TestConformsToDecimalConsumerCheck 复刻 base/decimal 的
// TestConsumerConvertRule（consumer_check_test.go:171）。
//
// 该走查用 baseScale 参数化，覆盖 0 / 1 / 2 位。本包的舍入基准来自真实币种，
// 故按位数取币种代入；1 位在当前币种表中无对应币种，改为直接验证算术等价
// （见下方说明）。
func TestConformsToDecimalConsumerCheck(t *testing.T) {
	// 按小数位数挑一个真实币种，作为该位数的代表
	byScale := map[int32]currency.Code{}
	for _, c := range currency.All() {
		if !c.CanBeBase() {
			continue
		}
		if _, ok := byScale[c.Scale()]; !ok {
			byScale[c.Scale()] = c
		}
	}

	cases := []struct {
		amount    string
		rawRate   string
		baseScale int32
		wantRate  string
		wantAmt   string
		desc      string
	}{
		{"100.00", "7.1234567891", 2, "7.12345679", "712.35", "本位币 2 位，汇率截到 8 位"},
		{"100.00", "7.1234567891", 0, "7.12345679", "712", "本位币 0 位"},
		{"100.00", "7.1234567891", 1, "7.12345679", "712.3", "本位币 1 位"},
		{"-100.00", "7.1234567891", 2, "7.12345679", "-712.35", "负金额（退款）"},
		{"0", "7.12345679", 2, "7.12345679", "0", "零金额"},
		{"100.00", "1", 2, "1", "100", "汇率为 1（本位币本身）"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			base, ok := byScale[c.baseScale]
			if !ok {
				// 当前币种表没有 1 位小数的候选币种。不跳过——那等于放弃这条契约；
				// 改为直接验证本包所用的算术与走查一致，位数取值本身由
				// TestRoundNeverFailsForAnyCurrency 兜住。
				rate := dec(c.rawRate).RoundRate()
				amt, err := dec(c.amount).Mul(rate).Round(c.baseScale, decimal.RoundHalfUp)
				if err != nil {
					t.Fatalf("算术验证失败: %v", err)
				}
				if rate.String() != c.wantRate || amt.String() != c.wantAmt {
					t.Errorf("%d 位: 得 (%s, %s), 期望 (%s, %s)",
						c.baseScale, rate, amt, c.wantRate, c.wantAmt)
				}
				return
			}

			got, err := Apply(Input{
				Amount:       dec(c.amount),
				Rate:         dec(c.rawRate),
				BaseCurrency: base,
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}
			if got.Rate.String() != c.wantRate {
				t.Errorf("规整后汇率 = %s, 期望 %s", got.Rate, c.wantRate)
			}
			if got.BaseAmount.String() != c.wantAmt {
				t.Errorf("折算金额 = %s, 期望 %s（本位币 %s）", got.BaseAmount, c.wantAmt, base)
			}
			// 走查同时要求：汇率能落 8 位列、金额能落 2 位定点列
			if !got.Rate.FitsScale(decimal.RateScale) {
				t.Errorf("汇率 %s 无法落 %d 位列", got.Rate, decimal.RateScale)
			}
			if !got.BaseAmount.FitsScale(decimal.BaseAmountStoreScale) {
				t.Errorf("金额 %s 无法落 %d 位定点列", got.BaseAmount, decimal.BaseAmountStoreScale)
			}
		})
	}
}

// TestConformsToCurrencyConsumerCheck 复刻 currency 的
// TestConsumerConvertRounding（consumer_check_test.go:56）。
func TestConformsToCurrencyConsumerCheck(t *testing.T) {
	cases := []struct {
		base   string
		amount string
		rate   string
		want   string
	}{
		{"CNY", "100.00", "7.1235", "712.35"},
		{"USD", "88.88", "1.0000", "88.88"},
		// 0 位本位币：舍到整数——位数取错就静默出错的地方
		{"JPY", "100.00", "1.5678", "157"},
		{"KRW", "10.00", "9.9999", "100"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			got, err := Apply(Input{
				Amount:       dec(tc.amount),
				Rate:         dec(tc.rate),
				BaseCurrency: cur(tc.base),
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("折算失败: %v", err)
			}
			if got.BaseAmount.String() != tc.want {
				t.Errorf("折算结果 = %s, 期望 %s", got.BaseAmount, tc.want)
			}
		})
	}

	// 走查末段：零值本位币必须被拦下，而不是静默按 0 位舍入
	if _, err := Apply(Input{
		Amount:       dec("100.00"),
		Rate:         dec("7.1235"),
		BaseCurrency: currency.Code{},
	}, TriggerRecalc); err == nil {
		t.Error("零值本位币应被拦下")
	}
}

// TestConformsToEntityConsumerCheck 复刻 entity 的
// TestConsumerConvertInvariant1a 与 TestConsumerConvertOnlyAmountChanged
// （consumer_check_test.go:41 与 :120）。
//
// entity 那边用 Expense 结构演示，本包不收 Expense（见包注释），故这里按
// 调用方实际会写的样子装配三元组——这同时也是本包的用法示例。
func TestConformsToEntityConsumerCheck(t *testing.T) {
	t.Run("CNY（2 位）折算后满足恒等式", func(t *testing.T) {
		got, err := Apply(Input{
			Amount:       dec("100.00"),
			Rate:         dec("7.12345678"),
			BaseCurrency: cur("CNY"),
		}, TriggerRateChanged)
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if !got.BaseAmount.Equal(dec("712.35")) {
			t.Errorf("本位币金额: got %s, want 712.35", got.BaseAmount)
		}
		if !got.BaseCurrency.Equal(cur("CNY")) {
			t.Error("折算本位币币种应被置为当前本位币（8.1）")
		}
	})

	t.Run("JPY（0 位）按本位币位数舍入而非固定 2 位", func(t *testing.T) {
		got, err := Apply(Input{
			Amount:       dec("100.00"),
			Rate:         dec("7.12345678"),
			BaseCurrency: cur("JPY"),
		}, TriggerRateChanged)
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if !got.BaseAmount.Equal(dec("712")) {
			t.Errorf("JPY 本位币金额: got %s, want 712", got.BaseAmount)
		}
		if !got.BaseAmount.FitsScale(decimal.BaseAmountStoreScale) {
			t.Error("按本位币位数算出的金额应能被 2 位定点列无损容纳（T38）")
		}
	})

	t.Run("负金额（退款）对称", func(t *testing.T) {
		got, err := Apply(Input{
			Amount:       dec("-100.00"),
			Rate:         dec("7.00000000"),
			BaseCurrency: cur("CNY"),
		}, TriggerRateChanged)
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if got.BaseAmount.Sign() >= 0 {
			t.Error("负金额折算后应仍为负（终版 §四 3：允许为负，作退款/冲正）")
		}
	})

	t.Run("改支出金额不改折算本位币币种（8.1 第 1 行）", func(t *testing.T) {
		// 一笔尚未收敛的行：折算本位币币种是 USD，当前本位币是 CNY
		got, err := Apply(Input{
			Amount:            dec("200.00"),
			Rate:              dec("7.00000000"),
			BaseCurrency:      cur("CNY"),
			PriorBaseCurrency: cur("USD"),
		}, TriggerAmountChanged)
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if !got.BaseCurrency.Equal(cur("USD")) {
			t.Errorf("折算本位币币种应保持 USD, 得 %s", got.BaseCurrency)
		}
		// 金额按 USD 的 2 位重算，恒等式仍成立
		if !got.BaseAmount.Equal(dec("1400")) {
			t.Errorf("本位币金额 = %s, 期望 1400", got.BaseAmount)
		}
	})
}

// TestAmortizeMonthsChangedMustNotClobberBaseAmount 锁死「改摊分月数不得抹掉
// 本位币金额」——8.1 第 6 行「本位币金额：不变」。
//
// # 这条用例防的是什么
//
// 本包的输出契约是「三字段同进退、整体写回」（Output 的注释、分层 §六 L2
// 「输出三元组含规整后的汇率，落库以它为准」）。而 8.1 第 6 行要求改摊分月数时
// 三字段全不动。这两句合在一起有一个陷阱：若「不动」也用一个看起来正常的
// Output 表达，按契约整体写回的调用方就会把 BaseAmount 写成那个 Output 里的值。
//
// 修复前 Apply 在该触发下回传 `{Rate: in.Rate, BaseAmount: 零值,
// BaseCurrency: in.PriorBaseCurrency}`，Changed 字段还不存在。实测后果：
// 一笔 100.00 × 7.12345678 = 712.35 的支出，用户只改了摊分月数（一个与金额
// 无关的字段），本位币金额就从 712.35 变成 0——恒等式 `本位币金额 = 支出金额
// × 折算汇率` 当场被打破，且全程不报错。
//
// 本用例以「一个遵守契约的调用方」的视角写：拿到 Output 后按 Changed 决定
// 是否写回，然后验证恒等式仍成立。把 Changed 判断去掉、或让 Apply 重新回传
// 一个非零的三元组，本用例必定失败。
func TestAmortizeMonthsChangedMustNotClobberBaseAmount(t *testing.T) {
	const (
		amount = "100.00"
		rate   = "7.12345678"
	)

	// 该行入库时已按 CNY 折算好，库里的三字段满足恒等式
	row := struct {
		amount     decimal.Decimal
		rate       decimal.Decimal
		baseAmount decimal.Decimal
		baseCode   currency.Code
	}{
		amount:   dec(amount),
		rate:     dec(rate),
		baseCode: cur("CNY"),
	}
	first, err := Apply(Input{
		Amount:       row.amount,
		Rate:         row.rate,
		BaseCurrency: row.baseCode,
	}, TriggerRateChanged)
	if err != nil {
		t.Fatalf("入库首次折算失败: %v", err)
	}
	if !first.Changed {
		t.Fatal("入库首次折算应产出新值，Changed 应为 true")
	}
	row.rate, row.baseAmount, row.baseCode = first.Rate, first.BaseAmount, first.BaseCurrency
	if !row.baseAmount.Equal(dec("712.35")) {
		t.Fatalf("前置条件不成立：本位币金额应为 712.35, 得 %s", row.baseAmount)
	}

	// 用户只改了摊分月数（8.1 第 6 行）：三字段必须全不动
	out, err := Apply(Input{
		Amount:            row.amount,
		Rate:              row.rate,
		BaseCurrency:      row.baseCode,
		PriorBaseCurrency: row.baseCode,
	}, TriggerAmortizeMonthsChanged)
	if err != nil {
		t.Fatalf("改摊分月数应恒不返回错误, 得 %v", err)
	}

	// 一个遵守「三字段同进退、整体写回」契约的调用方
	if out.Changed {
		row.rate, row.baseAmount, row.baseCode = out.Rate, out.BaseAmount, out.BaseCurrency
	}

	// 8.1 第 6 行：本位币金额不变、折算本位币币种不变
	if !row.baseAmount.Equal(dec("712.35")) {
		t.Errorf("改摊分月数后本位币金额 = %s, 期望仍为 712.35"+
			"（8.1 第 6 行「本位币金额：不变」）", row.baseAmount)
	}
	if !row.baseCode.Equal(cur("CNY")) {
		t.Errorf("改摊分月数后折算本位币币种 = %s, 期望仍为 CNY", row.baseCode)
	}
	if !row.rate.Equal(dec(rate)) {
		t.Errorf("改摊分月数后折算汇率 = %s, 期望仍为 %s", row.rate, rate)
	}

	// 恒等式复算：本位币金额 = 支出金额 × 折算汇率（按 CNY 的 2 位四舍五入）
	want, err := row.amount.Mul(row.rate).Round(row.baseCode.Scale(), decimal.RoundHalfUp)
	if err != nil {
		t.Fatalf("恒等式复算失败: %v", err)
	}
	if !row.baseAmount.Equal(want) {
		t.Errorf("恒等式失守：库里 本位币金额 = %s, 但 支出金额 × 折算汇率 = %s",
			row.baseAmount, want)
	}
}
