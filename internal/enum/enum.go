// Package enum 承载 jotcash 的 7 个代码内置枚举（L1.0 枚举层）。
//
// # 本包装什么
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md 的 L1.0 表 enum 行，本包恰含
// 7 个枚举类型，各自的唯一来源如下：
//
//	Role            角色           终版 §四 1 User.角色
//	UserStatus      用户状态       终版 §四 1 User.状态
//	FileFormat      文件格式       终版 C-2 / §四 4 File.格式（CSV·Excel·PDF）
//	ParserType      解析器类型键   终版 §五 2 / D-1（不含解析实现）
//	OperationType   操作类型       终版 §8.4 的 11 类
//	ObjectType      操作对象类型   终版 §8.4 对象类型列 / §四 5
//	OperationResult 操作结果       终版 §四 5 AuditLog.操作结果
//
// # 枚举即代码：码值就是落库的值
//
// 准则 3「枚举即代码」规定库中只存代码 / 键。因此每个枚举项的**码值即列值**，
// 7 个类型的底层类型一律是 string，`string(v)` 与 `v.Code()` 得到的就是库里那一格
// 的内容。这带来两条不可回避的契约：
//
//   - **码值一经发布不得修改**。历史行里存着旧码值，改码值等于让存量数据指向一个
//     不存在的枚举项。改中文名可以（它只进日志），改码值不行。
//   - **枚举项只能停用、不能删除**（终版 §五 1、§五 2）。删除项会让历史行的 Scan
//     当场报错。目前只有 ParserType 有「是否启用」这一维（§五 2 明列），其余 6 个
//     枚举无此概念，见各自类型的注释。
//
// 用字符串码而非整数码，是因为前提 1 假定无人直接读写数据库、数据一律明文存储：
// K-3 整库导出的产物与 SQLite 库文件都要人能直接看懂，整数码则必须另配一份映射表
// 才能解读，而那份表一旦与代码漂移就无从发现。
//
// # 零值一律是空串，含义分两类
//
// 7 个类型的零值都是空串，`Valid()` 对它一律返回 false。但它在业务上的含义分两类：
//
//   - **ObjectType 的零值是合法取值**——终版 §四 5 明写「零值 = 本次操作无具体对象
//     或为批量操作」，11 类审计中有 5 类（登录 / 数据入库 / 批量删除 / 本位币重算 /
//     数据导出）就该落零值；
//   - 其余 6 个类型的零值表示「未指定」，属非法状态，不应出现在任何已落库的行上。
//
// 因此 Value() 对零值返回**空串而非 NULL**：AuditLog.操作对象类型 是必填列
// （终版 §四 5 表头「全部必填」，「不适用」的场景以零值表达、不留空），落 NULL 会被
// NOT NULL 约束当场拒绝掉一个合法的零值。这与 base/calendar 的 Date 取向相反——
// 那里零值落 NULL 是刻意的，因为 Expense.支出日期 的零值本就该被 NOT NULL 拦下。
// 两者的差异来自「零值是否为合法业务取值」，不是口径不一致。
//
// # 四条出入口一律校验码值，两两对称
//
// 7 个类型的底层类型都是 string，于是 `enum.Role("root")` 这类强转在包外**编译
// 通过**（不同于 currency.Code 用私有字段把非法值堵在编译期）。因此本包在四条
// 出入口上一律校验码值合法性，且入口与出口必须对称：
//
//	读入   Scan / UnmarshalJSON   校验：库或前端来的码值必须在表内
//	写出   Value / MarshalJSON    校验：不在表内的码值不得落库、不得下发
//
// 只校验读入侧曾是本包的实际形态，实测后果是 `Role("root").Value()` 落库成功、
// 随后 Scan 读回报错——那一行**写得进去、永远读不出来**，且错误已离开造成它的
// 那次请求（K-3 整库导出与 K-2 审计分页会因一行坏数据整页失败）。JSON 侧同理：
// 下发一个前端拿得到、回传即失败的值。零值不受此限——它是 ObjectType 的合法取值。
//
// # String() 是中文名，Code() 才是码值
//
// 与 base/errs 的 Kind 同构：String() 返回中文名，仅供**日志与排错**阅读
// （约定 3「注释与日志一律中文」）；落库、JSON 与任何机器契约一律走 Code() /
// Value() / MarshalJSON()。切勿用 %s / %v 取码值——那会得到中文名。
//
// **面向用户的界面文案不在本包**。K-4 规定界面文案走语言配置文件，本包的中文名不是
// 文案源，L6 的下拉框应当「码值取自本包、标签取自 i18n」。唯一的例外是 ParserType
// 的名称——终版 §五 2 把「名称（机构 + 账单类型 + 格式）」明列为枚举项自身的属性，
// 它与支出类型名称同属「数据内容」，按 K-4 本就不纳入翻译。
//
// # 错误是哨兵，本包不选错误档位
//
// 解析 / 反序列化 / 扫描失败一律返回本包的哨兵错误，**不返回 base/errs 的分档错误**。
// 这不是偷懒：同一个「未知码值」在不同调用方是不同的档位——
//
//   - 经 api/http/dto 的 UnmarshalJSON 进来，是**用户输入错误**（外部提交了非法值）；
//   - 经 store/sqlite 的 Scan 出来，是**系统错误**（库里出现了不存在的码值，属数据
//     损坏，绝不能把 500 级的故障当成 400 级的用户错误吐给前端）。
//
// 本包无从判断自己被谁调用，一旦在此选定档位，必有一方是错的。故由调用方用
// errors.Is 判定哨兵后自行套 base/errs 的档位与文案键（与 base/calendar、
// base/decimal 的取向一致）。
//
// # 依赖边界
//
// 本包处于 L1.0，分层总表允许依赖 base/*，但**实际只 import 标准库**——7 个枚举
// 都不需要定点小数、日历日期或错误分档。这不是疏漏，是实现比允许边界更收敛。
//
// 全部枚举表在包初始化时构建完成，此后只读、不可变，可并发使用。表的自洽性
// （码值非空、码值不重复、ParserType 元数据齐备且格式合法）在包初始化时以 panic
// 校验：这类错误是编程错误，必须在进程启动的第一刻响亮失败，而不是等到某一行
// 数据落库时才发作。
package enum

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// 哨兵错误。上层用 errors.Is 判定后，按自身场景映射到 base/errs 的档位与文案键
// （为什么不在本包分档，见包注释「错误是哨兵」）。
var (
	// ErrUnknownCode 表示码值不属于该枚举。
	//
	// 两类来源需区别对待：来自 dto 的属用户输入错误；来自 store 扫描的属数据损坏，
	// 应按系统错误处理——枚举项只能停用不能删除，库里出现未知码值意味着有人删过
	// 枚举项，或者数据被外部写坏。
	ErrUnknownCode = errors.New("enum: 未知的枚举码值")

	// ErrCodeType 表示 JSON 值不是字符串形态（如传了数字或对象）。
	ErrCodeType = errors.New("enum: 枚举码值应为 JSON 字符串")

	// ErrScanType 表示从数据库扫描到的类型不受支持。
	ErrScanType = errors.New("enum: 不支持的数据库扫描类型")
)

// code 是 7 个枚举类型的公共约束：底层类型必为 string，即「码值就是落库的值」
// 这条契约在类型系统层面的表达。
type code interface{ ~string }

// entry 是一个枚举项的声明：码值 + 中文名。
type entry[T code] struct {
	code T
	name string
}

// set 是一个枚举类型的全部枚举项。
//
// 构造后只读，因此可并发使用；codes 保留声明序，names 供 O(1) 查名与判定合法性。
// 两份数据而非一份有序 map，是因为声明序必须稳定：K-1 的下拉、F-4 的筛选项都按它
// 呈现，用 map 遍历会每次给出不同的顺序。
type set[T code] struct {
	codes []T
	names map[T]string
}

// newSet 按声明序构建枚举表，并在包初始化期校验表自身的自洽性。
//
// 两条校验都以 panic 结束：它们是编程错误（写错了枚举声明），不是运行期可恢复的
// 状况。放在包初始化期意味着「一旦构建成功，进程启动即可用」，任何调用点都不必
// 再防御性判空。
func newSet[T code](entries []entry[T]) *set[T] {
	s := &set[T]{
		codes: make([]T, 0, len(entries)),
		names: make(map[T]string, len(entries)),
	}
	for _, e := range entries {
		//空串是全部枚举类型的零值（表示未指定 / 无对象），不能同时又是某个枚举项的
		//码值，否则 Valid() 与 IsZero() 会同时为真，零值判定彻底失效。
		if e.code == "" {
			panic("enum: 枚举码值不得为空串，空串是零值")
		}
		if _, dup := s.names[e.code]; dup {
			panic(fmt.Sprintf("enum: 枚举码值重复: %q", string(e.code)))
		}
		s.codes = append(s.codes, e.code)
		s.names[e.code] = e.name
	}
	return s
}

// all 返回全部码值的副本，保持声明序。
// 返回副本而非共享切片：调用方改动不得污染包级枚举表。
func (s *set[T]) all() []T {
	out := make([]T, len(s.codes))
	copy(out, s.codes)
	return out
}

// valid 报告码值是否属于本枚举。已停用的项同样返回 true——停用只影响能否被新选择，
// 不影响历史行的可读性（枚举项只能停用不能删除）。
func (s *set[T]) valid(c T) bool {
	_, ok := s.names[c]
	return ok
}

// name 返回中文名，仅供日志与排错。
//
// 零值返回「未指定」、未知码值返回「未知(码值)」而非空串：String() 会被 %v 隐式
// 调用，返回空串会让日志里出现看不见的字段，反而更难排查（与 errs.Kind 同口径）。
func (s *set[T]) name(c T) string {
	if n, ok := s.names[c]; ok {
		return n
	}
	if c == "" {
		return "未指定"
	}
	return fmt.Sprintf("未知(%s)", string(c))
}

// parse 把码值文本转为枚举值，**严格全等匹配**。
//
// 不做大小写折叠、不裁剪空白：码值是机器契约（库里的列值与 JSON 契约），不是用户
// 输入。宽松匹配会让「Admin」与「admin」两种写法同时可用，落库后成为两份互不相等
// 的历史数据，而这类问题在写入时毫无征兆。用户可读的宽松输入若将来确有需要，应在
// L6 显式转换后再进本包。
func (s *set[T]) parse(v string) (T, error) {
	c := T(v)
	if !s.valid(c) {
		var zero T
		return zero, fmt.Errorf("%w: %q", ErrUnknownCode, v)
	}
	return c, nil
}

// marshalJSON 恒序列化为 JSON 字符串；零值序列化为空串。
//
// 零值不输出 null：ObjectType 的零值是合法业务取值（无对象 / 批量），空串与 null
// 在 JSON 里都能表达它，但空串省去了前端的判空分支，也与 Value() 的落库形态一致。
//
// **同样校验码值合法性**，与 unmarshalJSON 对称，理由同 value：底层类型是 string，
// `enum.OperationType("rollback")` 这类强转在包外编译通过。实测不校验时的形态是
// 下发 `"rollback"` 给前端、前端原样回传即报 ErrUnknownCode——一个前端拿得到却
// 用不了的值。契约面必须只出现能被自己解回来的码值。
func marshalJSON[T code](c T, s *set[T]) ([]byte, error) {
	if c == "" {
		return json.Marshal("")
	}
	if !s.valid(c) {
		return nil, fmt.Errorf("%w: %q 不可下发（前端回传即失败）", ErrUnknownCode, string(c))
	}
	return json.Marshal(string(c))
}

// unmarshalJSON 反序列化并**校验码值合法性**。
//
// 校验而非照单收下，是因为 dto 是前后端 JSON 契约、属外部可控入口：若在此放行未知
// 码值，一个拼错的角色名就能一路写进库，之后所有按角色的判定都会静默走 else 分支。
// 与 base/decimal、base/calendar 的反序列化取向一致（都走各自的 Parse）。
//
// null、空串与字段缺失一律得零值且不报错——三者在 Go 中不可区分，行为必须一致。
func unmarshalJSON[T code](data []byte, dst *T, s *set[T]) error {
	var zero T
	if string(data) == "null" {
		*dst = zero
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%w: 实际为 %s", ErrCodeType, string(data))
	}
	if text == "" {
		*dst = zero
		return nil
	}
	parsed, err := s.parse(text)
	if err != nil {
		return err
	}
	*dst = parsed
	return nil
}

// value 实现落库形态：恒为字符串码值，零值落空串。
//
// 零值不落 NULL 的理由见包注释「零值一律是空串」——ObjectType 的零值是必填列上的
// 合法取值，落 NULL 会被 NOT NULL 约束拒绝。
//
// **写入侧同样校验码值合法性**，与 scan 对称。7 个枚举的底层类型都是 string，
// 于是 `enum.Role("root")` 这类强转在包外编译通过（不同于 currency.Code 的
// 私有字段构造，那种非法值造不出来）。若此处不校验，实测形态是：
//
//	Role("root").Value() → 落库成功 "root"，随后 Scan 读回报 ErrUnknownCode
//
// 即那一行**写得进去、永远读不出来**。校验放在写入侧才能让错误停在造成它的那次
// 请求上；放过它则错误被推迟到下一次读，且届时已无从判断是谁写坏的——K-3 整库导出
// 与 K-2 审计分页会因一行坏数据整页失败。零值不受此限（它是 ObjectType 的合法取值）。
func value[T code](c T, s *set[T]) (driver.Value, error) {
	if c == "" {
		return "", nil
	}
	if !s.valid(c) {
		return nil, fmt.Errorf("%w: %q 不可落库（写得进去也读不回来）", ErrUnknownCode, string(c))
	}
	return string(c), nil
}

// scan 从数据库读回枚举值，接受 NULL、string 与 []byte 三种形态。
//
// 同样校验码值合法性：库里出现未知码值意味着数据损坏（枚举项只能停用不能删除），
// 静默放行会让一行坏数据在后续每一次判定里都走错分支。NULL 与空串一律得零值，
// 兼容历史列或允许 NULL 的可选列。
func scan[T code](src any, dst *T, s *set[T]) error {
	var zero T
	switch v := src.(type) {
	case nil:
		*dst = zero
		return nil
	case string:
		return scanText(v, dst, s)
	case []byte:
		return scanText(string(v), dst, s)
	default:
		return fmt.Errorf("%w: %T", ErrScanType, src)
	}
}

// scanText 是 scan 中 string 与 []byte 两个分支的公共实现。
func scanText[T code](v string, dst *T, s *set[T]) error {
	var zero T
	if v == "" {
		*dst = zero
		return nil
	}
	parsed, err := s.parse(v)
	if err != nil {
		return err
	}
	*dst = parsed
	return nil
}
