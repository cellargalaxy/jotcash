// Package enum 提供 7 类**代码内置枚举**：角色、用户状态、文件格式、解析器类型键、
// 11 类操作类型、操作对象类型、操作结果。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md`（下称「分层版」）§六 L1.0 `enum` 行：
// 「角色 / 用户状态 / 文件格式（CSV·Excel·PDF）/ **解析器类型键**（不含解析实现，终版 §五 2）
// / **11 类操作类型** / 操作对象类型 / 操作结果」，依赖列为 `base/*`。
//
// # 一、本包的定位：枚举即代码，库中只存代码
//
// 分层版 §五 准则 3「**枚举即代码**：币种、解析器类型建独立包，库中只存代码/键；
// **枚举项里的「实现」只存标识，实现的注册表在 L3、实现体在 L4**」。
// 落到本包的三条后果：
//
//	① 合法性校验落在应用层——本包的 Parse* 系列是唯一校验点
//	   （`doc/decisions/记账系统功能与模型.md`（下称「终版」）§五 1 同口径）；
//	② 枚举项**只能停用不能删除**（终版 §五 2：历史 `File` 存着旧键），
//	   因此「读历史数据」与「收用户输入」是两条不同的校验路径，见下方第三节；
//	③ 解析实现不在本包——`ParserType` 只有键与元信息，注册表在 L3 `parser`、
//	   实现体在 L4 `parser/csv`。照搬「枚举项含实现」会让 L1.0 依赖 L3/L4 必成环（准则 3 原文）。
//
// # 二、为什么代码是字符串而不是整数
//
// 终版 §五 1/2 已把币种与解析器类型定死为字符串（「库中只存币种代码」「库中只存键」）。
// 其余五类枚举文档未指定存储形态，本包**统一取字符串**，理由有三条，其中前两条是硬约束：
//
//	① K-3 整库导出（终版 §二 K-3）导出的是给用户看的明文包。若 `操作类型` 落库为 7，
//	   导出包里就是一列 7/8/9，用户无从判断；字符串代码 `expense_edit_delete` 自解释。
//	② 「只能停用不能删除」要求代码**永久稳定**。整数代码的稳定性依赖「谁都不要动 iota 顺序」
//	   这一口头约定，插入一项就会静默改写历史数据的语义；字符串代码与声明顺序无关。
//	③ 与币种、解析器类型键同一形态，`store` 的列口径、`dto` 的 JSON 契约不必分两套。
//
// 代价（显式接受）：库体积略大于整数列，个人系统规模下不构成问题。
//
// # 三、两条校验路径：收用户输入 vs 读历史数据
//
// 只有 `ParserType` 带「是否启用」（终版 §五 2 枚举项含该属性），因此只有它区分两条路径：
//
//	ParseParserType       仅接受**已启用**项 —— 用户输入路径（C-1 上传页显式选择解析器类型）
//	ParseParserTypeStored 接受**含已停用**项 —— 历史数据路径（`File.解析器类型` 落库值、Scan）
//
// 两者混用的后果是双向的：读历史用了前者，一旦停用某解析器，存量 `File` 记录立刻读不出来
// （而终版 §二 C-4 要求文件只留存不删除）；收输入用了后者，已停用的解析器又能被重新选中。
// 其余六类枚举无停用概念（终版 §四 未给它们「是否启用」属性），故只有一条路径。
//
// # 四、零值口径：`ObjectType` 是唯一「零值合法」的枚举
//
// 终版 §四 5 `AuditLog.操作对象类型`「**零值 = 本次操作无具体对象或为批量操作**」，
// §四 表头又定「业务上『不适用』的场景以零值表达，不留空」。因此：
//
//	ObjectType  零值 = 「无具体对象」，**合法**，可落库（空串）、可序列化
//	其余 6 类    零值 = 「未设置」，**非法**——它们对应的属性全部必填
//	            （`User.角色`/`状态`、`File.格式`/`解析器类型`、`AuditLog.操作类型`/`操作结果`）
//
// 零值非法的类型在 Value / MarshalText 上**报错而不是输出空串**：这几个字段必填，
// 出现零值只能是上游漏赋值，静默落一个空串进库会把漏填变成一条永久无法解释的记录。
// 这与 `base/calendar` 对零值 Date/Month 的处理同源（那边的 ErrZeroValue 同理）。
//
// # 五、String() 返回代码，Name() 返回中文名
//
// 兄弟包 `base/errs` 的 `Kind.String()` 返回中文档位名（「用户输入错误」），因为 `Kind` 是
// 整数类型，中文名与错误码 `Code()` 不可能混淆。本包的枚举**本身就是字符串**，若 String()
// 也返回中文名，`string(role)` 与 `role.String()` 就会得到两个不同的值，而前者恰恰是落库值
// ——这类混淆一旦发生就是脏数据。故本包定死：
//
//	String()  → 代码，即落库值与 JSON 值（稳定、英文、机器可读）
//	Name()    → 中文名称，**仅供日志与诊断**（约定 3），不是面向用户的文案
//	TextKey() → i18n 文案键，面向用户展示时由 `i18n` 按「语言 + 文案键」取词
//
// Name() 与 `errs.Kind.String()` 的定位完全一致：都只进日志。面向用户的界面文案一律走
// TextKey()——这是 K-4「新增语种只需增加配置、无需改动代码」得以成立的前提，
// 若前端直接展示 Name()，加一门语言就必须改 Go 代码。
//
// # 六、依赖与分层
//
// 依赖列 `base/*`（分层版 §六 L1.0 表）：本包只 import `base/errs`，返回带**档位 + 文案键**
// 的错误，供 `middleware` 直接映射 HTTP 状态与渲染文案。不 import `entity`（L1.1 在本包之上）、
// 不 import `currency`（同层，层内互不依赖）、不 import 任何 L2 及以上的包。
// 本包无任何 IO、无日志、无包级可变状态：枚举表在 init 期一次构建后只读，天然并发安全。
//
// # 七、文件组织与七类枚举的落点
//
// 一类枚举一个文件——七类的改动理由互不相干（加一个操作类型与加一个文件格式毫无关系），
// 与 `base/calendar` 把 Date / Month / MonthRange 分三文件同一取向：
//
//	enum.go        本文件：entry / table 与三条不变式校验、两条校验路径的公共实现、
//	               落库与序列化的统一口径（valueOf / marshalTextOf / scanCode）
//	role.go        Role       角色（2 项）        终版 §四 1 `User.角色`
//	userstatus.go  UserStatus 用户状态（2 项）    终版 §四 1 `User.状态`
//	fileformat.go  FileFormat 文件格式（3 项）    终版 §四 4 `File.格式`
//	parsertype.go  ParserType 解析器类型键（1 项）终版 §五 2；**唯一带「是否启用」**
//	optype.go      OpType     操作类型（11 项）   终版 §八 8.4
//	objecttype.go  ObjectType 操作对象类型（4 项）终版 §四 5；**唯一零值合法**
//	opresult.go    OpResult   操作结果（2 项）    终版 §四 5
//
// 每类枚举一律出齐同一组符号，形状统一（下方口径对所有七类成立，例外只有两处，
// 均已在第三、四节界定）：
//
//	Parse<类型>()   唯一校验点，非法即「用户输入错误」档 —— 例外：ParserType 出两条路径
//	List<类型>s()   按声明顺序列举，供各下拉数据源 —— 例外：ParserType 只列已启用项、
//	                ObjectType 不列零值
//	Valid / IsZero  取值域判据
//	String / Name / TextKey                第五节的三分口径
//	Value / Scan / MarshalText / UnmarshalText  落库与序列化，口径必须同进同退
package enum

import (
	"database/sql/driver"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/pkg/errors"
)

// textKeyPrefix i18n 文案键的统一前缀。
//
// 完整口径：`enum.<枚举类型>.<代码>`，如 `enum.role.admin`、`enum.op_type.data_intake`。
// 与 `base/errs` 的键口径 `<域>.<对象>.<问题>` 同构（全小写、点分三段），
// 且以 `enum.` 前缀与错误文案键彻底隔开——两者共用同一份语言文件
// （分层版 §六 L3 `i18n` 行「K-4 界面文案与 `base/errs` 错误文案**共用同一份语言文件**」），
// 键空间必须互不侵占。
const textKeyPrefix = "enum."

// entry 一个枚举项：代码 + 中文名称。
//
// 只有这两个通用字段。带额外属性的枚举（目前仅 `ParserType` 有「适用格式」与「是否启用」）
// 在自己的文件里另置属性表，不把可选字段塞进通用结构——否则 6 个用不到的枚举都要为它留空位。
type entry struct {
	// code 代码，即落库值与 JSON 值。须满足 isCanonicalCode。
	code string
	// name 中文名称，仅供日志与诊断（约定 3），不面向用户。
	name string
}

// table 一类枚举的只读查找表。
//
// 7 类枚举各持一张，把「代码唯一」「代码规范」「名称非空」三条不变式收敛到 newTable 一处校验，
// 并统一提供按代码查名、按声明顺序列举、文案键派生。
// 构建后只读，无锁并发安全。
type table struct {
	// kind 枚举类型的代码侧标识，文案键的中间段，如 `role`、`op_type`。
	kind string
	// label 枚举类型的中文名，用于诊断信息与错误的 field 占位参数，如「角色」。
	label string
	// codes 全部代码，**保持声明顺序**——枚举项的声明顺序即业务顺序
	// （如 11 类操作类型按终版 §八 8.4 表的 1~11 排列），K-2 的操作类型下拉据此稳定排序。
	codes []string
	// entries 代码 → 枚举项。
	entries map[string]entry
}

// newTable 构建枚举查找表，并在**包初始化期**校验三条不变式：
// 代码非空且规范、代码不重复、中文名称非空。
//
// 违反即 panic 而不是返回错误，两条理由：
//
//	① 枚举表是写死在源码里的常量数据，违反不变式属于**编码错误**而非运行期输入错误，
//	   在进程启动（乃至 `go test` 启动）时当场崩掉，比让一个重码的枚举表进生产要好得多；
//	② 若返回 error，7 张表的包级变量初始化就得各写一遍错误处理，而这条错误永远不该发生。
//
// 与 `base/idgen` 对 ID 为 0 时 panic 的取舍同源：宁可当场崩，不可静默写坏数据。
func newTable(kind, label string, entries ...entry) *table {
	if !isCanonicalCode(kind) {
		panic("enum：枚举类型标识不规范，kind=" + kind)
	}
	if label == "" {
		panic("enum：枚举类型中文名为空，kind=" + kind)
	}
	t := &table{
		kind:    kind,
		label:   label,
		codes:   make([]string, 0, len(entries)),
		entries: make(map[string]entry, len(entries)),
	}
	for _, e := range entries {
		if !isCanonicalCode(e.code) {
			panic("enum：枚举代码不规范，kind=" + kind + "，code=" + e.code)
		}
		if e.name == "" {
			panic("enum：枚举中文名称为空，kind=" + kind + "，code=" + e.code)
		}
		if _, dup := t.entries[e.code]; dup {
			panic("enum：枚举代码重复，kind=" + kind + "，code=" + e.code)
		}
		t.entries[e.code] = e
		t.codes = append(t.codes, e.code)
	}
	return t
}

// has 代码是否为本表已定义的枚举项。空代码恒为 false（零值不是枚举项）。
func (t *table) has(code string) bool {
	_, ok := t.entries[code]
	return ok
}

// listCodes 按**声明顺序**返回全部代码的副本。
//
// 返回副本而非 t.codes 本身：调用方拿到的切片会被 sort / append 之类的操作改写
// （K-2 的操作类型下拉、K-1 的下拉都可能就地排序），而枚举表构建后必须只读——
// 一次就地排序会把「声明顺序即业务顺序」这条口径静默破坏掉，且此后每次调用都受影响。
func (t *table) listCodes() []string {
	codes := make([]string, len(t.codes))
	copy(codes, t.codes)
	return codes
}

// nameOf 按代码取中文名称；未定义的代码返回「未知<枚举中文名>」，供诊断信息使用。
//
// 不返回空串是刻意的：日志里出现「角色=」无法区分「取不到名字」与「名字是空的」，
// 而库里若真出现了本版本不认识的代码（历史数据、被改写的数据），日志必须能说清这件事。
func (t *table) nameOf(code string) string {
	if e, ok := t.entries[code]; ok {
		return e.name
	}
	return "未知" + t.label
}

// textKeyOf 按代码派生 i18n 文案键；未定义的代码返回空串
// ——没有对应枚举项就没有对应文案，返回一个查不到词的键只会让 i18n 静默取空。
func (t *table) textKeyOf(code string) string {
	if !t.has(code) {
		return ""
	}
	return textKeyPrefix + t.kind + "." + code
}

// parse 按代码查表，命中返回代码本身，未命中返回「用户输入错误」档的错误。
//
// 错误口径：档位 KindInput、文案键 errs.KeyFieldInvalid（「字段取值非法」）、
// 占位参数 field = 枚举类型中文名。**非法代码本身不进占位参数**，只进诊断信息：
// 占位参数会被 i18n 填进面向用户的文案，把原始输入回显给用户没有必要。
func (t *table) parse(code string) (string, error) {
	if t.has(code) {
		return code, nil
	}
	return "", t.invalidErr(code)
}

// invalidErr 构造「代码非法」错误，供 parse 与各枚举的 Scan 复用。
func (t *table) invalidErr(code string) error {
	return errs.Wrap(
		errors.Errorf("%s的枚举代码非法，code=%q，可取值=%v", t.label, code, t.codes),
		errs.KindInput, errs.KeyFieldInvalid, errs.A(errs.ArgField, t.label),
	)
}

// isCanonicalCode 代码是否符合规范形态：`[a-z][a-z0-9_]*`，即小写字母开头、
// 仅含小写字母/数字/下划线。
//
// 这条机械校验拦的是「同一个枚举项出现两种写法」——`Admin` 与 `admin`、
// `system-init` 与 `system_init`。代码是落库值且永不可改（终版 §五 2），
// 大小写或分隔符写混一次，历史数据就永久带着两种形态，此后每处比较都要先做归一化。
// 校验放在 newTable 里，新增枚举项时在包初始化期即暴露，不依赖评审。
func isCanonicalCode(code string) bool {
	if code == "" {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9', c == '_':
			// 数字与下划线不得作首字符：`_x` / `1x` 既不像标识符也不像业务代码
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// codeFromSrc 把 `sql.Scanner` 的入参归一化为代码字符串。
//
// 三条口径，与 `base/calendar` 的 Date.Scan 保持一致：
//
//	string / []byte → 直接取值（`store/sqlite` 的枚举列一律 TEXT）
//	nil（NULL）      → 报错。终版 §四 表头「业务上『不适用』的场景以零值表达，不留空」，
//	                  枚举列出现 NULL 属于建表或写入路径的缺陷，不是可容忍的空值
//	其余类型         → 报错
//
// 档位取 KindSystem 而非 KindInput：这是**库内数据或驱动**与预期不符，不是用户输入错误，
// 不该以「字段取值非法」提示给用户，也不该被上层当成可重试的输入问题。
func codeFromSrc(src any, label string) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case nil:
		return "", errs.Wrap(
			errors.Errorf("扫描%s，值为 NULL，而该列为必填", label),
			errs.KindSystem, errs.KeySystemInternal,
		)
	default:
		return "", errs.Wrap(
			errors.Errorf("扫描%s，不支持的类型=%T", label, src),
			errs.KindSystem, errs.KeySystemInternal,
		)
	}
}

// scanCode 枚举 Scan 的统一实现：归一化入参 → 校验代码 → 返回代码。
//
// 与 parse 的分工：parse 面向**用户输入**，非法即「用户输入错误」档；
// 本函数面向**库内数据**，非法即「系统错误」档（见 codeFromSrc 的档位说明）。
// 两者若共用一档，一条被改写过的库内数据就会被当成用户输入错误回给前端，
// 排查方向完全走偏。
//
// allowZero 仅 `ObjectType` 传 true（第四节：它的零值合法，库里是空串）。
func scanCode(src any, t *table, allowZero bool) (string, error) {
	code, err := codeFromSrc(src, t.label)
	if err != nil {
		return "", err
	}
	if code == "" {
		if allowZero {
			return "", nil
		}
		return "", zeroValueErr(t.label, "而该列为必填")
	}
	if !t.has(code) {
		return "", invalidForOutputErr(t.label, code, "库内数据与本版本枚举表不符")
	}
	return code, nil
}

// zeroValueErr 构造「零值不可落库/序列化」错误，供 6 个零值非法的枚举复用。
// 档位取 KindSystem：必填字段出现零值是上游漏赋值的程序缺陷，不是用户输入问题。
func zeroValueErr(label, scene string) error {
	return errs.Wrap(
		errors.Errorf("%s为零值（未设置），%s", label, scene),
		errs.KindSystem, errs.KeySystemInternal,
	)
}

// invalidForOutputErr 构造「非法代码不可落库/序列化」错误。
//
// 与 zeroValueErr 分开：零值是「漏赋值」，非法值是「赋了一个本版本不认识的值」——
// 后者多见于反序列化绕过了 Parse* 的路径（如直接强转字符串），两者的排查方向不同。
func invalidForOutputErr(label, code, scene string) error {
	return errs.Wrap(
		errors.Errorf("%s的代码非法，code=%q，%s", label, code, scene),
		errs.KindSystem, errs.KeySystemInternal,
	)
}

// valueOf 枚举落库的统一实现：零值与非法值一律报错，其余以代码文本落库。
//
// allowZero 仅 `ObjectType` 传 true（第四节：它的零值合法，落空串）。
func valueOf(t *table, code string, allowZero bool) (driver.Value, error) {
	if code == "" {
		if allowZero {
			return "", nil
		}
		return nil, zeroValueErr(t.label, "不可落库")
	}
	if !t.has(code) {
		return nil, invalidForOutputErr(t.label, code, "不可落库")
	}
	return code, nil
}

// marshalTextOf 枚举序列化的统一实现，口径与 valueOf 完全一致
// ——落库与 JSON 必须同进同退，否则会出现「存得下但发不出去」的字段。
func marshalTextOf(t *table, code string, allowZero bool) ([]byte, error) {
	if code == "" {
		if allowZero {
			return []byte{}, nil
		}
		return nil, zeroValueErr(t.label, "不可序列化")
	}
	if !t.has(code) {
		return nil, invalidForOutputErr(t.label, code, "不可序列化")
	}
	return []byte(code), nil
}
