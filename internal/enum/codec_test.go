package enum

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// 本文件测行为：解析、校验、JSON 编解码、SQL 存取。
//
// 7 个枚举共用同一套内核（enum.go 的 set/parse/marshal/scan），因此这里用一个泛型
// 驱动把「同一组行为」对 7 个类型各跑一遍，而不是把同样的断言抄 7 份。抄 7 份的写法
// 表面上覆盖率一样，实际上新增枚举时极易漏抄一份，而漏掉的那份恰恰是没人注意的。

// codecCase 描述一个枚举类型的编解码测试所需的全部要素。
type codecCase[T code] struct {
	name string
	// valid 是一个合法枚举值，用于走通「正常路径」。
	valid T
	// all 是全部枚举值，用于逐项往返。
	all []T
	// parse 是该类型的解析函数。
	parse func(string) (T, error)
	// unmarshal / scan / value / marshal 把接口方法暴露给泛型驱动。
	unmarshal func(*T, []byte) error
	scan      func(*T, any) error
	value     func(T) (driver.Value, error)
	marshal   func(T) ([]byte, error)
	valid_    func(T) bool
	isZero    func(T) bool
}

// TestRoleCodec 角色的编解码行为。
func TestRoleCodec(t *testing.T) {
	runCodecSuite(t, codecCase[Role]{
		name:      "Role",
		valid:     RoleAdmin,
		all:       Roles(),
		parse:     ParseRole,
		unmarshal: func(p *Role, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *Role, src any) error { return p.Scan(src) },
		value:     func(v Role) (driver.Value, error) { return v.Value() },
		marshal:   func(v Role) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v Role) bool { return v.Valid() },
		isZero:    func(v Role) bool { return v.IsZero() },
	})
}

// TestUserStatusCodec 用户状态的编解码行为。
func TestUserStatusCodec(t *testing.T) {
	runCodecSuite(t, codecCase[UserStatus]{
		name:      "UserStatus",
		valid:     UserStatusEnabled,
		all:       UserStatuses(),
		parse:     ParseUserStatus,
		unmarshal: func(p *UserStatus, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *UserStatus, src any) error { return p.Scan(src) },
		value:     func(v UserStatus) (driver.Value, error) { return v.Value() },
		marshal:   func(v UserStatus) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v UserStatus) bool { return v.Valid() },
		isZero:    func(v UserStatus) bool { return v.IsZero() },
	})
}

// TestFileFormatCodec 文件格式的编解码行为。
func TestFileFormatCodec(t *testing.T) {
	runCodecSuite(t, codecCase[FileFormat]{
		name:      "FileFormat",
		valid:     FileFormatCSV,
		all:       FileFormats(),
		parse:     ParseFileFormat,
		unmarshal: func(p *FileFormat, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *FileFormat, src any) error { return p.Scan(src) },
		value:     func(v FileFormat) (driver.Value, error) { return v.Value() },
		marshal:   func(v FileFormat) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v FileFormat) bool { return v.Valid() },
		isZero:    func(v FileFormat) bool { return v.IsZero() },
	})
}

// TestParserTypeCodec 解析器类型的编解码行为。
func TestParserTypeCodec(t *testing.T) {
	runCodecSuite(t, codecCase[ParserType]{
		name:      "ParserType",
		valid:     ParserTypeGenericCSV,
		all:       ParserTypes(),
		parse:     ParseParserType,
		unmarshal: func(p *ParserType, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *ParserType, src any) error { return p.Scan(src) },
		value:     func(v ParserType) (driver.Value, error) { return v.Value() },
		marshal:   func(v ParserType) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v ParserType) bool { return v.Valid() },
		isZero:    func(v ParserType) bool { return v.IsZero() },
	})
}

// TestOperationTypeCodec 操作类型的编解码行为。
func TestOperationTypeCodec(t *testing.T) {
	runCodecSuite(t, codecCase[OperationType]{
		name:      "OperationType",
		valid:     OperationTypeLogin,
		all:       OperationTypes(),
		parse:     ParseOperationType,
		unmarshal: func(p *OperationType, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *OperationType, src any) error { return p.Scan(src) },
		value:     func(v OperationType) (driver.Value, error) { return v.Value() },
		marshal:   func(v OperationType) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v OperationType) bool { return v.Valid() },
		isZero:    func(v OperationType) bool { return v.IsZero() },
	})
}

// TestObjectTypeCodec 操作对象类型的编解码行为。
func TestObjectTypeCodec(t *testing.T) {
	runCodecSuite(t, codecCase[ObjectType]{
		name:      "ObjectType",
		valid:     ObjectTypeUser,
		all:       ObjectTypes(),
		parse:     ParseObjectType,
		unmarshal: func(p *ObjectType, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *ObjectType, src any) error { return p.Scan(src) },
		value:     func(v ObjectType) (driver.Value, error) { return v.Value() },
		marshal:   func(v ObjectType) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v ObjectType) bool { return v.Valid() },
		isZero:    func(v ObjectType) bool { return v.IsZero() },
	})
}

// TestOperationResultCodec 操作结果的编解码行为。
func TestOperationResultCodec(t *testing.T) {
	runCodecSuite(t, codecCase[OperationResult]{
		name:      "OperationResult",
		valid:     OperationResultSuccess,
		all:       OperationResults(),
		parse:     ParseOperationResult,
		unmarshal: func(p *OperationResult, b []byte) error { return p.UnmarshalJSON(b) },
		scan:      func(p *OperationResult, src any) error { return p.Scan(src) },
		value:     func(v OperationResult) (driver.Value, error) { return v.Value() },
		marshal:   func(v OperationResult) ([]byte, error) { return v.MarshalJSON() },
		valid_:    func(v OperationResult) bool { return v.Valid() },
		isZero:    func(v OperationResult) bool { return v.IsZero() },
	})
}

// runCodecSuite 对一个枚举类型跑完整套行为断言。
func runCodecSuite[T code](t *testing.T, c codecCase[T]) {
	t.Helper()
	var zero T

	t.Run(c.name+"/合法值往返", func(t *testing.T) {
		for _, want := range c.all {
			// Parse 往返
			got, err := c.parse(string(want))
			if err != nil {
				t.Fatalf("Parse(%q) 报错: %v", string(want), err)
			}
			if got != want {
				t.Errorf("Parse(%q) = %q, want %q", string(want), string(got), string(want))
			}
			if !c.valid_(want) {
				t.Errorf("Valid(%q) = false, want true", string(want))
			}
			if c.isZero(want) {
				t.Errorf("IsZero(%q) = true, want false", string(want))
			}

			// JSON 往返：序列化必须恰为带引号的码值
			data, err := c.marshal(want)
			if err != nil {
				t.Fatalf("MarshalJSON(%q) 报错: %v", string(want), err)
			}
			if string(data) != `"`+string(want)+`"` {
				t.Errorf("MarshalJSON(%q) = %s, want %q", string(want), data, `"`+string(want)+`"`)
			}
			var back T
			if err := c.unmarshal(&back, data); err != nil {
				t.Fatalf("UnmarshalJSON(%s) 报错: %v", data, err)
			}
			if back != want {
				t.Errorf("JSON 往返后 = %q, want %q", string(back), string(want))
			}

			// SQL 往返：Value 必须产出 string（而非 []byte），否则驱动侧行为会分叉
			v, err := c.value(want)
			if err != nil {
				t.Fatalf("Value(%q) 报错: %v", string(want), err)
			}
			s, ok := v.(string)
			if !ok {
				t.Fatalf("Value(%q) 类型 = %T, want string", string(want), v)
			}
			if s != string(want) {
				t.Errorf("Value(%q) = %q, want %q", string(want), s, string(want))
			}
			var scanned T
			if err := c.scan(&scanned, s); err != nil {
				t.Fatalf("Scan(%q) 报错: %v", s, err)
			}
			if scanned != want {
				t.Errorf("SQL 往返后 = %q, want %q", string(scanned), string(want))
			}
			// []byte 形态（部分驱动返回 []byte 而非 string）必须等价
			var scannedBytes T
			if err := c.scan(&scannedBytes, []byte(s)); err != nil {
				t.Fatalf("Scan([]byte %q) 报错: %v", s, err)
			}
			if scannedBytes != want {
				t.Errorf("Scan([]byte) = %q, want %q", string(scannedBytes), string(want))
			}
		}
	})

	t.Run(c.name+"/未知码值被拒", func(t *testing.T) {
		// 大小写变体与空白变体都必须被拒——码值是机器契约，宽松匹配会制造两份
		// 互不相等的历史数据（见 set.parse 的注释）。
		bad := []string{
			"unknown_code_xyz",
			string(c.valid) + " ",
			" " + string(c.valid),
			upperFirst(string(c.valid)),
		}
		for _, b := range bad {
			if b == string(c.valid) {
				continue // upperFirst 对已大写或空串可能无变化
			}
			if _, err := c.parse(b); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Parse(%q) 错误 = %v, want ErrUnknownCode", b, err)
			}
			var dst T
			if err := c.unmarshal(&dst, []byte(`"`+b+`"`)); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("UnmarshalJSON(%q) 错误 = %v, want ErrUnknownCode", b, err)
			}
			// 反序列化失败时目标必须保持零值，不得被写入半个坏值
			if dst != zero {
				t.Errorf("UnmarshalJSON 失败后目标 = %q, want 零值", string(dst))
			}
			var scanDst T
			if err := c.scan(&scanDst, b); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Scan(%q) 错误 = %v, want ErrUnknownCode", b, err)
			}
			if scanDst != zero {
				t.Errorf("Scan 失败后目标 = %q, want 零值", string(scanDst))
			}
		}
	})

	t.Run(c.name+"/零值与空形态", func(t *testing.T) {
		if c.valid_(zero) {
			t.Error("Valid(零值) = true, want false")
		}
		if !c.isZero(zero) {
			t.Error("IsZero(零值) = false, want true")
		}
		// null / 空串 / 字段缺失三者在 Go 中不可区分，行为必须一致：都得零值且不报错
		for _, in := range []string{"null", `""`} {
			var dst T = c.valid // 先置为非零，确认确实被改写成零值
			if err := c.unmarshal(&dst, []byte(in)); err != nil {
				t.Errorf("UnmarshalJSON(%s) 报错: %v", in, err)
			}
			if dst != zero {
				t.Errorf("UnmarshalJSON(%s) = %q, want 零值", in, string(dst))
			}
		}
		// SQL NULL 与空串同样得零值
		for _, src := range []any{nil, "", []byte("")} {
			var dst T = c.valid
			if err := c.scan(&dst, src); err != nil {
				t.Errorf("Scan(%#v) 报错: %v", src, err)
			}
			if dst != zero {
				t.Errorf("Scan(%#v) = %q, want 零值", src, string(dst))
			}
		}
		// 零值落库为空串而非 NULL（见包注释「零值一律是空串」）
		v, err := c.value(zero)
		if err != nil {
			t.Fatalf("Value(零值) 报错: %v", err)
		}
		if v != any("") {
			t.Errorf("Value(零值) = %#v, want 空串（不得为 NULL，否则 ObjectType 的合法零值会被 NOT NULL 拒绝）", v)
		}
		// 零值序列化为空串
		data, err := c.marshal(zero)
		if err != nil {
			t.Fatalf("MarshalJSON(零值) 报错: %v", err)
		}
		if string(data) != `""` {
			t.Errorf("MarshalJSON(零值) = %s, want \"\"", data)
		}
	})

	t.Run(c.name+"/错误类型被拒", func(t *testing.T) {
		// JSON 非字符串形态
		for _, in := range []string{`123`, `{"a":1}`, `[1,2]`, `true`} {
			var dst T
			if err := c.unmarshal(&dst, []byte(in)); !errors.Is(err, ErrCodeType) {
				t.Errorf("UnmarshalJSON(%s) 错误 = %v, want ErrCodeType", in, err)
			}
		}
		// SQL 不支持的扫描类型
		for _, src := range []any{123, 1.5, true, struct{}{}} {
			var dst T
			if err := c.scan(&dst, src); !errors.Is(err, ErrScanType) {
				t.Errorf("Scan(%#v) 错误 = %v, want ErrScanType", src, err)
			}
		}
	})

	t.Run(c.name+"/写出侧与读入侧对称：非法码值不得落库、不得下发", func(t *testing.T) {
		// 7 个类型的底层类型都是 string，`enum.Role("root")` 这类强转在包外编译通过，
		// 所以「非法值根本造不出来」在本包不成立（currency.Code 靠私有字段才成立）。
		//
		// 这条用例防的是**只校验读入侧**这个具体错误。那种形态下：
		//   Role("root").Value() → 落库成功 "root" → 之后 Scan 读回永久报错
		// 即那一行写得进去、读不出来，且错误已离开造成它的那次请求；K-3 整库导出与
		// K-2 审计分页会因一行坏数据整页失败，排查时无从判断是谁写坏的。
		//
		// 断言方式刻意是「Value 报错」而非「Value 成功」——若将来有人为了图方便把
		// 写出侧的校验去掉，这里会立刻红，而不是等到某次导出失败才发现。
		bad := []T{
			T("no_such_code"),        // 纯未知
			T(string(c.valid) + " "), // 尾随空格：最常见的手工拼接事故
			T("NO_SUCH_CODE"),        // 大小写变体
		}
		for _, v := range bad {
			if _, err := c.value(v); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Value(%q) 错误 = %v, want ErrUnknownCode（非法码值不得落库）",
					string(v), err)
			}
			if _, err := c.marshal(v); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("MarshalJSON(%q) 错误 = %v, want ErrUnknownCode（非法码值不得下发）",
					string(v), err)
			}
			// 对称性本身：读入侧同样拒绝同一批值
			var dst T
			if err := c.scan(&dst, string(v)); !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Scan(%q) 错误 = %v, want ErrUnknownCode", string(v), err)
			}
		}

		// 合法值与零值必须照常通过——校验不能收得过紧，
		// 否则 ObjectType 的合法零值会被自己的写出侧拦下。
		if _, err := c.value(c.valid); err != nil {
			t.Errorf("Value(%q) 报错: %v（合法码值必须放行）", string(c.valid), err)
		}
		var zeroVal T
		if _, err := c.value(zeroVal); err != nil {
			t.Errorf("Value(零值) 报错: %v（零值是 ObjectType 的合法取值，必须放行）", err)
		}
		if _, err := c.marshal(zeroVal); err != nil {
			t.Errorf("MarshalJSON(零值) 报错: %v", err)
		}
	})

	t.Run(c.name+"/String 是中文名而非码值", func(t *testing.T) {
		// 这条断言防的是「用 %s 取码值」这类误用：String 必须与 Code 不同，
		// 否则误用不会被察觉，直到某天中文名被改动才暴露。
		s := fmt.Sprintf("%v", c.valid)
		if s == string(c.valid) {
			t.Errorf("String(%q) 与码值相同，无法区分日志面与契约面", string(c.valid))
		}
		if s == "" {
			t.Error("String() 返回空串，日志中会出现看不见的字段")
		}
		// 零值与未知码值都必须有可读输出
		var zeroVal T
		if got := fmt.Sprintf("%v", zeroVal); got != "未指定" {
			t.Errorf("String(零值) = %q, want 未指定", got)
		}
		unknown := T("no_such_code")
		if got := fmt.Sprintf("%v", unknown); got != "未知(no_such_code)" {
			t.Errorf("String(未知) = %q, want 未知(no_such_code)", got)
		}
	})

	t.Run(c.name+"/结构体字段整体编解码", func(t *testing.T) {
		// 前面测的是方法直调，这里测真实用法：作为结构体字段经 encoding/json 走一遍。
		// 两者不等价——值接收者的 MarshalJSON 与指针接收者的 UnmarshalJSON 在
		// 嵌入结构体时是否被正确识别，只有这样才测得出来。
		type wrapper struct {
			V T `json:"v"`
		}
		in := wrapper{V: c.valid}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("json.Marshal 报错: %v", err)
		}
		want := `{"v":"` + string(c.valid) + `"}`
		if string(data) != want {
			t.Errorf("json.Marshal = %s, want %s", data, want)
		}
		var out wrapper
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("json.Unmarshal 报错: %v", err)
		}
		if out.V != c.valid {
			t.Errorf("json 往返后 = %q, want %q", string(out.V), string(c.valid))
		}
		// 字段缺失得零值
		var missing wrapper
		if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
			t.Fatalf("json.Unmarshal({}) 报错: %v", err)
		}
		if missing.V != zero {
			t.Errorf("字段缺失时 = %q, want 零值", string(missing.V))
		}
		// 非法值经整体反序列化同样被拒
		var bad wrapper
		if err := json.Unmarshal([]byte(`{"v":"no_such_code"}`), &bad); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("json.Unmarshal 非法值错误 = %v, want ErrUnknownCode", err)
		}
	})

	t.Run(c.name+"/All 返回副本", func(t *testing.T) {
		// 调用方改动不得污染包级枚举表——这是并发安全的前提。
		first := c.all
		if len(first) == 0 {
			t.Fatal("枚举表为空")
		}
		snapshot := string(first[0])
		first[0] = T("tampered")
		var second []T
		switch c.name {
		case "Role":
			second = any(Roles()).([]T)
		case "UserStatus":
			second = any(UserStatuses()).([]T)
		case "FileFormat":
			second = any(FileFormats()).([]T)
		case "ParserType":
			second = any(ParserTypes()).([]T)
		case "OperationType":
			second = any(OperationTypes()).([]T)
		case "ObjectType":
			second = any(ObjectTypes()).([]T)
		case "OperationResult":
			second = any(OperationResults()).([]T)
		default:
			t.Fatalf("未覆盖的枚举类型: %s", c.name)
		}
		if string(second[0]) != snapshot {
			t.Errorf("包级枚举表被调用方改动污染: got %q, want %q", string(second[0]), snapshot)
		}
	})
}

// upperFirst 把首字母改为大写，用于构造大小写变体。
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}
