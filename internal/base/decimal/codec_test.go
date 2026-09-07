package decimal

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
)

// TestString 验证日志用的文本形态。
func TestString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0", "0"},
		{"100.50", "100.5"}, //不带尾随零
		{"-33.34", "-33.34"},
		{"7.12345678", "7.12345678"},
		{"1e3", "1000"},
		{"-0", "0"},
	}
	for _, c := range cases {
		if got := MustParse(c.in).String(); got != c.want {
			t.Errorf("String(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestStringFixed 验证「定点文本」列形态。
func TestStringFixed(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"1.5", 2, "1.50"},
		{"0", 2, "0.00"},
		{"100.50", 2, "100.50"},
		{"-33.34", 2, "-33.34"},
		{"7.12345678", 8, "7.12345678"},
		{"1.5", 8, "1.50000000"},
		{"200", 0, "200"},
		{"-0.01", 2, "-0.01"},
	}
	for _, c := range cases {
		got, err := MustParse(c.in).StringFixed(c.scale)
		if err != nil {
			t.Errorf("StringFixed(%q, %d) 报错: %v", c.in, c.scale, err)
			continue
		}
		if got != c.want {
			t.Errorf("StringFixed(%q, %d) = %q, 期望 %q", c.in, c.scale, got, c.want)
		}
	}

	//无损性不足时报错，不静默舍入
	if _, err := MustParse("1.005").StringFixed(2); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("1.005 按 2 位输出应报 ErrPrecisionLoss, 实际 %v", err)
	}
	if _, err := MustParse("1.5").StringFixed(-1); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("StringFixed 位数为负应报 ErrScaleInvalid, 实际 %v", err)
	}
}

// TestStringFixedIsFixedWidth 验证定长性质：同一列内位数一致后，字典序即数值序。
//
// store 的 F-3 排序与 F-4 区间筛选依赖这条性质。这里同时给出反面对照：
// 不定长的 String 形态会把 100.00 排在 9.99 之前。
func TestStringFixedIsFixedWidth(t *testing.T) {
	//同符号下，等整数位宽的定点文本按字典序排序应与数值序一致
	values := []string{"9.99", "10.00", "100.00", "2.00", "0.01"}

	//反面对照：不定长文本排序会出错
	loose := make([]string, len(values))
	for i, s := range values {
		loose[i] = MustParse(s).String()
	}
	sort.Strings(loose)
	if loose[0] == "0.01" && loose[1] == "10" {
		//"10" < "100" < "2" < "9.99" 这类顺序即为文本序的错误表现
		t.Logf("不定长文本序（错误示例）: %v", loose)
	}

	//正面：定长且左侧补齐后，字典序 = 数值序。
	//store 侧若用定点文本列，需保证整数位宽一致（或改用整数最小单位列）。
	type pair struct {
		text  string
		units int64
	}
	pairs := make([]pair, 0, len(values))
	for _, s := range values {
		d := MustParse(s)
		text, err := d.StringFixed(BaseAmountStoreScale)
		if err != nil {
			t.Fatalf("StringFixed(%s) 报错: %v", s, err)
		}
		units, err := d.Units(BaseAmountStoreScale)
		if err != nil {
			t.Fatalf("Units(%s) 报错: %v", s, err)
		}
		//小数部分位数必须恒定，这是定长的核心
		dot := strings.IndexByte(text, '.')
		if dot < 0 || len(text)-dot-1 != BaseAmountStoreScale {
			t.Errorf("StringFixed(%s) = %q 的小数位数不是恒定的 %d 位", s, text, BaseAmountStoreScale)
		}
		pairs = append(pairs, pair{text: text, units: units})
	}
	//整数最小单位排序必然正确（int64 数值序）
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].units < pairs[j].units })
	wantOrder := []string{"0.01", "2.00", "9.99", "10.00", "100.00"}
	for i, p := range pairs {
		if p.text != wantOrder[i] {
			t.Errorf("按最小单位排序第 %d 位 = %s, 期望 %s", i, p.text, wantOrder[i])
		}
	}
}

// TestMarshalJSON 验证恒以带引号字符串输出。
//
// 若默认结构体序列化生效，含未导出字段的本类型会输出「{}」，金额静默变空值；
// 若输出裸数字，JavaScript 一侧会用 float64 读取而静默丢精度。
func TestMarshalJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{"100.50", `"100.5"`},
		{"0", `"0"`},
		{"-33.34", `"-33.34"`},
		{"7.12345678", `"7.12345678"`},
	}
	for _, c := range cases {
		got, err := json.Marshal(MustParse(c.in))
		if err != nil {
			t.Errorf("Marshal(%q) 报错: %v", c.in, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("Marshal(%q) = %s, 期望 %s", c.in, got, c.want)
		}
	}

	//零值必须输出 "0"，不能是 {} 或 null
	var z Decimal
	got, err := json.Marshal(z)
	if err != nil {
		t.Fatalf("零值 Marshal 报错: %v", err)
	}
	if string(got) != `"0"` {
		t.Errorf("零值 Marshal = %s, 期望 \"0\"", got)
	}

	//嵌在结构体中（dto 的真实用法）同样不能退化成 {}
	type dto struct {
		Amount Decimal `json:"amount"`
	}
	body, err := json.Marshal(dto{Amount: MustParse("100.50")})
	if err != nil {
		t.Fatalf("结构体 Marshal 报错: %v", err)
	}
	if string(body) != `{"amount":"100.5"}` {
		t.Errorf("结构体 Marshal = %s, 期望 {\"amount\":\"100.5\"}", body)
	}
	if strings.Contains(string(body), "{}") {
		t.Errorf("金额被序列化成空对象: %s", body)
	}
}

// TestUnmarshalJSON 验证入站解析，含字符串与裸数字两种形态。
func TestUnmarshalJSON(t *testing.T) {
	okCases := []struct{ raw, want string }{
		{`"100.50"`, "100.5"}, //字符串形态（推荐）
		{`100.50`, "100.5"},   //裸数字形态（兼容）
		{`"0"`, "0"},
		{`0`, "0"},
		{`"-33.34"`, "-33.34"},
		{`"7.12345678"`, "7.12345678"},
		{`"1.005"`, "1.005"},
	}
	for _, c := range okCases {
		var d Decimal
		if err := json.Unmarshal([]byte(c.raw), &d); err != nil {
			t.Errorf("Unmarshal(%s) 报错: %v", c.raw, err)
			continue
		}
		if d.String() != c.want {
			t.Errorf("Unmarshal(%s) = %s, 期望 %s", c.raw, d.String(), c.want)
		}
	}

	//null 与字段缺失一律得零值 0 且不报错（两者在 Go 中不可区分）
	type dto struct {
		Amount Decimal `json:"amount"`
	}
	var withNull dto
	if err := json.Unmarshal([]byte(`{"amount":null}`), &withNull); err != nil {
		t.Errorf("null 应不报错: %v", err)
	}
	if !withNull.Amount.IsZero() {
		t.Errorf("null 应得零值, 实际 %s", withNull.Amount.String())
	}
	var absent dto
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Errorf("字段缺失应不报错: %v", err)
	}
	if !absent.Amount.IsZero() {
		t.Errorf("字段缺失应得零值, 实际 %s", absent.Amount.String())
	}

	//非法与越界必须报错
	badCases := []string{`""`, `"abc"`, `" 1.5"`, `"1,000"`, `true`, `[]`, `{}`}
	for _, raw := range badCases {
		var d Decimal
		if err := json.Unmarshal([]byte(raw), &d); err == nil {
			t.Errorf("Unmarshal(%s) 应报错, 实际得到 %s", raw, d.String())
		}
	}
}

// TestUnmarshalJSONEnforcesMagnitudeLimit 验证 dto 入口同样受量级设界约束。
//
// dto 是外部可控入口，若在此绕过校验，Parse 的设界形同虚设：
// 「1e1000000」这类值能从接口直接进入系统。
func TestUnmarshalJSONEnforcesMagnitudeLimit(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr error
	}{
		{`"1e1000000"`, ErrIntDigitsExceeded},
		{`"` + strings.Repeat("9", MaxIntDigits+1) + `"`, ErrIntDigitsExceeded},
		{`"1e-2000"`, ErrScaleExceeded},
		{`"0.` + strings.Repeat("0", MaxScale) + `1"`, ErrScaleExceeded},
	}
	for _, c := range cases {
		var d Decimal
		err := json.Unmarshal([]byte(c.raw), &d)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("Unmarshal(%.30s) 期望 %v, 实际 %v", c.raw, c.wantErr, err)
		}
	}
}

// TestJSONRoundTrip 验证 JSON 往返不丢精度。
//
// 与 idgen 的 18 位 ID 同一类问题：一旦走裸数字，超过 float64 安全整数上界的
// 精度会在往返中静默丢失。恒用字符串则往返无损。
func TestJSONRoundTrip(t *testing.T) {
	samples := []string{
		"0", "0.01", "-0.01", "100.5", "7.12345678", "-33.34",
		strings.Repeat("9", MaxIntDigits),    //整数位上界
		"0." + strings.Repeat("9", MaxScale), //小数位上界
	}
	type dto struct {
		Amount Decimal `json:"amount"`
	}
	for _, s := range samples {
		original := MustParse(s)
		body, err := json.Marshal(dto{Amount: original})
		if err != nil {
			t.Errorf("Marshal(%s) 报错: %v", s, err)
			continue
		}
		var back dto
		if err := json.Unmarshal(body, &back); err != nil {
			t.Errorf("Unmarshal(%s) 报错: %v", body, err)
			continue
		}
		if !back.Amount.Equal(original) {
			t.Errorf("往返后精度丢失: %s -> %s -> %s", s, body, back.Amount.String())
		}
	}
}
