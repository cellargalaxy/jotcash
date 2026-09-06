package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 7 项：**操作结果**。

import (
	"database/sql/driver"
)

// OpResult 操作结果，对应 `entity.AuditLog.操作结果`。
//
// 依据终版 §四 5 `AuditLog.操作结果`：「成功 / 失败」。
//
// # 「失败」这一档承载的是 8.4 第 2 类的登录失败
//
// §八 8.4 第 2 类原文：「登录 | A-4；**失败以 `操作结果 = 失败` 表达**——用户名**存在**
// （含密码错、账户禁用、锁定期内）则记录并**归属该用户**，用户名**不存在**则**不记审计**
// （无归属、无消费方）」。这是「有主则记、无主不记」口径（终版 §十二 相对对话17 的变化 ③）。
//
// 另有 K-1 的第 8 类（本位币金额重算）也会出现失败——I-7「单笔失败不影响其他笔，
// 天然幂等可续跑」，收尾审计记成功/失败笔数（分层版 §六 L5 `service/fx` 行）。
//
// # 与 `service/audit` 两种写入语义的对应
//
// 分层版 §六 L5 `service/audit` 行：「② **独立事务审计**——`操作结果 = 失败` 的记录
// （尤以 8.4 第 2 类登录失败），与业务事务解耦，业务回滚不带走它」。
// 即本枚举的取值**直接决定** audit 走哪种写入语义。本包只定义取值，不持有该分派。
type OpResult string

const (
	// OpResultSuccess 成功。
	OpResultSuccess OpResult = "success"
	// OpResultFailure 失败。走 `service/audit` 的**独立事务**写入语义
	// ——业务事务回滚不得带走这条失败记录，否则登录失败次数无从累计（A-8）。
	OpResultFailure OpResult = "failure"
)

// opResultTable 操作结果枚举表。声明顺序与终版 §四 5「成功 / 失败」一致。
var opResultTable = newTable("op_result", "操作结果",
	entry{code: string(OpResultSuccess), name: "成功"},
	entry{code: string(OpResultFailure), name: "失败"},
)

// ParseOpResult 校验并转换操作结果代码，是操作结果的唯一校验点。
// 供 K-2 的操作结果筛选参数校验使用。
func ParseOpResult(code string) (OpResult, error) {
	parsed, err := opResultTable.parse(code)
	return OpResult(parsed), err
}

// ListOpResults 按声明顺序返回全部操作结果，供 K-2 下拉使用。
func ListOpResults() []OpResult {
	codes := opResultTable.listCodes()
	results := make([]OpResult, 0, len(codes))
	for _, code := range codes {
		results = append(results, OpResult(code))
	}
	return results
}

// Valid 是否为已定义的操作结果。零值返回 false（`AuditLog.操作结果` 必填，终版 §四 5）。
func (r OpResult) Valid() bool { return opResultTable.has(string(r)) }

// IsZero 是否为零值，即「未设置」。
func (r OpResult) IsZero() bool { return r == "" }

// IsSuccess 是否为成功。
//
// 与 IsAdmin / IsEnabled 同一理由：审计写入方须按「是否成功」分派事务语义，
// 写成 `result != OpResultFailure` 会让零值（漏赋值）被当成成功——
// 而漏赋值的那条记录本该在 Value 处报错，却先被判成了「成功」并走进业务同事务，
// 失败记录随业务回滚一并消失。
func (r OpResult) IsSuccess() bool { return r == OpResultSuccess }

// String 返回代码，即落库值与 JSON 值。
func (r OpResult) String() string { return string(r) }

// Name 返回中文名称，仅供日志与诊断。
func (r OpResult) Name() string { return opResultTable.nameOf(string(r)) }

// TextKey 返回 i18n 文案键。
func (r OpResult) TextKey() string { return opResultTable.textKeyOf(string(r)) }

// Value 实现 driver.Valuer，以代码文本落库；零值与非法值报错。
func (r OpResult) Value() (driver.Value, error) { return valueOf(opResultTable, string(r), false) }

// Scan 实现 sql.Scanner。
func (r *OpResult) Scan(src any) error {
	code, err := scanCode(src, opResultTable, false)
	if err != nil {
		return err
	}
	*r = OpResult(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler。
func (r OpResult) MarshalText() ([]byte, error) {
	return marshalTextOf(opResultTable, string(r), false)
}

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (r *OpResult) UnmarshalText(data []byte) error {
	parsed, err := ParseOpResult(string(data))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}
