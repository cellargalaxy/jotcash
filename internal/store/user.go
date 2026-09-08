package store

import (
	"context"
	"time"

	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/entity"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// UserRepo 是 User 仓储（承载 ②）。
//
// 分层 §六 L3 store ②：「**User 仓储**——新增、按用户名查、按用户ID 查、列表、
// **按用户ID 更新**（密码哈希/盐、状态、需强制改密、连续失败次数、锁定截止时间、
// 登录态基线时间、最后登录时间、本位币、界面语言），**不带归属参数**（A-1/A-4/A-7
// 无「当前归属用户」），越权由服务层按 §九 白名单显式校验，**无删除方法**」。
//
// # 这是唯一签名不带归属的仓储，它的调用方是一份白名单
//
// §九 不变式 3 行：「**User 仓储是唯一签名不带归属的仓储，调用方共 5 个**：
// service/auth（A-4）、service/account（A-1/A-7/A-8）、service/preference（K-1 写
// 偏好）、service/export（K-3 导出本人）、service/fx（I-7 读本位币）。**白名单**：
// 仅 A-1 初始化、A-4 按用户名登录、A-7 管理员按目标用户ID 三类可访问非自身记录，
// **其余一律只能以当前登录用户ID 访问自身**」。
//
// 这条**在本包无编译期兜底**（§四 R2 已把它登记为残留），因为 A-1 建首个 admin
// 时根本没有「当前用户」，A-4 登录时也还没有。本包能做的是让形状差异醒目：
// 其余 4 个仓储的首参是具名类型 OwnerUserID，本仓储用裸 int64，评审时一眼可见
// 「这个方法没有归属过滤」。TestUserRepoHasNoOwnerParam 把这条形状差异钉住。
//
// # 无删除方法（前提 8）
//
// 承载 ② 末句与 §九 前提 8 行都明写这条。A-7 的账户管理只有「新增 / 启停除自身外
// 账户 / 重置他人密码 / 查列表」，**没有删除**——禁用（enum.UserStatusDisabled）
// 是唯一的退场方式。故本接口不得出现任何删除方法。
//
// # 返回值含密码哈希与盐，调用方必须裁剪（红线 4）
//
// 本仓储的全部读方法返回**完整**的 entity.User，含 PasswordHash 与 Salt。
// 这是刻意的：service/auth 的 A-4 校验需要它们，而 base/pwdhash 是白名单四包之一。
//
// 裁剪的责任在调用方，且分层已定死两个裁剪点：
//
//	A-7 账户列表与详情   service/account「**列表与详情裁剪 密码哈希、盐**」
//	K-3 整库导出         service/export「**User 裁剪 密码哈希/盐**」
//
// 本包**不**提供一个「安全视图」结构来代劳。若提供了，裁剪点就有两处（store 一处、
// service 一处），而两处裁剪必然漂移——将来加一个敏感字段时，只改了其中一处。
// 更糟的是「store 已经裁过了」会让 service 侧的裁剪看起来是多余的，被后来人删掉，
// 而那时 service/auth 需要的完整读取又必须绕过裁剪，于是回到两条路径。
//
// 注意 entity.User 的 PasswordHash 与 Salt 已带 `json:"-"` 标签，那是**序列化**
// 层面的兜底，不是裁剪——K-3 导出若走自定义编码器，标签不起作用。
type UserRepo interface {
	// Create 新增用户（A-1 系统初始化建 admin、A-7 新增账户）。
	//
	// user.ID 必须由调用方经 base/idgen.GenUserID 生成（约定 9：实体标识一律由
	// base/idgen 生成，**不依赖自增回填**）。实现不得自行生成或覆盖它。
	//
	// # 用户名唯一由唯一索引保证，重复初始化天然幂等
	//
	// 承载 ⑤：「User.用户名 全局唯一，**由存储层唯一索引保证**，冲突返回
	// base/errs 的「唯一冲突」档（先查后插只用于友好提示，不作正确性依据）」。
	// 故本方法在用户名重复时返回 KeyUsernameConflict（409）。
	//
	// service/account 行进一步说明 A-1「用户名唯一由 store 唯一索引保证，
	// **重复初始化天然幂等**」——app 每次启动都调用一次 A-1 初始化，第二次起
	// 撞唯一索引，service/account 把这个 409 当成「已初始化过」正常放行。
	// 这条幂等性直接依赖于「冲突必须是 409 而不是 500」，故 L4 实现必须把驱动的
	// 唯一约束违反准确映射过来（用 WrapUsernameConflictError）。
	Create(ctx context.Context, user entity.User) error

	// CreateTx 同 Create，在给定事务内执行（约定 7：句柄首参）。
	//
	// A-1 初始化要「先落审计取 ID → 业务数据挂载该 ID → 同事务提交」
	// （不变式 4 的同事务审计语义，8.4 第 1 类「系统初始化」）。
	CreateTx(ctx context.Context, tx TxHandle, user entity.User) error

	// GetByID 按用户ID 查用户。
	//
	// 白名单第三类（A-7 管理员按目标用户ID）与「其余一律只能以当前登录用户ID
	// 访问自身」都走本方法——**本方法不区分这两者**，区分在服务层。
	//
	// 取不到时返回 KeyNotFound（见 errors.go 的红线 2）。
	GetByID(ctx context.Context, userID int64) (entity.User, error)

	// GetByUsername 按用户名查用户（A-4 登录，白名单第二类）。
	//
	// # 用户名不存在时返回 KeyNotFound，而调用方不得把它透给前端
	//
	// A-4 的失败审计遵 8.4 第 2 类的「**有主则记、无主不记**（用户名不存在时
	// 不记）」（分层 §六 L5 service/auth 行），故 service/auth 需要能区分
	// 「用户名不存在」与「密码错」——本方法的 KeyNotFound 正是那个判据。
	//
	// 但**登录接口回给前端的提示必须一致**（否则用户名可被枚举），这条在
	// service/auth 落实，不在本包。本包只负责如实报告。
	GetByUsername(ctx context.Context, username string) (entity.User, error)

	// List 列出全部用户（A-7 账户管理页）。
	//
	// 顺序按用户ID 升序——idgen 的 ID 含时间前缀，故它等价于「按创建先后」，
	// admin（A-1 建的第一个）恒在最前。顺序必须稳定，理由同 ExpenseSort.Normalize。
	//
	// # 为什么不分页
	//
	// A-7 是单人自建的记账系统的账户管理页，用户数量级是个位数到十位数
	// （终版前提 1 的场景是个人/家庭记账）。分页在这里只增加接口复杂度。
	// 若将来确实需要，加一个 ListPage 方法即可，不影响现有调用方。
	//
	// **返回值含密码哈希与盐，调用方必须裁剪**（红线 4）。
	List(ctx context.Context) ([]entity.User, error)

	// UpdateCredential 更新密码哈希与盐，并**推进登录态基线时间**（A-8 改密、
	// A-7 重置他人密码）。
	//
	// # 为什么把基线时间与密码绑在一个方法里
	//
	// 分层 §六 L5 service/account 行：「**改密/重置/禁用推进基线时间**」。
	// 基线时间是 A-6 校验「令牌是否在改密前签发」的依据（entity.User
	// .TokenBaselineTime）：改了密码但没推进基线，旧令牌继续有效——这正是
	// 「重置了被盗账户的密码，攻击者的会话仍然活着」。
	//
	// 拆成两个方法（改密 + 推基线）就意味着有一条「只改密码不推基线」的合法
	// 调用路径，而它在任何时候都是错的。绑成一个方法后，那条路径在本层不存在。
	//
	// baselineTime 由调用方传入而非实现取 time.Now()：A-7 重置密码时要与审计
	// 记录用同一个时刻，且可测试（实现取 Now 的方法无法在测试里断言基线值）。
	UpdateCredential(ctx context.Context, userID int64, passwordHash, salt string, baselineTime time.Time) error

	// UpdateCredentialTx 同 UpdateCredential，在给定事务内执行。
	//
	// A-8 改密与 A-7 重置都要记 8.4 第 4 类「密码修改」/第 3 类「账户管理」审计，
	// 走同事务语义。
	UpdateCredentialTx(ctx context.Context, tx TxHandle, userID int64, passwordHash, salt string, baselineTime time.Time) error

	// UpdateStatus 更新账户状态（A-7 启停），并推进登录态基线时间。
	//
	// 基线时间一并推进，理由同 UpdateCredential：「禁用推进基线时间」
	// （service/account 行）。禁用了账户但不推基线，被禁用者的现有令牌仍能
	// 通过 A-6 校验，禁用形同虚设。
	//
	// **A-7 不得停用自身**（承载「启停除自身外账户」），该校验在
	// service/account——本包不知道「当前登录用户是谁」（约定 6：L5 一律不依赖
	// ctxs，用户ID 由 L6 显式传入），无法做这个判断。
	UpdateStatus(ctx context.Context, userID int64, status enum.UserStatus, baselineTime time.Time) error

	// UpdateStatusTx 同 UpdateStatus，在给定事务内执行。
	UpdateStatusTx(ctx context.Context, tx TxHandle, userID int64, status enum.UserStatus, baselineTime time.Time) error

	// UpdateMustChangePassword 更新「需强制改密」标记（A-2 初始密码生命周期）。
	//
	// A-2 的链路是「生成 → 交付一次 → **强制改密**」：新建账户与重置密码后置为
	// 真，用户完成自助改密后置为假。middleware 的 /api/ 组链据此拦截
	// （「A-2 需强制改密拦截（为是时除改密接口外一律拦截）」）。
	//
	// 与 UpdateCredential 分开：改密成功时两者都要改，但**顺序与事务性由服务层
	// 决定**；而 A-7 重置他人密码时置为真、A-8 自助改密时置为假，两条链路对本
	// 标记的取值相反，绑在一起就要多一个布尔参数，反而更容易传错。
	UpdateMustChangePassword(ctx context.Context, userID int64, mustChange bool) error

	// UpdateMustChangePasswordTx 同上，在给定事务内执行。
	UpdateMustChangePasswordTx(ctx context.Context, tx TxHandle, userID int64, mustChange bool) error

	// UpdateLoginFailure 更新连续失败次数与锁定截止时间（A-8 失败计数与锁定）。
	//
	// A-8 的「5 次 / 10 分钟」阈值**写死在 service/auth**（分层 §六 L5
	// service/auth 行：「A-8 失败计数与 10 分钟锁定（**5 次 / 10 分钟写死在
	// 本包**，终版 A-8 是业务规则）」），本包只负责落这两个字段的值，
	// 不判断「是否该锁了」。
	//
	// 两个字段绑在一个方法里：它们恒同时变化（第 5 次失败 → 次数置 5 且写锁定
	// 截止时间；成功登录 → 次数归零且清空锁定）。分开会出现「次数归零了但锁还在」
	// 这种状态，表现为「密码明明对了却说被锁定」。
	//
	// lockedUntil 传零值即清空锁定（entity.User.LockedUntil 的零值语义）。
	UpdateLoginFailure(ctx context.Context, userID int64, failedAttempts int, lockedUntil time.Time) error

	// UpdateLastLoginTime 更新最后登录时间（A-4「登录成功即刷新最后登录时间」）。
	//
	// 单独一个方法：它只在登录成功时写，与失败计数的清零虽同时发生但语义不同
	// ——前者是 A-7 账户列表要展示的信息（§十二 曾把「最后登录时间 → A-7」
	// 列为四个薄弱项之一并已确认落点），后者是 A-8 的安全状态。
	UpdateLastLoginTime(ctx context.Context, userID int64, lastLoginTime time.Time) error

	// UpdatePreference 更新用户偏好：本位币与界面语言（K-1）。
	//
	// # 取值合法性由服务层校验，本包不校验
	//
	// 分层 §六 L5 service/preference 行：「K-1 本位币与界面语言切换（**本位币走
	// currency 的「本位币候选」校验（T38，非候选一律拒绝）、界面语言走 i18n
	// 语言键校验**，两者都不接受任意值）」。
	//
	// 本包不重复校验，理由是「两处校验等于两处口径」——尤其本位币候选的判据是
	// currency.Code.CanBeBase()，而语言键的判据在 i18n 包，本包 import i18n 会
	// 违反分层（store 的依赖列只有 entity、enum、currency）。
	//
	// 但 currency.Code 的类型本身已经保证「要么零值、要么是枚举表内已知币种」
	// （该类型的注释），故本层不可能落进一个 Code("蛋炒饭")。
	//
	// # 切换本位币之后必须触发 I-7，那不在本包
	//
	// K-1：「切换本位币会落两条审计：用户偏好设置（必成功）+ 本位币金额重算
	// （允许部分失败可续跑）」。第二条由 service/preference 调 service/fx 触发。
	// 本方法只改 User 上的字段，**不**级联更新任何 Expense——那是 I-7 的逐笔
	// 小事务（不变式 2），绝不能并进这一次更新。
	UpdatePreference(ctx context.Context, userID int64, baseCurrency currency.Code, language string) error

	// UpdatePreferenceTx 同 UpdatePreference，在给定事务内执行。
	//
	// K-1 的「用户偏好设置」审计必成功，走同事务语义。
	UpdatePreferenceTx(ctx context.Context, tx TxHandle, userID int64, baseCurrency currency.Code, language string) error
}
