package currency

import (
	"database/sql/driver"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// 本文件是 **T38 本位币候选口径**的唯一实现处。
//
// 分层 §六 L1.0 `currency` 行：「**另出本位币候选口径（T38）**：
// `可作本位币 = 已启用 且 小数位数 ≤ 2`，对外提供候选集（K-1 下拉）与单值校验；
// **支出币种不受此限**（3 位币种仍可正常记账与摊分）」。
//
// # 为什么口径在这里，而不是在 config 或 service
//
// 这个判据要被至少三个地方用到：K-1 配置页渲染下拉（BaseCandidates）、
// I-2 保存本位币时的校验（ValidateBase）、I-7 重算前的前置检查。若各处自己写
// `cur.Enabled && cur.Scale <= 2`，就有三份同义条件——将来口径一变（比如放宽到 ≤ 3），
// 漏改一处就是「下拉里能选、保存时被拒」这类自相矛盾。故把它收成本包的一个具名判据。
//
// # 为什么是 ≤ 2 而不是「只能 CNY」或「不限」
//
// 本位币的小数位数是不变式 1a 折算的**舍入基准**（「按本位币的 currency.小数位数 四舍五入」），
// 也是 8.2 里本位币侧摊分的**均分基准**。选一个 3 位币种作本位币，
// 会让所有本位币金额都带 3 位小数，与「个人记账」的实际呈现习惯不符，
// 也会把 store 侧本位币列的精度需求抬高一档。而限死 CNY 又过紧——
// 长期居住在美 / 欧 / 港的用户理应能把本位币设为 USD / EUR / HKD。
// 「≤ 2」正好放开这一批、挡住 KWD / BHD / OMR / JOD 这类 3 位币种。
//
// **注意这条只约束本位币**：3 位币种作**支出币种**完全正常——KWD 的支出会按 3 位均分摊分，
// 再按本位币的 2 位折算入账。SupportsBase 与 Enabled 是两个不同判据，不要互相替代。

// baseMaxScale 可作本位币的小数位数上限（T38）。
//
// 具名而非在条件里写字面量 2：这个 2 与 `base/decimal` 的任何精度档都无关
// （那些是存储与运算精度），它是一条**业务口径**。将来若口径调整，改这一处即可，
// 且 grep 得到的每一处引用都是真的与本位币口径有关，不会混进别的 2。
const baseMaxScale int32 = 2

// SupportsBase 该枚举项是否可作本位币（T38：已启用 且 小数位数 ≤ 2）。
//
// 挂在 Currency 上而非只提供按 Code 的函数版本：BaseCandidates 内部遍历的是 Currency 值，
// 调用方拿到 Currency 后也常需就地判断，避免「拿着 Currency 却要再按 Code 查一次表」。
func (c Currency) SupportsBase() bool {
	return c.Enabled && c.Scale <= baseMaxScale
}

// CanBeBase 该币种代码是否可作本位币（T38）。
//
// 未收录的代码返回 false（而非 panic）：本函数的调用方包括校验路径，
// 拿到一个未知代码是正常的「输入非法」而不是程序错误。
func CanBeBase(code Code) bool {
	cur, ok := byCode[code]
	return ok && cur.SupportsBase()
}

// BaseCandidates 返回全部可作本位币的枚举项（T38），顺序同 All（即策展表声明顺序）。
//
// 供 K-1 配置页的本位币下拉直接渲染。返回副本，调用方修改不影响策展表。
func BaseCandidates() []Currency {
	out := make([]Currency, 0, len(table))
	for _, cur := range table {
		if cur.SupportsBase() {
			out = append(out, cur)
		}
	}
	return out
}

// ValidateBase 校验该币种代码可作本位币（T38），供 I-2 保存本位币时使用。
//
// **两档错误刻意分开**，因为这是两个不同的用户处境，且 `base/errs` 为二者预留了不同文案键：
//
//	代码不在枚举表内        → currency.code.invalid（这个币种不存在 / 不认识）
//	代码合法但不满足 T38    → currency.code.not_base_candidate（这个币种存在，但不能当本位币）
//
// 后者是 `base/errs/key.go` 里 KeyCurrencyNotBaseCandidate 的**唯一**使用处
// （其注释即写「T38：可作本位币 = 已启用 且 小数位数 ≤ 2」）。
// 若两者合并成一个键，用户把本位币设成 KWD 时只会看到「币种非法」，
// 无从知道「KWD 是好币种，只是不能作本位币」——而这正是他需要知道的那一句。
//
// 已停用币种归入前者（currency.code.invalid）：与 ValidateEnabled 保持一致，
// 不对外泄露「存在但已停用」这一内部状态。
func ValidateBase(code Code) error {
	cur, ok := byCode[code]
	if !ok || !cur.Enabled {
		return errs.NewInput(errs.KeyCurrencyInvalid, errs.A(errs.ArgCurrency, code.String()))
	}
	if cur.Scale > baseMaxScale {
		return errs.NewInput(errs.KeyCurrencyNotBaseCandidate, errs.A(errs.ArgCurrency, code.String()))
	}
	return nil
}

// Value 实现 driver.Valuer，以**字符串**落库（终版 §五 1「库中只存币种代码」）。
//
// 落 string 而非 ISO 数字代码（156 / 840…）：数字代码在库里完全不可读，
// 排查问题时每次都要反查对照表；且 8.3 判重要按「(日期, 金额, 币种)」比较，
// 字符串比较与 Go 侧的 Code 天然一致，不需要一层来回映射。
//
// 零值落 NULL 而非空串：`Expense.卡片币种` 非必填，NULL 才是「无此信息」在库里的正确表达
// ——空串会让「留空」和「存了个空值」在 SQL 层不可区分，也会让 `WHERE 卡片币种 IS NULL`
// 这类查询漏掉记录。与 Scan 的 nil 分支严格互逆。
func (c Code) Value() (driver.Value, error) {
	if c.IsZero() {
		return nil, nil
	}
	return string(c), nil
}

// Scan 实现 sql.Scanner，从库中读回币种代码。
//
// 三条与 UnmarshalText 不同的取向，都源于「数据来自库、不来自用户」：
//
//	① **错误档位是 KindSystem 而非 KindInput**。库里出现一个不在枚举表内的币种代码，
//	   意味着数据被外部改写、或有人违反了「只增不删」纪律删了枚举行——这是系统 / 数据异常，
//	   不是用户这次输入错了。档位错了会让这类严重问题在接口上表现为 4xx，被当成用户问题忽略。
//	② **收录停用项**（走 Valid 而非 Enabled）：历史明细里存着的正是可能已停用的币种，
//	   这是「只能停用不能删除」这条纪律存在的全部意义。若此处按 Enabled 判，
//	   停用一个币种就会让它的所有历史支出变成读不出来的行。
//	③ **接受 NULL 与空串**，均映射为零值 Code：NULL 是本类型 Value 写出的形态，
//	   空串则可能来自本仓库之前的写入或外部导入，二者都表示「留空」，
//	   在读侧一并容忍比让一行历史数据读不出来更合适。
//
// 类型上同时接受 string 与 []byte：不同驱动对文本列的返回类型不一致
// （database/sql 允许两者），只认一种会在换驱动时踩坑。
func (c *Code) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*c = ""
		return nil
	case string:
		return c.scanText(v)
	case []byte:
		return c.scanText(string(v))
	default:
		return errs.NewSystem(errs.KeyCurrencyInvalid, errs.A(errs.ArgCurrency, "unsupported scan type"))
	}
}

// scanText 是 Scan 的文本分支实现，供 string 与 []byte 两个 case 共用。
func (c *Code) scanText(s string) error {
	parsed := Code(s)
	if parsed.IsZero() {
		*c = ""
		return nil
	}
	if !Valid(parsed) {
		// 档位 KindSystem，理由见 Scan 注释 ①。
		return errs.NewSystem(errs.KeyCurrencyInvalid, errs.A(errs.ArgCurrency, s))
	}
	*c = parsed
	return nil
}
