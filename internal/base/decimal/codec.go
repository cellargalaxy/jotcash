package decimal

import (
	"encoding/json"
	"fmt"
)

// String 返回定点文本形式，不带尾随零：100.50 输出「100.5」，零值输出「0」。
//
// 仅供日志与排查使用。落库与对外接口一律用 StringFixed 或 Units 显式指定位数——
// 「不带尾随零」意味着长度不定长，而 store 的排序与区间筛选依赖定长或整数形态
// （文本比较下「100.00」会排在「9.99」之前）。
func (d Decimal) String() string {
	return d.v.String()
}

// StringFixed 返回固定 scale 位小数的定点文本，供 store/sqlite 的「定点文本」列形态使用。
// 如 2 位：1.5 输出「1.50」、0 输出「0.00」；8 位汇率：7.12345678 原样输出。
//
// 定长是关键性质：同一列内全部取值位数一致后，字典序即数值序（同符号内），
// store 侧的 F-3 排序与 F-4 区间筛选才能用朴素字符串比较完成。
//
// 值无法被 scale 位无损容纳时返回 ErrPrecisionLoss，不做静默舍入：
// 需要舍入的调用方应先显式调 Round，让舍入这一动作在调用点可见。
func (d Decimal) StringFixed(scale int32) (string, error) {
	if err := checkScale(scale); err != nil {
		return "", err
	}
	if !d.FitsScale(scale) {
		return "", fmt.Errorf("%w: 值 %s 需 %d 位, 目标 %d 位",
			ErrPrecisionLoss, d.v.String(), d.Scale(), scale)
	}
	return d.v.StringFixed(scale), nil
}

// Units 换算为最小单位整数，供 store/sqlite 的「整数最小单位」列形态使用。
// 如 2 位：100.50 得 10050；8 位汇率：7.12345678 得 712345678。
//
// 两道显式校验，任一不满足即报错而非返回错值：
//   - 无损性：值需能被 scale 位无损容纳，否则 ErrPrecisionLoss；
//   - 溢出：结果需在 int64 范围内，否则 ErrUnitsOverflow。
//
// 溢出判定走 big.Int 的 IsInt64，不用第三方库的取整数部分方法——后者对超出
// int64 的值会静默返回截断后的错误整数（实测 23 位数得到一个完全无关的值），
// 那意味着一笔金额可以在毫无征兆的情况下变成另一个数字落进库里。
func (d Decimal) Units(scale int32) (int64, error) {
	if err := checkScale(scale); err != nil {
		return 0, err
	}
	if !d.FitsScale(scale) {
		return 0, fmt.Errorf("%w: 值 %s 需 %d 位, 目标 %d 位",
			ErrPrecisionLoss, d.v.String(), d.Scale(), scale)
	}
	//左移 scale 位后取整数系数：此时小数部分必为零，取整不丢信息
	units := d.v.Shift(scale).BigInt()
	if !units.IsInt64() {
		return 0, fmt.Errorf("%w: 值 %s 按 %d 位换算超出 int64", ErrUnitsOverflow, d.v.String(), scale)
	}
	return units.Int64(), nil
}

// MarshalJSON 实现 json.Marshaler，恒序列化为带引号的字符串，如「"100.5"」。
//
// 必须自实现，两个原因各自都会导致金额静默出错：
//   - 本类型含未导出字段与零宽的 noCmp，默认的结构体序列化会输出空对象「{}」，
//     即金额静默变成空值；
//   - 第三方库自身的序列化形态受一个包级可变量控制，任何包都能把它改成输出裸数字。
//     裸数字会被 JavaScript 一侧读成 float64，与 idgen 的 18 位 ID 同一个问题：
//     超过 2^53 的精度在往返中静默丢失。恒用字符串则不受该全局状态影响。
func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.v.String())
}

// UnmarshalJSON 实现 json.Unmarshaler，接受 JSON 字符串与裸数字两种形态。
//
// 走与 Parse 完全相同的量级校验：dto 是前后端的 JSON 契约，属外部可控入口，
// 若在此绕过校验，「1e1000000」这类值就能从接口直接进入系统，Parse 的设界形同虚设。
//
// null 与字段缺失一律得零值 0 且不报错——两者在 Go 中不可区分，行为必须一致。
func (d *Decimal) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = Decimal{}
		return nil
	}
	//先按字符串解析，失败再按裸数字解析：两种形态都要能读进来。
	//注意不能反过来——字符串形态若先走裸数字分支会带上引号，解析必然失败。
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		text = string(data)
	}
	parsed, err := Parse(text)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
