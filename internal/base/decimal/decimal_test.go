package decimal

// 包内单测：构造、算术、比较与序列化。
// 精度口径（三处）的用例在 precision_test.go，外部消费者验证在 zz_consumer_check_test.go。

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pkg/errors"
	shop "github.com/shopspring/decimal"
)

func TestNewFromString(t *testing.T) {
	// 合法输入：解析后的 String() 必须与期望一致
	valid := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"1.5", "1.5"},
		{"-1.5", "-1.5"},
		{"+1.5", "1.5"},
		{".5", "0.5"},
		{"1234.56", "1234.56"},
		{"0.00000001", "0.00000001"}, // 汇率 8 位下限
		{"7.83456789", "7.83456789"}, // 典型 8 位汇率
		{"1e3", "1000"},              // 科学计数法
		{"1.5e-2", "0.015"},          // 负指数
		{"1.50", "1.5"},              // String() 去尾零
		{"-0.005", "-0.005"},         // 负半值，供舍入对称性用例
		{"0.000000000000000001", "0.000000000000000001"}, // 恰好 18 位，边界内
	}
	for _, c := range valid {
		got, err := NewFromString(c.in)
		if err != nil {
			t.Errorf("NewFromString(%q) 异常: %+v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("NewFromString(%q).String() = %v, 期望 %v", c.in, got.String(), c.want)
		}
	}

	// 非法输入：必须报错且返回零值（调用方忽略 error 也拿不到脏值）
	invalid := []struct {
		in     string
		target error
	}{
		{"", ErrParse},
		{" 1.5", ErrParse},  // 前导空格：清洗归 parser，本包不 trim
		{"1.5 ", ErrParse},  // 尾随空格
		{"1_000", ErrParse}, // Go 数字字面量写法，非十进制文本
		{"0x10", ErrParse},
		{"abc", ErrParse},
		{"1.5.6", ErrParse},
		{"Inf", ErrParse},
		{"NaN", ErrParse},
		{"1,234.56", ErrParse},                 // 千分位须由 parser 清洗掉
		{"0.0000000000000000001", ErrOverflow}, // 19 位小数，超 MaxScale=18
	}
	for _, c := range invalid {
		got, err := NewFromString(c.in)
		if err == nil {
			t.Errorf("NewFromString(%q) 期望报错，实际得到 %v", c.in, got.String())
			continue
		}
		if !errors.Is(err, c.target) {
			t.Errorf("NewFromString(%q) 错误类型 = %+v, 期望可判定为 %v", c.in, err, c.target)
		}
		if !got.IsZero() {
			t.Errorf("NewFromString(%q) 失败时应返回零值，实际 %v", c.in, got.String())
		}
	}
}

// TestOverflowGuardDigits 有效数字超 MaxDigits=38 必须被入口挡住。
func TestOverflowGuardDigits(t *testing.T) {
	// 39 个 9
	s := ""
	for i := 0; i < 39; i++ {
		s += "9"
	}
	if _, err := NewFromString(s); !errors.Is(err, ErrOverflow) {
		t.Errorf("39 位有效数字期望 ErrOverflow，实际 %+v", err)
	}
	// 38 个 9 恰好在界内
	if _, err := NewFromString(s[:38]); err != nil {
		t.Errorf("38 位有效数字应可接受，实际异常 %+v", err)
	}
}

// TestZeroValueUsable 零值必须开箱可算：终版 §四 表头要求「不适用以零值表达，不留空」，
// 若零值不可用，entity 的每个金额字段都得先判空。
func TestZeroValueUsable(t *testing.T) {
	var z Decimal // 未经任何构造函数
	if z.String() != "0" {
		t.Errorf("零值 String() = %v, 期望 0", z.String())
	}
	if !z.IsZero() || z.Sign() != 0 || z.IsNegative() {
		t.Errorf("零值符号判定有误: IsZero=%v Sign=%v IsNegative=%v", z.IsZero(), z.Sign(), z.IsNegative())
	}
	if z.Scale() != 0 {
		t.Errorf("零值 Scale() = %v, 期望 0", z.Scale())
	}
	// 参与运算不得 panic
	if got := z.Add(NewFromInt(5)); got.String() != "5" {
		t.Errorf("零值 Add = %v, 期望 5", got.String())
	}
	if got := z.Sub(NewFromInt(5)); got.String() != "-5" {
		t.Errorf("零值 Sub = %v, 期望 -5", got.String())
	}
	if got, err := z.Mul(NewFromInt(5)); err != nil || !got.IsZero() {
		t.Errorf("零值 Mul = %v, err=%+v, 期望 0", got.String(), err)
	}
	if got := z.Neg(); !got.IsZero() {
		t.Errorf("零值 Neg = %v, 期望 0", got.String())
	}
	if got := z.Abs(); !got.IsZero() {
		t.Errorf("零值 Abs = %v, 期望 0", got.String())
	}
	if got, err := Round(z, 2, RoundHalfUp); err != nil || !got.IsZero() {
		t.Errorf("零值 Round = %v, err=%+v", got.String(), err)
	}
	// 零值落库形态必须补零，而非 "0"
	if got, err := StoreBaseAmount(z); err != nil || got != "0.00" {
		t.Errorf("零值 StoreBaseAmount = %v, err=%+v, 期望 0.00", got, err)
	}
	// 零值作除数应报错而非 panic
	if _, err := NewFromInt(1).DivRound(z, 2); !errors.Is(err, ErrDivZero) {
		t.Errorf("零值作除数期望 ErrDivZero，实际 %+v", err)
	}
}

func TestArithmetic(t *testing.T) {
	// 精确性：这些值若经 float64 中转必然失真
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"加法不失真", MustFromString("0.1").Add(MustFromString("0.2")).String(), "0.3"},
		{"减法不失真", MustFromString("0.3").Sub(MustFromString("0.1")).String(), "0.2"},
		{"负金额相加（退款冲正）", MustFromString("100.50").Add(MustFromString("-100.50")).String(), "0"},
		{"取负", MustFromString("1.23").Neg().String(), "-1.23"},
		{"绝对值", MustFromString("-1.23").Abs().String(), "1.23"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, 期望 %v", c.name, c.got, c.want)
		}
	}

	// 乘法精确、不预舍入（不变式 1a 要求中途不舍）
	product, err := MustFromString("123.45").Mul(MustFromString("7.12345678"))
	if err != nil {
		t.Fatalf("乘法异常: %+v", err)
	}
	if product.String() != "879.390739491" {
		t.Errorf("乘积 = %v, 期望 879.390739491（精确值，未预舍入）", product.String())
	}
}

// TestMulOverflowNotPanic 底层 Mul 在指数和越界时会 panic，本包必须转成 error。
// 本包位于 L0、被全仓库依赖，对外零 panic 是硬要求。
func TestMulOverflowNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Mul 溢出时发生 panic，本包不得对外 panic: %v", r)
		}
	}()
	// 直接构造极端指数（绕过 NewFromString 的入口校验，模拟内部运算累积到边界）
	huge := wrap(shop.New(1, -2000000000))
	if _, err := huge.Mul(huge); !errors.Is(err, ErrOverflow) {
		t.Errorf("极端指数相乘期望 ErrOverflow，实际 %+v", err)
	}
}

func TestCompare(t *testing.T) {
	a := MustFromString("1.50")
	b := MustFromString("1.5")
	// 忽略标度：库里是 2 位、内存里 1 位，必须判等，否则 I-7 幂等判断会误判
	if !a.Equal(b) {
		t.Errorf("Equal(1.50, 1.5) = false, 期望 true（忽略标度差异）")
	}
	if a.Cmp(b) != 0 {
		t.Errorf("Cmp(1.50, 1.5) = %v, 期望 0", a.Cmp(b))
	}
	if MustFromString("1.4").Cmp(b) != -1 {
		t.Errorf("Cmp(1.4, 1.5) 期望 -1")
	}
	if MustFromString("1.6").Cmp(b) != 1 {
		t.Errorf("Cmp(1.6, 1.5) 期望 1")
	}
	if !MustFromString("-0.01").IsNegative() {
		t.Errorf("-0.01 应判为负（负金额是合法值，表示退款/冲正）")
	}
	if MustFromString("1.500").Scale() != 3 {
		t.Errorf("Scale(1.500) = %v, 期望 3", MustFromString("1.500").Scale())
	}
	// 指数为正时按 0 位计
	if MustFromString("1e3").Scale() != 0 {
		t.Errorf("Scale(1e3) = %v, 期望 0", MustFromString("1e3").Scale())
	}
}

// TestNotComparable 类型必须不可比较：内含 *big.Int，== 比的是指针，
// 同一数值经不同路径构造会静默得到 false。误用应是编译错误，而非运行期错值。
func TestNotComparable(t *testing.T) {
	if reflect.TypeOf(Decimal{}).Comparable() {
		t.Errorf("Decimal 可用 == 比较，会导致金额比较静默出错；应保持不可比较")
	}
}

func TestJSON(t *testing.T) {
	// 序列化必须带引号：JS number 是双精度浮点，8 位汇率与大额金额会失真
	d := MustFromString("1234.56")
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	if string(data) != `"1234.56"` {
		t.Errorf("MarshalJSON = %v, 期望 \"1234.56\"（字符串形态，避免 JS 浮点失真）", string(data))
	}

	// 反序列化接受两种形态，且裸数字不经浮点中转
	forms := []struct {
		name string
		in   string
		want string
	}{
		{"字符串形态", `"1234.56"`, "1234.56"},
		{"裸数字形态", `1234.56`, "1234.56"},
		{"裸数字·浮点不可精确表示的值", `0.1`, "0.1"},
		{"8 位汇率", `"7.83456789"`, "7.83456789"},
		{"负值", `"-100.50"`, "-100.5"},
	}
	for _, c := range forms {
		var got Decimal
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Errorf("%s 反序列化异常: %+v", c.name, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("%s 反序列化 = %v, 期望 %v", c.name, got.String(), c.want)
		}
	}

	// null 保持接收者不变
	keep := MustFromString("9.9")
	if err := json.Unmarshal([]byte("null"), &keep); err != nil {
		t.Errorf("null 反序列化异常: %+v", err)
	}
	if keep.String() != "9.9" {
		t.Errorf("null 反序列化后 = %v, 期望保持 9.9", keep.String())
	}

	// 非法 JSON 值必须报错，不得静默变成零值
	var bad Decimal
	if err := json.Unmarshal([]byte(`"abc"`), &bad); !errors.Is(err, ErrParse) {
		t.Errorf("非法 JSON 值期望 ErrParse，实际 %+v", err)
	}

	// 往返：结构体字段形态（模拟 dto 契约）
	type payload struct {
		Amount Decimal `json:"amount"`
		Rate   Decimal `json:"rate"`
	}
	in := payload{Amount: MustFromString("1204.50"), Rate: MustFromString("7.83456789")}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("往返序列化异常: %+v", err)
	}
	if string(raw) != `{"amount":"1204.5","rate":"7.83456789"}` {
		t.Errorf("往返序列化 = %v", string(raw))
	}
	var out payload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("往返反序列化异常: %+v", err)
	}
	if !out.Amount.Equal(in.Amount) || !out.Rate.Equal(in.Rate) {
		t.Errorf("往返后数值不等: amount %v vs %v, rate %v vs %v",
			out.Amount.String(), in.Amount.String(), out.Rate.String(), in.Rate.String())
	}
}

func TestConstructors(t *testing.T) {
	if NewFromInt(0).String() != "0" {
		t.Errorf("NewFromInt(0) = %v, 期望 0", NewFromInt(0).String())
	}
	if NewFromInt(-37).String() != "-37" {
		t.Errorf("NewFromInt(-37) = %v, 期望 -37", NewFromInt(-37).String())
	}
	if !Zero().IsZero() {
		t.Errorf("Zero() 应为零")
	}
	// One() 对应 I-4「原币 = 本位币时汇率取 1」
	if One().String() != "1" {
		t.Errorf("One() = %v, 期望 1", One().String())
	}
}

func TestMustFromString(t *testing.T) {
	if MustFromString("1.5").String() != "1.5" {
		t.Errorf("MustFromString 合法输入结果有误")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustFromString 非法输入应 panic（仅供包内常量与测试使用）")
		}
	}()
	MustFromString("not-a-number")
}
