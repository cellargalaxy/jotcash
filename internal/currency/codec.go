package currency

import (
	"database/sql/driver"
	"fmt"
)

// MarshalJSON 实现 json.Marshaler：非零值序列化为 "CNY"，零值为 null。
//
// 序列化成字符串代码而非对象：dto 契约里的币种字段就是一个代码，名称与符号
// 由前端按 i18n 文案键自行渲染（终版 K-4），不随每条明细重复下发。
func (c Code) MarshalJSON() ([]byte, error) {
	if c.entry == nil {
		return []byte("null"), nil
	}
	// 代码在 init 中已校验为 3 个大写字母，不含需要转义的字符，可直接拼接。
	return []byte(`"` + c.entry.code + `"`), nil
}

// UnmarshalJSON 实现 json.Unmarshaler：接受 "CNY"、null 与空串，
// 后两者一律回到零值（表示「未指定」，用于 卡片币种 可空的场景）。
//
// 非空字符串一律走 Parse，因此**反序列化即校验**：一个从 JSON 解出来的
// 非零 Code 必然是表内已知币种，handler 不必再补一次校验。
func (c *Code) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" {
		*c = Code{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("%w: 非法的 JSON 币种字面值 %s", ErrUnknownCode, s)
	}
	s = s[1 : len(s)-1]
	if s == "" {
		*c = Code{}
		return nil
	}
	code, err := Parse(s)
	if err != nil {
		return err
	}
	*c = code
	return nil
}

// Value 实现 driver.Valuer：非零值落库为代码文本，零值落库为 NULL。
//
// 「只存代码入库」是分层 §六 L1.0 对本包的明确要求：名称、符号、小数位数
// 都是随代码派生的静态属性，落库会造成同一事实多处存储，改一处就产生分歧。
//
// 零值落 NULL 而非空串：必填列（Expense.支出币种、User.本位币）此时会被
// NOT NULL 约束当场拦下，而不是把一个 Parse 解不回来的空值静默写进库。
// 同 calendar.Date.Value 的取向。
func (c Code) Value() (driver.Value, error) {
	if c.entry == nil {
		return nil, nil
	}
	return c.entry.code, nil
}

// Scan 实现 sql.Scanner：接受 NULL、string 与 []byte。
//
// # 为什么建立在「已知」而非「已启用」上
//
// 终版 I-1 规定枚举项只能停用不能删除，因为「历史明细存着旧代码」。若这里
// 要求「已启用」，某币种一旦停用，引用它的历史明细当场读不出来——F-4
// 「显示已删除」查看、K-3 整库导出、I-7 逐笔重算三处同时失效。故 Scan 只要
// 代码在表内即成功，停用与否交由上层按场景判断（见 Code.Enabled）。
//
// # 为什么未知代码要响亮失败
//
// 读到表里根本没有的代码，意味着有人违规删了枚举项（I-1 禁止）或库被外部
// 改写。此时返回错误而非静默降级为零值：零值意味着「未指定」，把一个有主的
// 历史币种读成「未指定」，会让该笔明细的折算基准凭空消失，且不留任何痕迹。
func (c *Code) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*c = Code{}
		return nil
	case string:
		if v == "" {
			*c = Code{}
			return nil
		}
		code, err := Parse(v)
		if err != nil {
			return err
		}
		*c = code
		return nil
	case []byte:
		if len(v) == 0 {
			*c = Code{}
			return nil
		}
		code, err := Parse(string(v))
		if err != nil {
			return err
		}
		*c = code
		return nil
	default:
		return fmt.Errorf("%w: %T", ErrScanType, src)
	}
}
