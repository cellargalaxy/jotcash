package decimal_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 decimal_test）模拟本包的真实调用方
// ——`rule/convert` 的不变式 1a 折算、`rule/amortize` 的 8.2 摊分、`store/sqlite` 的
// 定点文本往返、`api/http/dto` 的 JSON 契约，确认包对外契约真的可用。
//
// 另有一条独立校验路径：**绕过 Round**，直接用 math/big.Rat 按「半值远离零」的定义
// 自行回算，与 RoundAmount / RoundRate 的产物逐个比对——同一实现自己校验自己必然自洽，
// 只有独立回算才能证明舍入方向确实是中文四舍五入而非银行家舍入（后者 2.5→2，
// 两者仅在半值处分歧，而半值恰恰是账单金额里最常见的取值）。

import (
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
)

// isFloatKind 判断反射种类是否为浮点。
func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

// ratOf 把十进制文本解析为精确有理数，供独立回算使用（全程不经浮点）。
func ratOf(t *testing.T, text string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("基准回算：无法解析 %q", text)
	}
	return r
}

// roundHalfAwayFromZeroByRat 用有理数独立实现「按 scale 位半值远离零」舍入，
// 全程精确、不经浮点，作为比对基准。
// 算法：v * 10^scale 得到有理数 r，取 n = floor(|r|) 与余数 f；f ≥ 1/2 则进位；再带回符号。
func roundHalfAwayFromZeroByRat(t *testing.T, text string, scale int32) string {
	t.Helper()
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("基准回算：无法解析 %q", text)
	}
	neg := r.Sign() < 0
	r.Abs(r)

	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	r.Mul(r, new(big.Rat).SetInt(pow))

	n := new(big.Int).Quo(r.Num(), r.Denom()) // 向零截断（此处已取绝对值，即 floor）
	frac := new(big.Rat).Sub(r, new(big.Rat).SetInt(n))
	if frac.Cmp(big.NewRat(1, 2)) >= 0 {
		n.Add(n, big.NewInt(1))
	}

	// 把整数 n 还原为 scale 位定点文本
	digits := n.String()
	var out string
	if scale == 0 {
		out = digits
	} else {
		for int32(len(digits)) <= scale {
			digits = "0" + digits
		}
		cut := int32(len(digits)) - scale
		out = digits[:cut] + "." + digits[cut:]
	}
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out
}

// TestRoundingIsGenuineHalfAwayFromZero 不调用本包的比较逻辑：自行用 big.Rat 回算，
// 与 RoundAmount 的产物按定点文本逐字节比对。若实现悄悄换成银行家舍入或向上舍入，本用例即失败。
func TestRoundingIsGenuineHalfAwayFromZero(t *testing.T) {
	// 覆盖半值、非半值、正负、各币种位数
	texts := []string{
		"2.5", "-2.5", "3.5", "-3.5", "0.5", "-0.5",
		"0.005", "-0.005", "2.675", "-2.675",
		"1204.555", "-1204.555", "1204.5", "-1204.5",
		"9670.2005983584", "-9670.2005983584",
		"0.125", "0.135", "33.335", "1.0049", "1.005",
		"0", "1000",
	}
	scales := []int32{0, 1, 2, 3}
	for _, text := range texts {
		for _, scale := range scales {
			d, err := decimal.NewFromString(text)
			if err != nil {
				t.Fatalf("解析 %q 异常: %+v", text, err)
			}
			rounded, err := decimal.RoundAmount(d, scale)
			if err != nil {
				t.Fatalf("RoundAmount(%s, %d) 异常: %+v", text, scale, err)
			}
			// 用 FixedString 取定点文本，与基准同形态比对
			got, err := decimal.FixedString(rounded, scale)
			if err != nil {
				t.Fatalf("FixedString(%s, %d) 异常（舍入后应恰好 %d 位）: %+v", rounded.String(), scale, scale, err)
			}
			want := roundHalfAwayFromZeroByRat(t, text, scale)
			if got != want {
				t.Errorf("RoundAmount(%s, %d) = %v, 独立回算期望 %v（半值须远离零，非银行家舍入）",
					text, scale, got, want)
			}
		}
	}

	// 汇率 8 位同样独立回算
	for _, text := range []string{"7.834567891", "7.834567895", "7.1234567849", "0.000000004", "0.000000005"} {
		d, err := decimal.NewFromString(text)
		if err != nil {
			t.Fatalf("解析 %q 异常: %+v", text, err)
		}
		rate, err := decimal.RoundRate(d)
		if err != nil {
			t.Fatalf("RoundRate(%s) 异常: %+v", text, err)
		}
		got, err := decimal.StoreRate(rate)
		if err != nil {
			t.Fatalf("StoreRate(%s) 异常: %+v", rate.String(), err)
		}
		want := roundHalfAwayFromZeroByRat(t, text, decimal.RateScale)
		if got != want {
			t.Errorf("RoundRate(%s) 落库 = %v, 独立回算期望 %v", text, got, want)
		}
	}
}

// TestConvertFlowAsRuleConvert 模拟 rule/convert 的不变式 1a：
// 「汇率先规整 8 位 → 精确相乘 → 按本位币小数位四舍五入」，且乘法与舍入只发生这一处。
func TestConvertFlowAsRuleConvert(t *testing.T) {
	// 一笔 KWD（3 位小数，T38 不约束支出币种）支出折算为 CNY（2 位）本位币
	amount, err := decimal.NewFromString("1204.567")
	if err != nil {
		t.Fatalf("原币金额解析异常: %+v", err)
	}
	// 原币金额按支出币种小数位（口径①）
	amount, err = decimal.RoundAmount(amount, 3)
	if err != nil {
		t.Fatalf("原币金额舍入异常: %+v", err)
	}

	rawRate, err := decimal.NewFromString("23.9876543219") // 汇率源返回 10 位
	if err != nil {
		t.Fatalf("汇率解析异常: %+v", err)
	}
	rate, err := decimal.RoundRate(rawRate) // 口径③：先规整 8 位
	if err != nil {
		t.Fatalf("汇率规整异常: %+v", err)
	}
	if rate.Scale() > decimal.RateScale {
		t.Fatalf("规整后汇率小数位 %d 超过 %d", rate.Scale(), decimal.RateScale)
	}

	product, err := amount.Mul(rate) // 中途不舍
	if err != nil {
		t.Fatalf("折算相乘异常: %+v", err)
	}
	baseAmount, err := decimal.RoundAmount(product, 2) // 口径②「算」：按本位币小数位
	if err != nil {
		t.Fatalf("本位币金额舍入异常: %+v", err)
	}
	stored, err := decimal.StoreBaseAmount(baseAmount) // 口径②「存」：2 位定点
	if err != nil {
		t.Fatalf("本位币金额落库异常: %+v", err)
	}

	// 独立回算：用 big.Rat 精确算出 1204.567 × 23.98765432 再按 2 位舍入，
	// 不写死中间结果——手算的期望值本身就可能算错，那样测的就不是实现而是我的算术。
	exact := new(big.Rat).Mul(
		ratOf(t, "1204.567"),
		ratOf(t, "23.98765432"),
	)
	want := roundHalfAwayFromZeroByRat(t, exact.FloatString(20), 2)
	if stored != want {
		t.Errorf("折算落库 = %v, 独立回算期望 %v", stored, want)
	}
	// 顺带核验乘积本身精确无预舍入：3 + 8 = 11 位小数
	if product.Scale() != 11 {
		t.Errorf("精确乘积小数位 = %d, 期望 11（3 + 8，中途不得预舍）", product.Scale())
	}
	if product.String() != strings.TrimRight(exact.FloatString(11), "0") {
		t.Errorf("精确乘积 = %v, 独立回算 = %v", product.String(), exact.FloatString(11))
	}

	// I-4：原币 = 本位币时汇率取 1，折算结果与原币金额相等
	one := decimal.One()
	same, err := amount.Mul(one)
	if err != nil {
		t.Fatalf("汇率 1 折算异常: %+v", err)
	}
	if !same.Equal(amount) {
		t.Errorf("汇率取 1 折算后 = %v, 期望等于原币 %v", same.String(), amount.String())
	}
}

// TestAmortizeFlowAsRuleAmortize 模拟 rule/amortize 的 8.2：
// 「按币种小数位数均分、尾差归末月、原币与本位币各自独立均分」，两条链各自恒等。
func TestAmortizeFlowAsRuleAmortize(t *testing.T) {
	const months = 7
	// 原币 CNY 2 位，本位币 JPY 0 位——两条链位数不同，必须各自独立均分
	chains := []struct {
		name  string
		total string
		scale int32
	}{
		{"原币链（2 位）", "1000.01", 2},
		{"本位币链（0 位）", "160837", 0},
		{"负金额链（退款，2 位）", "-1000.01", 2},
	}
	for _, chain := range chains {
		total, err := decimal.NewFromString(chain.total)
		if err != nil {
			t.Fatalf("%s 总额解析异常: %+v", chain.name, err)
		}
		per, err := total.DivTruncate(decimal.NewFromInt(months), chain.scale)
		if err != nil {
			t.Fatalf("%s 均分异常: %+v", chain.name, err)
		}
		sum := decimal.Zero()
		for i := 0; i < months-1; i++ {
			// 每月金额必须能按该币种位数无损落库
			if _, err := decimal.FixedString(per, chain.scale); err != nil {
				t.Errorf("%s 第 %d 月金额 %v 无法按 %d 位无损表示: %+v", chain.name, i+1, per.String(), chain.scale, err)
			}
			sum = sum.Add(per)
		}
		last := total.Sub(sum) // 尾差归末月
		if _, err := decimal.FixedString(last, chain.scale); err != nil {
			t.Errorf("%s 末月金额 %v 无法按 %d 位无损表示: %+v", chain.name, last.String(), chain.scale, err)
		}
		if !sum.Add(last).Equal(total) {
			t.Errorf("%s 逐月之和 = %v, 期望恒等于总额 %v", chain.name, sum.Add(last).String(), total.String())
		}
		// 末月与前月的差额不应超过一个最小单位（尾差只应吸收零碎）
		diff := last.Sub(per).Abs()
		oneUnit, err := decimal.NewFromString("1")
		if err != nil {
			t.Fatalf("构造异常: %+v", err)
		}
		if chain.scale > 0 {
			oneUnit, err = decimal.NewFromString("0." + strings.Repeat("0", int(chain.scale)-1) + "1")
			if err != nil {
				t.Fatalf("构造最小单位异常: %+v", err)
			}
		}
		maxTail, err := oneUnit.Mul(decimal.NewFromInt(months))
		if err != nil {
			t.Fatalf("乘法异常: %+v", err)
		}
		if diff.Cmp(maxTail) > 0 {
			t.Errorf("%s 末月尾差 %v 过大，超过 %d 个最小单位", chain.name, diff.String(), months)
		}
		t.Logf("%s：每月 %v，末月 %v，合计恒等 %v", chain.name, per.String(), last.String(), total.String())
	}
}

// TestStoreRoundTripAsStoreSqlite 模拟 store/sqlite 的往返：
// 落库走定点文本（禁用 REAL），读回走 NewFromString，数值必须严格相等。
// 本包**有意不实现** driver.Valuer / sql.Scanner，强制存储层显式经这两个入口，
// 因此这条往返就是真实的落库路径。
func TestStoreRoundTripAsStoreSqlite(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		store func(decimal.Decimal) (string, error)
	}{
		{"本位币金额", "1204.5", decimal.StoreBaseAmount},
		{"本位币金额·整数", "1205", decimal.StoreBaseAmount},
		{"本位币金额·零", "0", decimal.StoreBaseAmount},
		{"本位币金额·负（退款）", "-1204.56", decimal.StoreBaseAmount},
		{"汇率", "7.83456789", decimal.StoreRate},
		{"汇率·取 1（I-4）", "1", decimal.StoreRate},
		{"汇率·下限", "0.00000001", decimal.StoreRate},
	}
	for _, c := range cases {
		original, err := decimal.NewFromString(c.text)
		if err != nil {
			t.Fatalf("%s 解析异常: %+v", c.name, err)
		}
		text, err := c.store(original)
		if err != nil {
			t.Errorf("%s 落库异常: %+v", c.name, err)
			continue
		}
		readBack, err := decimal.NewFromString(text)
		if err != nil {
			t.Errorf("%s 读回异常（落库文本 %q 必须能被本包解析）: %+v", c.name, text, err)
			continue
		}
		if !readBack.Equal(original) {
			t.Errorf("%s 往返后数值不等: %v → %q → %v", c.name, original.String(), text, readBack.String())
		}
		t.Logf("%s：%v → %q → %v", c.name, original.String(), text, readBack.String())
	}
}

// TestNoFloatInPublicAPI 约束 3「金额一律不用浮点」的可执行校验：
// 反射扫描本包所有导出方法与函数的签名，出现 float32/float64 即失败。
// 写成注释等于没写——只有出口面里根本没有浮点，才不可能出现「先经浮点中转」。
func TestNoFloatInPublicAPI(t *testing.T) {
	// 反射只能扫到方法集；包级函数无法反射枚举，故此处覆盖 Decimal 的全部导出方法，
	// 包级函数（Round/RoundAmount/RoundRate/Truncate/FixedString/Store*/New*）的签名
	// 由下方显式赋值给具名函数类型来钉住——类型不符即编译失败。
	var d decimal.Decimal
	typ := reflect.TypeOf(d)
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		sig := m.Type
		for j := 0; j < sig.NumIn(); j++ {
			if isFloatKind(sig.In(j).Kind()) {
				t.Errorf("导出方法 %s 的第 %d 个入参是浮点，违反「金额一律不用浮点」", m.Name, j)
			}
		}
		for j := 0; j < sig.NumOut(); j++ {
			if isFloatKind(sig.Out(j).Kind()) {
				t.Errorf("导出方法 %s 的第 %d 个返回值是浮点，违反「金额一律不用浮点」", m.Name, j)
			}
		}
	}

	// 包级函数签名钉死：若有人给它们加了 float 入参/返回值，这里当场编译不过
	var (
		_ func(string) (decimal.Decimal, error)                                    = decimal.NewFromString
		_ func(int64) decimal.Decimal                                              = decimal.NewFromInt
		_ func() decimal.Decimal                                                   = decimal.Zero
		_ func() decimal.Decimal                                                   = decimal.One
		_ func(decimal.Decimal, int32, decimal.RoundMode) (decimal.Decimal, error) = decimal.Round
		_ func(decimal.Decimal, int32) (decimal.Decimal, error)                    = decimal.RoundAmount
		_ func(decimal.Decimal) (decimal.Decimal, error)                           = decimal.RoundRate
		_ func(decimal.Decimal, int32) (decimal.Decimal, error)                    = decimal.Truncate
		_ func(decimal.Decimal, int32) (string, error)                             = decimal.FixedString
		_ func(decimal.Decimal) (string, error)                                    = decimal.StoreBaseAmount
		_ func(decimal.Decimal) (string, error)                                    = decimal.StoreRate
	)
}

// TestJSONContractAsDto 模拟 api/http/dto 的 JSON 契约（T34②）：
// 金额与汇率一律以字符串出入，不得是 JSON 数字——JS 的 number 是双精度浮点。
func TestJSONContractAsDto(t *testing.T) {
	type expenseDTO struct {
		Amount     decimal.Decimal `json:"amount"`
		BaseAmount decimal.Decimal `json:"baseAmount"`
		Rate       decimal.Decimal `json:"rate"`
	}
	amount, _ := decimal.NewFromString("1204.50")
	baseAmount, _ := decimal.NewFromString("9670.20")
	rate, _ := decimal.NewFromString("7.83456789")

	raw, err := json.Marshal(expenseDTO{Amount: amount, BaseAmount: baseAmount, Rate: rate})
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	got := string(raw)
	if strings.Contains(got, `:1204`) || strings.Contains(got, `:7.83`) {
		t.Errorf("序列化出现裸数字，JS 侧会失真: %v", got)
	}
	if got != `{"amount":"1204.5","baseAmount":"9670.2","rate":"7.83456789"}` {
		t.Errorf("序列化 = %v", got)
	}

	// 前端回传（含裸数字形态）必须能解析且不失真
	var back expenseDTO
	if err := json.Unmarshal([]byte(`{"amount":1204.50,"baseAmount":"9670.20","rate":7.83456789}`), &back); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	if !back.Amount.Equal(amount) || !back.BaseAmount.Equal(baseAmount) || !back.Rate.Equal(rate) {
		t.Errorf("反序列化后数值不等: %v / %v / %v", back.Amount.String(), back.BaseAmount.String(), back.Rate.String())
	}
	// 汇率经 JSON 往返后仍是 8 位可落库
	if _, err := decimal.StoreRate(back.Rate); err != nil {
		t.Errorf("JSON 往返后汇率无法落库: %+v", err)
	}
}
