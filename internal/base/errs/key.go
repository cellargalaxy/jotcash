package errs

// 文案键常量。
//
// 定位：文案键是 `base/errs` 与 `i18n` 之间的**契约**——本包只负责产出键与占位参数，
// 取词与填充由 `i18n` 按「语言 + 文案键」完成（`doc/decisions/仓库代码包的层级设计与划分.md`
// §六 L3 `i18n` 行）。因此本文件只声明键，**不含任何语种的文案内容**；
// 文案内容位于 `internal/i18n/locales/`（T37 随包 embed），加语种不改本文件（K-4）。
//
// 命名口径：`<域>.<对象>.<问题>`，全小写、点分。域名沿用需求文档的功能域字母含义，
// 但键本身用英文单词而非域编号——`parser.file.format_mismatch` 比 `d3.1` 可读。
//
// 收录范围：**只收录需求文档明确点名的错误提示**，避免凭空发明键。
// 依据逐条标注在各常量注释中；其余错误场景的键在实现对应包时按同一命名口径补充。
const (
	// —— 系统兜底（本包自用）——

	// KeySystemInternal 系统内部错误的兜底文案键。
	// 用于 From() 归一化非本包错误：middleware 拿到的错误一定有键可取词，
	// 不会出现「无键可取词」的出口。
	KeySystemInternal = "system.internal.error"

	// —— A 域 账户与认证 ——
	// 依据：终版 §二 A-6「除登录页与登录接口外，所有页面与接口强制校验；失效统一跳回登录页」，
	// 落地口径见 T34③「A-6 失效返 401、跳登录页由前端执行」。

	// KeyAuthNotLogin 未登录或登录态缺失（A-6）。
	KeyAuthNotLogin = "auth.session.not_login"
	// KeyAuthSessionExpired 登录态过期（A-5 签发起 12h 绝对过期、不可续期）。
	KeyAuthSessionExpired = "auth.session.expired"
	// KeyAuthSessionRevoked 登录态被基线时间作废（终版 §四 `User.登录态基线时间`：
	// 改密 / 被重置 / 被禁用时推进，签发时间早于它的令牌失效）。
	KeyAuthSessionRevoked = "auth.session.revoked"
	// KeyAuthForbidden 归属越权：访问的数据不属于当前用户（不变式 3）。
	KeyAuthForbidden = "auth.access.forbidden"
	// KeyAuthAdminRequired 需要管理员角色（A-7 账户管理仅管理员）。
	KeyAuthAdminRequired = "auth.access.admin_required"
	// KeyAuthPasswordChangeRequired 需强制改密期间访问了非改密接口
	// （A-2「首登强制修改」，拦截范围见 L6 `middleware` 行 ②）。
	KeyAuthPasswordChangeRequired = "auth.password.change_required"

	// —— A-8 密码策略 ——
	// 依据：终版 §二 A-8「长度 ≥ 10 且字母/数字/符号至少含两类；连续失败 5 次锁定 10 分钟」。

	// KeyPasswordTooShort 密码长度不足；占位参数 ArgMin 为要求的最小长度。
	KeyPasswordTooShort = "password.policy.too_short"
	// KeyPasswordCharClassInsufficient 密码字符类别不足（字母/数字/符号至少两类）；
	// 占位参数 ArgMin 为要求的最少类别数。
	KeyPasswordCharClassInsufficient = "password.policy.char_class_insufficient"
	// KeyAuthCredentialInvalid 账号或密码错误（A-4）。
	// 口径：不区分「用户名不存在」与「密码错误」，避免账号枚举。
	KeyAuthCredentialInvalid = "auth.credential.invalid"
	// KeyAuthAccountDisabled 账户被禁用（终版 §四 `User.状态`）。
	KeyAuthAccountDisabled = "auth.account.disabled"
	// KeyAuthAccountLocked 账户锁定期内（A-8 连续失败 5 次锁定 10 分钟）；
	// 占位参数 ArgUntil 为锁定截止时间。
	KeyAuthAccountLocked = "auth.account.locked"

	// —— D-3 五类解析错误 ——
	// 依据：终版 §二 D-3「格式不符、内容损坏、字段缺失、解密失败、所选解析器类型与文件不匹配」，
	// 五类与 §六 L3 `parser` 行的错误分类一一对应，一律返回带文案键的 base/errs。

	// KeyParserFormatMismatch 格式不符（D-3 第 1 类）。
	KeyParserFormatMismatch = "parser.file.format_mismatch"
	// KeyParserContentCorrupted 内容损坏（D-3 第 2 类）。
	KeyParserContentCorrupted = "parser.file.content_corrupted"
	// KeyParserFieldMissing 字段缺失（D-3 第 3 类）；占位参数 ArgField 为缺失的字段名。
	KeyParserFieldMissing = "parser.file.field_missing"
	// KeyParserDecryptFailed 解密失败（D-3 第 4 类，文件密码不正确或缺失）。
	KeyParserDecryptFailed = "parser.file.decrypt_failed"
	// KeyParserTypeMismatch 所选解析器类型与文件不匹配（D-3 第 5 类）。
	KeyParserTypeMismatch = "parser.type.mismatch"
	// KeyParserTypeUnknown 解析器类型键不存在或未启用（D-1 注册表未命中）。
	KeyParserTypeUnknown = "parser.type.unknown"

	// —— G-1 / A-7 重名（唯一冲突档）——
	// 依据：§六 L3 `store` 行 ⑤「`User.用户名` 全局唯一、`ExpenseCategory.(归属用户ID, 名称)` 唯一，
	// 由存储层唯一索引保证，冲突返回 base/errs 的『唯一冲突』档」。

	// KeyUserNameDuplicated 用户名已存在（A-7 新增账户）；占位参数 ArgName 为用户名。
	KeyUserNameDuplicated = "user.name.duplicated"
	// KeyCategoryNameDuplicated 同一用户下支出类型名称重复（G-1）；占位参数 ArgName 为类型名称。
	KeyCategoryNameDuplicated = "category.name.duplicated"

	// —— I-5 汇率兜底 ——
	// 依据：终版 §二 I-5「自动获取或重拉失败时该行汇率变为必填，未填不可提交 / 不可保存，
	// 不静默按 1:1 处理」。

	// KeyFxRateRequired 折算汇率必填未填（I-5）。
	KeyFxRateRequired = "fx.rate.required"
	// KeyFxRateFetchFailed 汇率获取失败（I-3 自动获取 / I-7 重拉失败）；
	// 占位参数 ArgCurrency、ArgDate 标明失败的币种与日期。
	KeyFxRateFetchFailed = "fx.rate.fetch_failed"

	// —— 通用输入校验 ——

	// KeyCurrencyInvalid 币种代码非法（终版 §五 1：合法性校验落在应用层）；
	// 占位参数 ArgCurrency 为传入的币种代码。
	KeyCurrencyInvalid = "currency.code.invalid"
	// KeyCurrencyNotBaseCandidate 该币种不可作本位币（T38：可作本位币 = 已启用 且 小数位数 ≤ 2）；
	// 占位参数 ArgCurrency 为传入的币种代码。
	KeyCurrencyNotBaseCandidate = "currency.code.not_base_candidate"
	// KeyLanguageInvalid 界面语言键非法（K-1 走 i18n 语言键校验）；
	// 占位参数 ArgLanguage 为传入的语言键。
	KeyLanguageInvalid = "language.key.invalid"
	// KeyRecordNotFound 记录不存在或不可见。
	// 口径：归属校验不通过时**不要**用本键——那属于 KindForbidden，见 KeyAuthForbidden。
	KeyRecordNotFound = "record.not_found"
	// KeyFieldRequired 必填字段缺失；占位参数 ArgField 为字段名。
	KeyFieldRequired = "field.required"
	// KeyFieldInvalid 字段取值非法；占位参数 ArgField 为字段名。
	KeyFieldInvalid = "field.invalid"

	// —— G-4 删除保护 ——
	// 依据：终版 §二 G-4「被明细引用的类型不可删除，已软删除的明细同样构成引用」。

	// KeyCategoryInUse 支出类型仍被明细引用，不可删除（G-4）；
	// 占位参数 ArgName 为类型名称、ArgCount 为引用笔数。
	KeyCategoryInUse = "category.in_use"
	// KeyExpenseDeletedReadonly 已删除明细只读、不可编辑（F-5 仅对未删除明细开放）。
	KeyExpenseDeletedReadonly = "expense.deleted.readonly"
)

// 占位参数名常量。
//
// 参数名是文案模板里的变量名，模板与代码必须用同一套名字，因此收敛为常量而非散落的字面量
// ——写错一个名字的后果是文案渲染出空值，且编译期无法发现。
const (
	// ArgName 名称：用户名、支出类型名称等。
	ArgName = "name"
	// ArgField 字段名。
	ArgField = "field"
	// ArgMin 下限值：最小长度、最少类别数等。
	ArgMin = "min"
	// ArgMax 上限值。
	ArgMax = "max"
	// ArgCount 数量：引用笔数、影响笔数等。
	ArgCount = "count"
	// ArgCurrency 币种代码。
	ArgCurrency = "currency"
	// ArgDate 日期。
	ArgDate = "date"
	// ArgLanguage 语言键。
	ArgLanguage = "language"
	// ArgUntil 截止时间：锁定截止时间等。
	ArgUntil = "until"
	// ArgType 类型：解析器类型键、文件格式等。
	ArgType = "type"
)
