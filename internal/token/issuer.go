package token

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/cellargalaxy/jotcash/internal/base/idgen"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// Payload 是登录令牌的载荷，四个字段与分层 §六 L3 token 行的承载
// 「用户ID / 角色 / 签发时间 / 过期时间」逐项对应，一项不多一项不少。
//
// # 为什么恰是这四项
//
// 终版 §五 3 定死了这四项。**不多**：不放用户名（会随 A-7 改名而失真，且它是
// 自由文本）、不放本位币与界面语言（K-1 可随时改，令牌里的会是过期快照）、
// 不放基线时间（那是用来作废令牌的，放进被作废方等于让它自证清白）、
// 不放密码哈希与盐（分层 §九 红线，且载荷是明文可读）。**不少**：少了角色则
// A-7 的管理员判定要每请求查库；少了签发时间则基线时间无从比对。
//
// # 时间字段一律是秒级
//
// IssuedAt 与 ExpiresAt 恒为**整秒**（纳秒部分为 0），因为 JWT 的 NumericDate
// 是秒级的（库 TimePrecision 默认 time.Second，实测会把 10:00:00.987654321
// 截断成 10:00:00）。Issue 因此先把 now 截断到秒再算 exp，并把截断后的载荷作为
// 返回值交出——这样调用方手里的时间与令牌内的值**逐字节一致**，不会出现
// 「按返回值推进基线时间，却与令牌里的 iat 差半秒」这种误差。
//
// 这条对 service/auth 的基线时间比对至关重要，其实测坑与正确口径见包注释
// 「秒级截断」一节。
//
// # 时区
//
// 两个时间字段带时区（time.Time 由库从 Unix 秒还原，实测为本地时区表示的同一
// 时刻）。这与 base/calendar.Date「不带时区的字面值」（前提 7）不同，不是口径
// 不一致：支出日期是人写在账单上的日历日，而令牌签发与过期是**精确到秒的时刻**，
// 本就该带时区。与 entity.User 的 LockedUntil 同理（见其注释）。
//
// 比较时刻一律用 time.Time.Before / After / Equal，不比较字段。
type Payload struct {
	// UserID 用户ID，全部业务数据的归属键（终版 §四 1）。
	//
	// 恒为正：签发前经 idgen.IsValid 校验，解析后经 idgen.ParseString 复校，
	// 非法一律报 ErrUserIDInvalid。它在令牌里以 JWT 标准的 sub 字段承载，
	// 且**以字符串形态**——理由见 issuedClaims.Subject。
	UserID int64

	// Role 角色，管理员 / 普通用户（终版 §四 1）。
	//
	// 用 enum.Role 而非裸 string（分层 §六 L1.1 的取向）。两侧都校验
	// Valid / ParseRole：库对业务码值一概不管，实测 role 为 "" 或 "root"
	// 都能正常解析出来。
	//
	// 注意前提 3：管理员特权仅限 A-7 账户管理，**没有业务数据特权**。
	// 本字段只是把角色如实带到 middleware，具体特权判定不在本包。
	Role enum.Role

	// IssuedAt 签发时间，整秒。
	//
	// 它是 service/auth 做基线时间比对的**唯一输入**（终版 §四 1：
	// 「签发时间早于它的令牌失效」）。因此 Parse 在 iat 缺失时必须报错而不能
	// 放行——一个没有签发时间的令牌永远无法被改密 / 重置 / 禁用作废。
	IssuedAt time.Time

	// ExpiresAt 过期时间，整秒，恒等于 IssuedAt + TTL。
	//
	// 这条恒等式由 Issue 保证、由 Parse **断言**：库不校验 exp 与 iat 的间距
	// （实测 exp = iat + 365 天 的令牌解析通过），不断言就等于 A-5 的 12 小时
	// 可被任意绕过。不满足即报 ErrTokenClaims。
	ExpiresAt time.Time
}

// IsZero 报告载荷是否为零值。
//
// 用于表达「解析失败，无可用载荷」：Parse 在任何错误路径上都返回零值 Payload
// （理由见 Parse 的「失败即零值」一节）。
func (p Payload) IsZero() bool {
	return p.UserID == 0 && p.Role.IsZero() && p.IssuedAt.IsZero() && p.ExpiresAt.IsZero()
}

// String 给出便于排错的文本形态。
//
// 只含四个载荷字段，**不含令牌串本身**——令牌等价于口令，打进日志即等于把
// 登录态写进持久介质。本包不打日志（见包注释），但仍要保证「上层若打了
// Payload，也不会连带泄露令牌」。
func (p Payload) String() string {
	return fmt.Sprintf("token.Payload{用户ID:%d, 角色:%s, 签发:%s, 过期:%s}",
		p.UserID, p.Role.Code(),
		p.IssuedAt.Format(time.RFC3339), p.ExpiresAt.Format(time.RFC3339))
}

// issuedClaims 是本包写进令牌的 claims 结构，不导出。
//
// # 为什么用结构体而不是 jwt.MapClaims
//
// MapClaims 是 map[string]any，取值要逐个断言类型，且**类型不符时静默得到零值**。
// 结构体让 json 解码在类型不符时直接失败（实测 role 是数字 123 而非字符串时
// 报 could not JSON decode claim，本包归 ErrTokenMalformed），错误出现在解析
// 现场而不是若干层之后的类型断言里。
//
// # 为什么 sub 是字符串
//
// 用户ID 是 int64，但令牌里以字符串承载，理由与 base/idgen.String 的注释同一条：
// 18 位 ID 超过 float64 的安全整数上界 2^53，实测 260906160443337625 经 float64
// 往返后变成 260906160443337632。JWT 的 sub 若写成数字，任何按 JSON Number
// 解码的一方（含浏览器里解令牌看载荷的调试代码）拿到的就是另一个 ID，且不报错。
// jwt.RegisteredClaims.Subject 本身即 string，与这条要求天然吻合。
type issuedClaims struct {
	// Role 角色码值。放在标准字段之外，用短键名 role。
	//
	// 承载要求载荷含角色，而 JWT 的标准字段里没有合适的位置——不能借用 scope 或
	// aud：aud 有其既定语义（受众），挪用会让将来真要用它时无法区分。
	Role string `json:"role"`

	// RegisteredClaims 提供 sub / iat / exp 三个标准字段。
	//
	// 用标准字段而不自造 user_id / issued_at / expires_at 三个私有键，是为了让
	// 库的校验逻辑（WithExpirationRequired / WithIssuedAt 等）能真正生效——
	// 那些选项只认标准字段。自造键的话校验就全要自己写一遍。
	jwt.RegisteredClaims
}

// Issuer 持有令牌密钥，是本包的签发与解析入口。
//
// # 为什么是结构体而不是包级密钥变量
//
// 密钥是**注入参数**（承载：仅密钥由 app 注入）。若用包级 var secret，
// 「注入」就退化成一个任何包都能改写的全局可变状态，且单测无法并行（两个用例
// 各设一次密钥就会互相干扰）。用结构体后密钥随实例走，构造完即只读——
// 与 fxrate.Registry「构造时填好、之后只读、无写入口」是同一手法（准则 6）。
//
// 零值 *Issuer 是 nil，误用会当场 panic 而非静默用空密钥签发。这是刻意的：
// 空密钥能签出可验证的令牌（实测），静默失败比 panic 危险得多。
//
// 构造后的 Issuer **并发安全**：无可变状态，Issue 与 Parse 都只读 secret。
// A-6 每请求调 Parse，这条是必要的。
type Issuer struct {
	// secret 是 HMAC 密钥的字节形态。
	//
	// 存 []byte 而非 string 是因为库只接受 []byte：实测密钥回调返回 string 会
	// 报 "HMAC verify expects []byte"。在构造时转一次，避免每次签发解析都转、
	// 也避免把这个易错点留在调用处。
	secret []byte
}

// New 按注入的密钥构造 Issuer，由 app 在启动装配时调用一次
// （分层 §六 L7 app 行：「**向 token 注入密钥**」）。
//
// # 两项密钥校验，全部在构造时完成
//
// 密钥的强度**只有本包能拦**——实测库对密钥长度毫无门槛：1 / 8 / 16 / 31 / 32 /
// 64 字节全部正常工作，连 []byte("") 与 []byte(nil) 都能签出可验证的令牌，
// err 全程为 nil。于是：
//
//	① 非空（去空白后）  → ErrSecretEmpty
//	② 长度 ≥ MinSecretLength → ErrSecretTooShort
//
// 放在构造时而不是每次签发时，是为了让「密钥太弱」在**进程启动**时就失败，
// 而不是等到第一个用户登录。这与 fxrate.NewRegistry 把全覆盖校验放在构造时
// 是同一取向：接线问题必须在启动阶段暴露。
//
// # 密钥不做 TrimSpace
//
// 判空时去空白，但**实际使用原串**。理由是密钥是二进制熵的文本载体，
// 首尾空白也是熵的一部分；悄悄裁掉会让「配置文件里多敲了一个空格」变成
// 「换了一把密钥」，而后果是全部存量令牌失效却查不出原因。base/config 亦
// 「原样保留」密钥（其单测 TestLoad… 断言 token_secret 为 "  keep me  " 时
// 原样保留）。两处口径因此一致。
//
// 错误里**不含密钥内容**，只含长度——错误会流向日志（base/errs 包注释），
// 把密钥写进去等于泄露。
func New(secret string) (*Issuer, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, ErrSecretEmpty
	}
	//长度按字节计而非 rune：HMAC 消费的是字节，与算法口径一致。
	//这与 rule/passwd 按 rune 计长度不同——那边量的是「用户看到几个字符」，
	//这边量的是「喂给 HMAC 多少熵」，是两件事。
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("%w: 实际 %d 字节，至少需要 %d 字节",
			ErrSecretTooShort, len(secret), MinSecretLength)
	}
	return &Issuer{secret: []byte(secret)}, nil
}

// Issue 按用户ID 与角色签发令牌，返回令牌串与**实际写进令牌的载荷**。
//
// 由 service/auth 在 A-4 登录成功后调用。
//
// # 三个返回值的分工
//
// 第二个返回值是本函数存在的一半理由：它是**截断到秒之后**的载荷，与令牌内的
// iat / exp 逐字节一致。调用方（service/auth 记录最后登录时间、将来推进基线
// 时间）必须用它，不能自己拿 time.Now() 推算——实测 NewNumericDate 会截断亚秒，
// 自己算出来的时间会比令牌里的大出最多 999999999 纳秒，而基线时间比对正是
// 拿这两个值比大小的（实测坑见包注释「秒级截断」）。
//
// # now 为什么是入参
//
// 时间必须可控，否则「12h 边界」「过期 1 秒」这类用例无法确定性地测（实测
// 有效期是左闭右开 [iat, iat+TTL)，边界只差 1 秒，靠 sleep 测不了）。
// 仓库尚无时钟注入的先例，故取最简形式：入参，不引入 Clock 接口——
// 一个接口只为一个函数服务时，它带来的抽象成本高于收益。
//
// 调用方传 time.Now() 即可；now 为零值也允许（签出一个 1970 年签发的令牌），
// 本包不拦——拦它等于替调用方判断「什么时间算合理」，而单测恰恰需要构造
// 各种时间。真实链路里 now 恒来自 time.Now()。
//
// # 入参校验
//
// 用户ID 与角色都校验，因为库一概不管（实测 sub 为 "0"/"-1"/"abc"、
// role 为 ""/"root" 全部能签出并解析成功）。签发侧不拦的后果是把非法载荷
// **签上了合法签名**——那之后任何一方都无法再判定它非法，只能照单执行。
func (i *Issuer) Issue(p Payload, now time.Time) (string, Payload, error) {
	if !idgen.IsValid(p.UserID) {
		return "", Payload{}, fmt.Errorf("%w: %d", ErrUserIDInvalid, p.UserID)
	}
	//用 Valid 而非判空：enum.Role 底层是 string，enum.Role("root") 这类强转在
	//包外编译通过（见 enum 包注释），只判空拦不住它。
	if !p.Role.Valid() {
		return "", Payload{}, fmt.Errorf("%w: %q", ErrRoleInvalid, p.Role.Code())
	}

	//先截断到秒，再算 exp。顺序不能反：若先算 exp 再各自截断，
	//exp - iat 仍是 12h（都截断了同样的亚秒），但那是巧合而非保证——
	//先截断能让「exp = iat + TTL」这条恒等式在任何入参下都精确成立。
	issuedAt := now.Truncate(time.Second)
	expiresAt := issuedAt.Add(TTL)

	claims := issuedClaims{
		Role: p.Role.Code(),
		RegisteredClaims: jwt.RegisteredClaims{
			//sub 用 idgen.String 而非 strconv：前者是全仓库「ID → 规范文本」的
			//唯一入口，与 Parse 侧的 ParseString 严格互逆，保证一个 ID 只有一种
			//文本写法（详见 idgen.go 的 ParseString 注释）。
			Subject:   idgen.String(p.UserID),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	//签名方法写死 HS256，与 Parse 的白名单是同一个值。
	//不做成参数：算法是安全口径而非部署差异，可配置等于给了一个能配成 none 的入口
	//（实测 alg=none 确实能签出令牌）。
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		//正常不可达：入参已校验、密钥已在 New 校验、HS256 对 []byte 密钥无其他
		//失败条件。仍如实上抛而不 panic——库将来变更行为时，这里应当报错而不是崩。
		//错误里不带令牌与密钥，只带用户ID。
		return "", Payload{}, fmt.Errorf("token: 签发失败，用户ID %d: %w", p.UserID, err)
	}

	//交出的载荷用截断后的时间，与令牌内容一致（见上文「三个返回值的分工」）。
	return signed, Payload{
		UserID:    p.UserID,
		Role:      p.Role,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// Parse 校验并解析令牌，返回载荷。
//
// 由 middleware 经 service/auth 在 A-6 全局登录态校验中**每请求**调用一次。
//
// # 本函数只判「这个令牌本身是否有效」
//
// 不判基线时间（需读库，归 service/auth）、不判账户是否被禁用或锁定（同上）、
// 不判是否需强制改密（A-2 拦截在 middleware）。返回 nil error 只意味着
// 「令牌是本密钥签的、未过期、载荷符合口径」，**不意味着这个登录态可用**。
//
// # 失败即零值：一条实测出来的必要设计
//
// 任何错误路径都返回零值 Payload。这不是风格取舍——实测库在**签名校验失败时
// 已经把载荷填进了传入的 claims 结构**：把某个令牌的 sub 从真实用户改成 "999"
// 后，Parse 返回签名错误，但 claims 里的 sub 已是 "999"。若本函数把已填充的
// 载荷连同错误一起返回，调用方一个「err 非 nil 但顺手用了载荷」的写法，就等于
// 直接采信攻击者构造的用户ID。返回零值让这条路径在语法上就不存在。
//
// # 四个解析选项恒开，一个都不能省
//
//	WithValidMethods(["HS256"])  只认 HS256。库虽内置拒 alg=none，但白名单是
//	                             正面收口——实测此时报 "signing method none is
//	                             invalid"，且顺带拦下所有算法混淆变体。
//	WithExpirationRequired()     库**默认放行无 exp 的令牌**（实测 err 为 nil），
//	                             即一个永不过期的令牌。A-5 是绝对过期，必须要求。
//	WithIssuedAt()               库**默认不校验 iat**（实测 iat 在未来 1 小时仍
//	                             放行）。未来签发时间会让基线时间比对失去意义。
//	WithTimeFunc(now)            时间可控，否则 12h 边界无法确定性测试。
//
// 此外还有两项库**不做**、本函数自己做的校验：iat 是否存在（WithIssuedAt 在
// iat 缺失时照样放行，实测 err 为 nil 且 IssuedAt 为 nil）、exp - iat 是否恰为
// TTL（实测 exp = iat + 365 天 的令牌解析通过）。
func (i *Issuer) Parse(tokenString string, now time.Time) (Payload, error) {
	//空串单独一档：它对应「请求没带令牌」，与「带了个畸形串」的排错方向不同。
	//不做 TrimSpace——令牌来自机器拼装的 Authorization 头，出现空白即代表
	//拼装有误或探测行为，应作非法输入暴露（取向同 idgen.ParseString）。
	if tokenString == "" {
		return Payload{}, ErrTokenEmpty
	}

	var claims issuedClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims,
		//密钥回调恒返回同一把密钥。**不看 token.Header 里的任何东西**——
		//「按 header 里的 alg / kid 决定用哪把密钥」正是算法混淆漏洞的根源。
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		//库的哨兵映射到本包哨兵。顺序有讲究：过期先判，因为它是唯一的正常事件，
		//且实测「同时过期 + 未来签发」的令牌会同时命中两个哨兵（errors.Is 对二者
		//均为真），此时应报过期——那是用户看到的真实原因。
		return Payload{}, classify(err)
	}

	//—— 以下是库通过、但本系统口径未必通过的部分 ——

	//iat 必须存在。WithIssuedAt 只校验「不在未来」，缺失时它放行（实测）。
	//没有签发时间的令牌永远无法被基线时间作废，等于一张不可撤销的通行证。
	if claims.IssuedAt == nil {
		return Payload{}, fmt.Errorf("%w: 缺少签发时间(iat)", ErrTokenClaims)
	}
	//exp 必须存在。WithExpirationRequired 已保证，此处是防御性断言：
	//若库将来改变该选项语义，这里会失败而不是放行一个永不过期的令牌。
	if claims.ExpiresAt == nil {
		return Payload{}, fmt.Errorf("%w: 缺少过期时间(exp)", ErrTokenClaims)
	}

	issuedAt := claims.IssuedAt.Time
	expiresAt := claims.ExpiresAt.Time

	//有效期必须恰为 TTL。库不管这条（实测 exp = iat + 365 天 通过），
	//不判则任何持有密钥的一方都能自签长期令牌，A-5 的「12 小时绝对过期」失效。
	//用相等而非「不超过」：本包是唯一签发方，Issue 恒写 iat + TTL，
	//出现别的间距就说明这个令牌不是本版本签的，应当拒绝而非宽容。
	if lifetime := expiresAt.Sub(issuedAt); lifetime != TTL {
		return Payload{}, fmt.Errorf("%w: 有效期为 %s，应为 %s",
			ErrTokenClaims, lifetime, TTL)
	}

	//用户ID 复校。用 ParseString 而非 strconv：它只接受 idgen.String 的规范形态
	//（拒 ""、"0"、"-1"、"007"、" 123"、溢出），与签发侧严格互逆。
	//实测库对这些 sub 一概放行，故这一步不可省。
	userID, ok := idgen.ParseString(claims.Subject)
	if !ok {
		return Payload{}, fmt.Errorf("%w: sub %q", ErrUserIDInvalid, claims.Subject)
	}

	//角色复校。实测库对 role 为 "" 或 "root" 均放行；ParseRole 严格全等匹配，
	//未知码值返回 enum.ErrUnknownCode，本包统一归 ErrRoleInvalid
	//（不把 enum 的哨兵透出去：上层判的是「令牌里的角色不对」，
	//不需要知道它来自 enum 的哪个函数）。
	role, err := enum.ParseRole(claims.Role)
	if err != nil {
		return Payload{}, fmt.Errorf("%w: role %q", ErrRoleInvalid, claims.Role)
	}

	return Payload{
		UserID:    userID,
		Role:      role,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// classify 把库的错误映射到本包哨兵。
//
// 用 errors.Is 判定而非匹配错误文本——文本会随库版本变化，哨兵不会。
// 库的错误链在包装后仍可判定（实测：包装一层后 errors.Is(err, ErrTokenExpired)
// 仍为 true），故这里能安全地逐个判。
//
// 映射后**保留原错误在链上**（%w），排错时仍可从链里取到库的具体错误，
// 但上层只需认本包的九个哨兵。
// 是包级函数而非方法：它只做错误映射，不碰密钥，与 Issuer 的状态无关。
func classify(err error) error {
	switch {
	//过期最先判：它是唯一的正常事件，且与其他哨兵可能同时命中（实测
	//「过期 + 未来签发」两个 Is 均为真），先判它才能给出用户看到的真实原因。
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("%w: %v", ErrTokenExpired, err)

	//必需 claim 缺失（本包开了 WithExpirationRequired，缺 exp 落在此）。
	//归载荷档而非格式档：令牌形态与签名都对，是内容不合本系统口径。
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return fmt.Errorf("%w: %v", ErrTokenClaims, err)

	//未来签发（本包开了 WithIssuedAt）。同样是载荷不合口径。
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued), errors.Is(err, jwt.ErrTokenNotValidYet):
		return fmt.Errorf("%w: %v", ErrTokenClaims, err)

	//格式非法要放在签名之前判：实测畸形输入（段数不对、base64 解不开、
	//JSON 解不开、role 类型不符）报的是 ErrTokenMalformed，而库把部分
	//签名相关错误也包在 ErrTokenSignatureInvalid 下，先判格式能让
	//「压根不是 JWT」与「是 JWT 但签名不对」分开。
	case errors.Is(err, jwt.ErrTokenMalformed):
		return fmt.Errorf("%w: %v", ErrTokenMalformed, err)

	//签名相关：签名被改、载荷被改、换了密钥、算法混淆、alg=none、
	//密钥类型不符（实测这些均命中 ErrTokenSignatureInvalid 或 ErrTokenUnverifiable）。
	case errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrTokenUnverifiable),
		errors.Is(err, jwt.ErrInvalidKeyType),
		errors.Is(err, jwt.ErrInvalidKey):
		return fmt.Errorf("%w: %v", ErrTokenSignature, err)

	//其余 claims 校验失败。
	//
	//这条**必须留在最后**：库的 ErrTokenInvalidClaims 是上面三条
	//（过期 / 必需 claim 缺失 / 未来签发）的**父错误**——实测过期令牌的
	//errors.Is(err, ErrTokenInvalidClaims) 同样为真（库 parser.go:120 把校验
	//错误统一包在它下面）。一旦上移，所有过期令牌都会被归成载荷不符，
	//「登录已过期，请重新登录」这句提示就再也不会出现。
	//由 TestClassifyOrderIsLoadBearing 与 TestExpiryBoundary 两侧钉住。
	case errors.Is(err, jwt.ErrTokenInvalidClaims):
		return fmt.Errorf("%w: %v", ErrTokenClaims, err)
	}

	//兜底归格式非法而非「系统错误」：走到这里说明库给出了一个本包未枚举的
	//失败原因，而所有已知原因都指向「这个令牌有问题」。归到未登录一侧（上层会
	//映射成 KindUnauthorized）比归系统错误安全——后者会让一个畸形令牌返 500，
	//既暴露了内部状态，也让攻击者能用畸形输入刷错误日志。
	return fmt.Errorf("%w: %v", ErrTokenMalformed, err)
}
