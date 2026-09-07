package currency

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
)

// TestMarshalJSON 覆盖序列化。
func TestMarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		code Code
		want string
	}{
		{"普通币种", MustParse("CNY"), `"CNY"`},
		{"0 位小数币种", MustParse("JPY"), `"JPY"`},
		{"3 位小数币种", MustParse("KWD"), `"KWD"`},
		{"零值序列化为 null", Code{}, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.code)
			if err != nil {
				t.Fatalf("Marshal 返回错误: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal = %s, 期望 %s", got, tc.want)
			}
		})
	}
}

// TestUnmarshalJSON 覆盖反序列化的成功与失败分支。
func TestUnmarshalJSON(t *testing.T) {
	t.Run("合法代码", func(t *testing.T) {
		var c Code
		if err := json.Unmarshal([]byte(`"USD"`), &c); err != nil {
			t.Fatalf("Unmarshal 返回错误: %v", err)
		}
		if c.String() != "USD" {
			t.Errorf("得到 %q, 期望 USD", c)
		}
	})

	t.Run("null 与空串回到零值", func(t *testing.T) {
		// 卡片币种「解析不出则留空」的场景
		for _, input := range []string{`null`, `""`} {
			c := MustParse("CNY") // 先赋非零值，确认确实被重置
			if err := json.Unmarshal([]byte(input), &c); err != nil {
				t.Fatalf("Unmarshal(%s) 返回错误: %v", input, err)
			}
			if !c.IsZero() {
				t.Errorf("Unmarshal(%s) 后应为零值, 实际 %q", input, c)
			}
		}
	})

	t.Run("未知代码被拒", func(t *testing.T) {
		// 反序列化即校验：handler 不必再补一次
		for _, input := range []string{`"XXX"`, `"cny"`, `"CN"`} {
			var c Code
			err := json.Unmarshal([]byte(input), &c)
			if !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Unmarshal(%s) 错误 = %v, 期望 ErrUnknownCode", input, err)
			}
		}
	})

	t.Run("非字符串字面值被拒", func(t *testing.T) {
		for _, input := range []string{`123`, `true`, `{}`, `[]`, `"`} {
			var c Code
			if err := json.Unmarshal([]byte(input), &c); err == nil {
				t.Errorf("Unmarshal(%s) 应返回错误", input)
			}
		}
	})
}

// TestJSONRoundTrip 覆盖往返一致性，含结构体字段场景。
func TestJSONRoundTrip(t *testing.T) {
	t.Run("全部币种往返", func(t *testing.T) {
		for _, want := range All() {
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("%q Marshal 失败: %v", want, err)
			}
			var got Code
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("%q Unmarshal 失败: %v", want, err)
			}
			if !got.Equal(want) {
				t.Errorf("往返后 %q != %q", got, want)
			}
		}
	})

	t.Run("作为结构体字段", func(t *testing.T) {
		// 模拟 dto：支出币种必填、卡片币种可空
		type expenseDTO struct {
			Spend Code `json:"spend"`
			Card  Code `json:"card"`
		}
		in := expenseDTO{Spend: MustParse("USD")} // Card 留零值
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal 失败: %v", err)
		}
		if string(data) != `{"spend":"USD","card":null}` {
			t.Errorf("Marshal = %s", data)
		}

		var out expenseDTO
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}
		if !out.Spend.Equal(in.Spend) {
			t.Errorf("支出币种往返不一致: %q != %q", out.Spend, in.Spend)
		}
		if !out.Card.IsZero() {
			t.Errorf("卡片币种应为零值, 实际 %q", out.Card)
		}
	})

	t.Run("字段缺失时保持零值", func(t *testing.T) {
		type dto struct {
			Spend Code `json:"spend"`
		}
		var out dto
		if err := json.Unmarshal([]byte(`{}`), &out); err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}
		if !out.Spend.IsZero() {
			t.Errorf("缺失字段应为零值, 实际 %q", out.Spend)
		}
	})
}

// TestValue 覆盖落库形态：只存代码，零值落 NULL。
func TestValue(t *testing.T) {
	t.Run("非零值落代码文本", func(t *testing.T) {
		for _, c := range All() {
			got, err := c.Value()
			if err != nil {
				t.Fatalf("%q Value() 返回错误: %v", c, err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("%q Value() 类型 = %T, 期望 string", c, got)
			}
			if s != c.String() {
				t.Errorf("Value() = %q, 期望 %q", s, c)
			}
		}
	})

	t.Run("零值落 NULL", func(t *testing.T) {
		// 落 NULL 而非空串：必填列会被 NOT NULL 约束当场拦下
		got, err := Code{}.Value()
		if err != nil {
			t.Fatalf("Value() 返回错误: %v", err)
		}
		if got != nil {
			t.Errorf("零值 Value() = %v, 期望 nil", got)
		}
	})

	t.Run("返回值是合法的 driver.Value", func(t *testing.T) {
		got, err := MustParse("CNY").Value()
		if err != nil {
			t.Fatal(err)
		}
		if !driver.IsValue(got) {
			t.Errorf("Value() 返回 %T 不是合法的 driver.Value", got)
		}
	})
}

// TestScan 覆盖回读的各类来源与失败分支。
func TestScan(t *testing.T) {
	t.Run("string 与 []byte", func(t *testing.T) {
		for _, src := range []any{"CNY", []byte("CNY")} {
			var c Code
			if err := c.Scan(src); err != nil {
				t.Fatalf("Scan(%T) 返回错误: %v", src, err)
			}
			if c.String() != "CNY" {
				t.Errorf("Scan(%T) 得到 %q, 期望 CNY", src, c)
			}
		}
	})

	t.Run("NULL 与空值回到零值", func(t *testing.T) {
		for _, src := range []any{nil, "", []byte{}, []byte(nil)} {
			c := MustParse("CNY") // 先赋非零值
			if err := c.Scan(src); err != nil {
				t.Fatalf("Scan(%#v) 返回错误: %v", src, err)
			}
			if !c.IsZero() {
				t.Errorf("Scan(%#v) 后应为零值, 实际 %q", src, c)
			}
		}
	})

	t.Run("未知代码响亮失败", func(t *testing.T) {
		// 不静默降级为零值：那会让该笔明细的折算基准凭空消失
		for _, src := range []any{"XXX", []byte("XXX"), "cny"} {
			var c Code
			err := c.Scan(src)
			if !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Scan(%v) 错误 = %v, 期望 ErrUnknownCode", src, err)
			}
			if !c.IsZero() {
				t.Errorf("Scan 失败后不应写入值, 实际 %q", c)
			}
		}
	})

	t.Run("不支持的类型返回 ErrScanType", func(t *testing.T) {
		for _, src := range []any{123, 12.5, true, struct{}{}} {
			var c Code
			err := c.Scan(src)
			if !errors.Is(err, ErrScanType) {
				t.Errorf("Scan(%T) 错误 = %v, 期望 ErrScanType", src, err)
			}
		}
	})
}

// TestSQLRoundTrip 覆盖落库与回读的往返一致性。
func TestSQLRoundTrip(t *testing.T) {
	t.Run("全部币种往返", func(t *testing.T) {
		for _, want := range All() {
			v, err := want.Value()
			if err != nil {
				t.Fatalf("%q Value() 失败: %v", want, err)
			}
			var got Code
			if err := got.Scan(v); err != nil {
				t.Fatalf("%q Scan() 失败: %v", want, err)
			}
			if !got.Equal(want) {
				t.Errorf("往返后 %q != %q", got, want)
			}
		}
	})

	t.Run("零值往返", func(t *testing.T) {
		v, err := Code{}.Value()
		if err != nil {
			t.Fatal(err)
		}
		var got Code
		if err := got.Scan(v); err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Errorf("零值往返后应仍为零值, 实际 %q", got)
		}
	})

	t.Run("驱动返回 []byte 时同样可回读", func(t *testing.T) {
		// 部分驱动对 TEXT 列返回 []byte 而非 string
		for _, want := range All() {
			var got Code
			if err := got.Scan([]byte(want.String())); err != nil {
				t.Fatalf("%q Scan([]byte) 失败: %v", want, err)
			}
			if !got.Equal(want) {
				t.Errorf("往返后 %q != %q", got, want)
			}
		}
	})
}
