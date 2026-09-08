package entity

import (
	"time"

	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// User 是用户，承载终版 §四 1 的 15 项属性，**全部必填**。
//
// 它是全模型的归属根：终版 §三 明写「5 个实体全部挂在 User 下，无系统级表」，
// 另外 4 个实体各自带一个 OwnerUserID 指回本实体的 ID。
//
// # 无删除标记，这是设计而非遗漏
//
// 终版前提 8 把 User 列为「一律不可删除」的三个实体之一（另两个是 File 与
// AuditLog），§四 1 亦明写「账户不可删除，无删除标记」。因此本结构体既无
// DeletedAt 也无 enum.UserStatus 的删除态（后者已由 enum 的
// TestUserStatusNoDeletedItem 锁死）。停用账户走 Status 字段，那是可逆的业务
// 状态开关，与「存在性单向不可逆」不冲突。
//
// # 三个业务状态位不受前提 8 约束
//
// Status、LockedUntil、MustChangePassword 三者的启停是设计内的可逆开关
// （终版前提 8 末句显式豁免）。它们与「删除」的区别在于：删除改变记录的存在性，
// 状态位只改变记录的可用性。
type User struct {
	// ID 用户ID，唯一标识，全部业务数据的归属键。
	//
	// 由 base/idgen.GenUserID() 生成后赋值（约定 9：标识由应用层生成，
	// store 不生成 ID、不依赖自增回填），本包只声明类型。
	// 恒为正整数，零值表示「未指定」。
	ID int64

	// Username 用户名，登录账号；管理员固定 admin（终版 A-1）。
	//
	// **全局唯一**，且该唯一性由存储层唯一索引保证而非先查后插
	// （分层 §六 L3 store ⑤、准则 6）——service/account 的先查只为友好提示。
	//
	// 无取值域可言，故用 string：它是用户自选的自由文本。
	Username string

	// PasswordHash 密码哈希，加盐哈希后的结果，明文任何环节不落库、也不进审计
	// （终版 §四 1）。形态为 base/pwdhash 的 Argon2id 输出（带版本前缀）。
	//
	// # json:"-" 是密码红线的兜底，不是契约声明
	//
	// 本包不加 json 标签（见包注释「三样刻意不做」第 2 条），此处与 Salt 是唯一
	// 两个例外。分层 §九 红线行定死「密码哈希 与 盐 连三类出口都不得出现——dto
	// 不定义、A-7 列表裁剪、K-3 导出裁剪」，三处裁剪都是「记得做才有效」的运行期
	// 约定；`json:"-"` 让「万一有人直接 json.Marshal 了 entity.User」这条路径
	// 也漏不出去。它不表示本结构体是 JSON 契约——dto 仍必须自建结构体。
	//
	// 注意它管不住日志：`%+v` 仍会打出本字段，见包注释末节的已登记残留。
	PasswordHash string `json:"-"`

	// Salt 盐，与 PasswordHash 配对（终版 §四 1）。
	//
	// **独立列存而非拼进哈希串**：分层 §六 L0 base/pwdhash 行明写「盐独立列存
	// （终版 §四 User.盐 是单列属性，不用自包含哈希串格式）」。
	//
	// json:"-" 的理由同 PasswordHash。
	Salt string `json:"-"`

	// Role 角色，管理员 / 普通用户；A-7 新建账户一律为普通用户（终版 §四 1）。
	//
	// 用 enum.Role 而非裸 string（分层 §六 L1.1）。注意前提 3：管理员特权仅限
	// 账户管理域且只作用于 User 实体，故本字段不参与另外 4 个实体的归属过滤。
	Role enum.Role

	// Status 状态，启用 / 禁用，默认启用（终版 §四 1）。
	//
	// 与 LockedUntil 正交：本字段是 A-7 管理员显式启停的持久状态，锁定是 A-8
	// 连续失败触发的临时状态，一个账户可以同时被禁用且处于锁定期内。
	Status enum.UserStatus

	// MustChangePassword 需强制改密，新建账户与被重置密码后为是；为是时只能走
	// 改密流程（终版 §四 1 / A-2）。
	//
	// 拦截点在 api/http/middleware 的 /api/ 组链（分层 §六 L6.1：「A-2 需强制
	// 改密拦截（为是时除改密接口外一律拦截）」），不在本包。
	MustChangePassword bool

	// FailedAttempts 连续失败次数，默认 0；连续 5 次锁定 10 分钟（终版 §四 1 / A-8）。
	//
	// 阈值 5 与时长 10 分钟**不在本包也不在 config**——按约定 8「业务阈值一律
	// 写死在对应包」，二者写死在 service/auth。本包只承载计数值本身。
	FailedAttempts int

	// LockedUntil 锁定截止时间，**零值 = 未锁定**（终版 §四 1）。
	//
	// 用 time.Time 而非 calendar.Date：前提 7 的「不做时区换算」只约束支出日期
	// （分层 §九 前提 7 行末句明写「业务时间戳如创建/更新/操作时间不受此限」），
	// 而锁定是一个精确到秒的时刻判定，本就该带时区。
	LockedUntil time.Time

	// TokenBaselineTime 登录态基线时间，签发时间早于它的令牌失效；改密 / 被重置 /
	// 被禁用时推进（终版 §四 1 / §五 3）。
	//
	// 这是「无状态令牌 + 可作废」得以成立的唯一支点：终版 §六 刻意不建登录态实体，
	// 作废靠本字段而非令牌黑名单，代价是每请求多读一次用户（终版 §五 3 已登记）。
	// 比对逻辑在 service/auth，不在 token 包（后者不读库）。
	TokenBaselineTime time.Time

	// BaseCurrency 本位币，币种枚举代码，默认 CNY（终版 §四 1 / I-2）。
	//
	// 取值域**不是全部币种**：T38 把本位币限定为「已启用 且 小数位数 ≤ 2」，
	// 校验入口是 currency.ParseBase，调用方为 service/preference（K-1）。
	// 本字段同时是 I-7 判断某笔明细是否已收敛的基准——
	// Expense.BaseCurrency ≠ 本字段 即待重算（终版 §四 3 / 不变式 2）。
	BaseCurrency currency.Code

	// Language 界面语言，语言配置文件的键，默认中文；由 K-1 维护（终版 §四 1）。
	//
	// # 全包唯一「本该有取值域却只能用 string」的字段
	//
	// 终版 §四 1 定义它是「语言配置文件的键」，而语言集由 i18n 持有且在编译期
	// 确定（分层 §六 L3 i18n 行：语言文件随包 embed）。i18n 在 L3，本包（L1.1）
	// import 它会反向依赖成环——分层 §七 依赖图里没有 L1.1 → L3 这条边。
	//
	// 故本字段用 string，合法性由 service/preference 调 i18n 的「语言键合法性
	// 校验」兜住（分层 §六 L5.4 preference 行：「界面语言走 i18n 语言键校验，
	// 两者都不接受任意值」）。这与 currency.Code 的「反序列化即校验」形成对比，
	// 差异纯粹来自依赖方向，不是口径不一致。
	Language string

	// CreatedAt 创建时间（终版 §四 1）。
	CreatedAt time.Time

	// UpdatedAt 更新时间（终版 §四 1）。
	UpdatedAt time.Time

	// LastLoginTime 最后登录时间，**零值 = 从未登录**（终版 §四 1）。
	//
	// 由 service/auth 在 A-4 登录成功时刷新；唯一消费方是 A-7 的账户列表
	// （分层 §九 校验方式④ 把它列为「反向校验的四个薄弱项」之一，落点为 A-7）。
	LastLoginTime time.Time
}
