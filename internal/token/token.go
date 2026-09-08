// Package token 承载 jotcash 的无状态登录令牌（L3 边界层）。
//
// # 承载与边界
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L3 表
// token 行，本包恰承载一件事：
//
//	无状态令牌签发与解析（载荷：用户ID / 角色 / 签发时间 / 过期时间；
//	12h 绝对过期不可续期，时长写死在本包——终版 A-5 是业务规则；
//	仅密钥由 app 注入）；基线时间比对不在此包（需读库，属 service/auth）
//
// 依赖列为 enum。本包属**边界包**而非端口包：分层 §六 L3 表头注写明
// 「边界包（token/i18n）实现无选型分歧，内联包内，**无 L4 子包**」，
// 故实现直接落在本包，不像 store / fxrate / parser 那样另有 L4 实现子包。
//
// 本包**不做**的四件事，每条都有出处，不是自行加码：
//
//   - **不比对基线时间**。终版 §四 1 的 User.登录态基线时间「签发时间早于它的
//     令牌失效」需读库，归 service/auth（承载末句明写；entity/user.go 的
//     TokenBaselineTime 注释亦写「比对逻辑在 service/auth，不在 token 包」）。
//     本包只把签发时间**如实交出**，让那一层能比对——交出的形态很关键，
//     见下文「秒级截断」。
//   - **不建令牌黑名单、不支持单端登出**。终版 §五 3 明写「唯一做不到的语义是
//     『单端登出而其他端不受影响』，需令牌黑名单，**本轮不建**」。
//     这也是 A-4 的「登出为前端丢弃令牌，服务端无动作、不记审计」的前提。
//   - **不读配置**。约定 8「配置只在 L7 读」，密钥由 app 注入（承载：仅密钥由
//     app 注入）。本包不 import base/config。
//   - **不打日志**。理由同 fxrate：本包的 Parse 是 A-6 全局登录态校验的判定点，
//     **每个请求都会调一次**，而「令牌过期」是最正常不过的事件。端口层在此打
//     日志等于按请求量刷日志，还会把真正的异常淹掉。日志归上层按语境打。
//
// # 选型：golang-jwt/jwt/v5，以及为什么不复用 go_common 的 EnJwt/DeJwt
//
// 采用 github.com/golang-jwt/jwt/v5（v5.3.1，实测 9217 star、MIT、最近提交
// 2026-07，依赖面实测为**零非标准库依赖**）。它是 dgrijalva/jwt-go 官方交接后的
// 延续仓库，也是候选中唯一在 2026 年仍有提交的。
//
// 仓库统一使用 go_common，且 go.mod 里**已存在** golang-jwt/jwt v3.2.2 作为它的
// indirect 依赖，故「复用优先」下先评估了 util.EnJwt / util.DeJwt，实测结论是
// **不可用**，三条硬伤：
//
//	① 载荷放不下角色。实测 model.Claims 序列化为 {"exp","iat","sub"}，
//	   字段是 Ip/ServerName/LogId/ReqId + 内嵌 StandardClaims，
//	   既无角色字段也无开放扩展位——而承载明写载荷含「角色」。
//	② 错误不可判定。实测过期与签名错分别返回
//	   "JWT解密异常: token is expired by 1h0m0s" 与 "JWT解密异常: signature is invalid"，
//	   它用 errors.Errorf 重造错误，**哨兵链已断**，errors.Is 命中不了任何库哨兵。
//	   service/auth 要区分「过期」（正常，跳登录页）与「伪造」（异常）就只能匹配
//	   错误文本，那不是可以建立判定的基础。
//	③ 底层是 v3.2.2，而 v3 实测**默认放行无 exp 的令牌**（err 为 nil）。
//	   另有独立佐证：govulncheck 报 v3.2.2 命中 GO-2025-3553
//	   （「Excessive memory allocation during header parsing」），且**Fixed in: N/A**
//	   ——v3 分支不再修，修复途径就是升到 v5。这与实测吻合：同样喂 10 万个句点，
//	   v3 耗时 2.1ms，v5 对 100 万个句点仅 1µs。
//
// 另有一条同类理由：DeJwt 失败时会自己打一条 ERRO 日志（实测 codec.go:123），
// 与上文「本包不打日志」直接冲突——日志行为不该由被调库决定。
//
// 选定的 v5 自身**未被任何漏洞告警点名**（本包 govulncheck 的两条告警分别来自
// quic-go v0.59.0 与 jwt v3.2.2，两者都经 go_common 传入，与 v5 无关；
// 详见文件末尾「遗留：v3.2.2 仍在依赖图中」）。
//
// 未选 lestrrat-go/jwx（2423 star，最近提交 2024-08）是因为它是 JWK/JWE/JWS
// 全家桶，本包只要 HS256 签发解析一种能力；未选 PASETO 两家（941 / 494 star）
// 是因为它是对称密钥语义之外的另一套协议，而 base/config 对 TokenSecret 的报错
// 文案已写死「令牌是无状态的，其不可伪造性完全依赖此密钥」并给出
// openssl rand -base64 48（config.go:186-188），换协议要连带改配置形态与文案，
// 超出本轮范围。
//
// # 库的默认行为不安全，四个解析选项必须恒开
//
// 这是本包存在的主要价值——**库的默认值放行了三类不该放行的令牌**，全部实测：
//
//	现象                                          对策
//	无 exp 的令牌解析通过（err 为 nil）             WithExpirationRequired()
//	iat 在未来 1 小时的令牌解析通过                  WithIssuedAt()
//	alg=none 可签出令牌                            WithValidMethods(["HS256"])
//	exp = iat + 365 天 的令牌解析通过               本包显式断言 exp-iat == TTL
//
// 前三条交给库的选项，第四条**库不管**（承载写死 12h，故必须自己判）：不判的
// 后果是任何拿到密钥的一方都能自签一个长期令牌，A-5 的「12 小时绝对过期」就成了
// 一句注释。注意 WithIssuedAt 只校验「iat 不在未来」，**iat 缺失时它照样放行**
// （实测 err 为 nil 且 IssuedAt 为 nil），故本包另有显式判空。
//
// # 秒级截断：一条会毁掉基线时间比对的实测坑
//
// JWT 的 NumericDate 是秒级（库的 TimePrecision 默认 time.Second），实测
// NewNumericDate 会把 10:00:00.987654321 截断成 10:00:00。这件事本身无害，
// 但它与 service/auth 的基线时间比对组合起来会出错，实测形态如下：
//
//	基线时间       10:00:00.950（改密那一刻推进）
//	改密前的令牌   iat = 10:00:00.900  → 落进令牌后是 10:00:00
//	改密后重登的   iat = 10:00:00.990  → 落进令牌后**也是** 10:00:00
//
// 两个令牌的 iat 变得完全相同，于是 iat < 基线 这个朴素判据会把**刚签发的新令牌
// 也判失效**——用户改完密码立刻重新登录，仍被踢回登录页；而若把基线也截断到秒，
// 则反过来**旧令牌被放行**，A-8 的「改密后当前令牌一并失效」当场失效。
// 实测两种判据都错，正确解是把基线**向上取整到秒**（此时新旧令牌皆失效，代价是
// 重登需等到下一秒，最多 1 秒）。
//
// 处置分两处：本包保证交出的时间**与令牌内的值逐字节一致**（Issue 先把 now
// 截断到秒再算 exp，并把实际载荷作为第二个返回值交出），使那一层有正确的比对
// 基准；取整口径本身归 service/auth，本包不实现（承载明写不在此包）。
//
// # 载荷是明文，不得放任何密钥材料
//
// 实测无需密钥即可从令牌读出载荷（base64 解一下就是
// {"role":"user","sub":"...","exp":...,"iat":...}）。JWT 只保证**不可篡改**，
// 不保证**不可读**。因此载荷严格只放承载列的四项，绝不放密码哈希、盐、
// 基线时间——分层 §九 红线行给密码值只留了三个出口，令牌不在其中。
//
// # 错误是哨兵，不在本包分档
//
// 与同层 fxrate、i18n 一致，本包只出哨兵错误，由上层用 errors.Is 判定后映射到
// base/errs 的档位。这与 rule/passwd 直接返回分档错误的做法相反，理由是
// **档位在本包不唯一**：
//
//	Parse 失败    → KindUnauthorized（base/errs kind.go 的该档注释已明写
//	                「无令牌、令牌过期（A-5 的 12 小时绝对过期）或被基线时间作废」）
//	Issue 失败    → KindInternal（签发入参非法属编程错误，用户无从修正）
//
// 同一个包的错误分属两档，就不能在本包定死，否则每个调用方都要先拆开再重新分档。
// 本包因此也**不** import base/errs。
//
// # 依赖边界
//
// 非测试文件只 import：enum（承载依赖列，角色类型）、base/idgen（用户ID 与
// sub 之间的规范文本互转）、标准库、golang-jwt/jwt/v5。
//
// 刻意**不**依赖：base/errs（哨兵，见上）、base/config（约定 8）、base/log 与
// logrus（不打日志）、entity（不读 User，基线比对不在此包）、以及同层的
// store / fxrate / parser / i18n（分层 §六「层内规则：同层包互不依赖」）。
// 由 TestDependencyBoundary 以 AST 扫描双向锁死。
//
// 关于 base/idgen：分层 §六 L3 token 行的依赖列只写了 enum。本包引 base/idgen
// 是因为 sub 需要「ID ↔ 规范文本」的互逆转换，而 idgen.String / ParseString
// 已是全仓库该规范的唯一实现（含「18 位 ID 超出 float64 安全整数」这条理由，
// 见 idgen.go 的 String 注释）；自己再写一份 strconv 转换等于把同一规范实现两遍，
// 且两份迟早不一致。此项已作为待文档追认记入本轮 answer。
//
// # 遗留：v3.2.2 仍在依赖图中
//
// go.mod 里 golang-jwt/jwt v3.2.2+incompatible 仍在（经 go_common 传入）。
// **本仓库代码无任何路径可达它**：全仓库无一处调用 util.EnJwt / util.DeJwt
// （已 grep 确认），本包用的是 v5，且 TestNoLegacyJwtImport 会拦下任何
// 对 v3 的 import。
//
// 但 govulncheck 仍将 GO-2025-3553 报为「affected」，因为它的调用链是
// idgen.init → util.init → jwt.init，即**包初始化**就把 v3 链进了二进制。
// 该链路始于 base/idgen（实测 idgen.go:87 的 util.GenId），凡 import idgen 的包
// 都会带上，与本包无关：实测 store 与 idgen 自身同样报这两条，
// 而不 import idgen 的 fxrate / entity / enum 报 No vulnerabilities。
//
// 彻底消除需 go_common 升级到 v5（不在本轮范围）。同理，另一条 GO-2026-5676
// 来自 quic-go v0.59.0（go.mod 既有 indirect，可升 v0.59.1），亦经 go_common 传入。
package token

import (
	"errors"
	"time"
)

// TTL 是登录令牌的有效期，取自终版 A-5「签发起 12 小时**绝对过期、不可续期**」。
//
// **写死在本包**，不进 base/config——这是分层的两处明写要求：约定 8
// 「业务阈值一律写死在对应包（12h / 5 次 / 10 分钟 / Argon2 参数 / …）」，
// 以及 §六 L0 base/config 行「业务阈值不进配置（令牌 12h 写死在 token）」。
// 理由是它属业务规则而非部署差异：留一个可配置入口，等于留一个能把有效期调成
// 10 年的入口。
//
// 「绝对过期、不可续期」在本包的落地形态是**没有续期函数**：本包只有 Issue 与
// Parse，没有 Refresh / Renew，也没有任何以旧令牌换新令牌的路径。想延长登录态
// 只能重新走 A-4 登录。
//
// 有效期区间是**左闭右开** [IssuedAt, IssuedAt+TTL)，已实测：签发后
// 11h59m59s 通过、12h 整判过期。
const TTL = 12 * time.Hour

// MinSecretLength 是令牌密钥的最小长度（字节）。
//
// 存在理由是库**完全不管密钥强度**：实测 1 / 8 / 16 / 31 / 32 / 64 字节的密钥
// 全部能正常签发与验证，连空串 []byte("") 与 nil 都能签出可验证的令牌
// （err 均为 nil）。密钥弱不弱只有本包能拦。
//
// 取 32 是因为 HS256 的底层是 HMAC-SHA256，其分组与输出均为 32 字节，短于此
// 即无法给出与算法强度相称的熵。它也与 base/config 报错文案建议的生成方式相容
// （config.go:186-188 的 openssl rand -base64 48 产出 64 个 base64 字符）。
//
// 与 base/config 的分工：那边校验「填了没」（TrimSpace 后非空即通过，
// config.go:184-189），本包校验「够不够强」。两处门槛不同是刻意的——config
// 是部署参数的形态校验，本包是安全参数的强度校验。
const MinSecretLength = 32

// 哨兵错误。上层（service/auth）用 errors.Is 判定后，按自身场景映射到
// base/errs 的档位与文案键（为什么不在本包分档，见包注释「错误是哨兵」）。
//
// 分两组：构造与签发侧（前四个，属编程错误或接线错误，正常运行不该出现）、
// 解析侧（后五个，其中过期是正常事件）。
var (
	// ErrSecretEmpty 表示注入的密钥为空（含全空白）。
	//
	// 必须拦：实测空密钥能签出**可验证**的令牌，即任何人都能自签任意身份的令牌，
	// 而 err 全程为 nil。这是最严重的一种静默失效，故 New 直接拒绝构造，
	// 让 app 在启动装配时就失败。
	ErrSecretEmpty = errors.New("token: 令牌密钥为空")

	// ErrSecretTooShort 表示密钥长度不足 MinSecretLength。
	// 同样在 New 阶段拦下，理由见 MinSecretLength。
	ErrSecretTooShort = errors.New("token: 令牌密钥长度不足")

	// ErrUserIDInvalid 表示待签发的用户ID 不是一个合法标识。
	//
	// 判据是 base/idgen.IsValid（恒为正）。拦它是因为库不校验业务码值：
	// 实测 sub 为 ""、"abc"、"0"、"-1"、"007" 时解析全部成功。零值用户ID 一路
	// 签出去，会得到一个「归属用户为 0」的登录态，而不变式 3 的归属校验以
	// 用户ID 为键——那等于开了一个不属于任何人的会话。
	ErrUserIDInvalid = errors.New("token: 用户ID 非法")

	// ErrRoleInvalid 表示待签发或解析出的角色不是 enum 的已知码值。
	//
	// 两侧都判。签发侧拦「零值或强转出来的角色」——enum.Role 底层是 string，
	// enum.Role("root") 在包外强转编译通过（见 enum 包注释），不判就会签出一个
	// 角色为 root 的令牌；解析侧拦「换了个角色码的旧令牌」，此时用 enum.ParseRole
	// 复校，未知码值即拒。
	ErrRoleInvalid = errors.New("token: 角色非法")

	// ErrTokenEmpty 表示待解析的令牌串为空。
	//
	// 单独一档而不并入 ErrTokenMalformed：空串对应「请求没带令牌」（A-6 的未登录），
	// 畸形串对应「带了但不是本系统签的」（伪造或客户端拼装错误）。两者对排错的
	// 指向完全不同，虽然都映射到 KindUnauthorized。
	ErrTokenEmpty = errors.New("token: 令牌为空")

	// ErrTokenMalformed 表示令牌不是合法的 JWT 形态（段数不对、base64 解不开、
	// JSON 解不开、载荷字段类型不符等）。
	//
	// 实测归入本档的输入包括：""、"."、".."、"a.b.c"、100 万个句点、
	// "Bearer <token>"（带前缀）、"<token>.x"（多一段）、以及 role 字段是数字
	// 而非字符串的令牌。特别提一下 "Bearer " 前缀：那是 HTTP 头的形态，剥前缀
	// 归 middleware，本包只收裸令牌。
	ErrTokenMalformed = errors.New("token: 令牌格式非法")

	// ErrTokenSignature 表示签名校验不通过：令牌被篡改，或不是本密钥签发的。
	//
	// 覆盖三类实测情形：签名段被改、载荷被改（如把 sub 改成别人）、
	// 用另一个密钥签发的令牌。改密钥会使全部存量令牌落入本档，
	// 与 base/config 对 TokenSecret 的注释「修改此值会使所有已签发的令牌立即失效」
	// 一致。
	//
	// 也覆盖算法混淆：alg=none 的令牌、以及 alg 与密钥类型不匹配的令牌。
	ErrTokenSignature = errors.New("token: 令牌签名校验失败")

	// ErrTokenExpired 表示令牌已过期，即 A-5 的 12 小时绝对过期已到。
	//
	// 这是九个错误里**唯一一个预期会在正常运行中频繁出现**的：任何用户挂满 12
	// 小时都会走到这里。它对应「跳回登录页」这个正常流程（终版 A-6），不是故障，
	// 故上层不应按异常记录或告警。
	ErrTokenExpired = errors.New("token: 令牌已过期")

	// ErrTokenClaims 表示令牌签名有效、但载荷不符合本系统口径。
	//
	// 这是本档存在的关键理由：**签名有效不等于载荷可信**。拿到密钥的一方（或
	// 未来某个改错了签发逻辑的版本）能签出签名完全正确、但载荷违反 A-5 的令牌。
	// 实测库对这些一律放行：
	//
	//	缺 exp（永不过期）              → 由 WithExpirationRequired 拦，归本档
	//	缺 iat（无签发时间可比基线）      → 库放行，本包显式判空后归本档
	//	exp - iat = 365 天（TTL 被绕过） → 库放行，本包断言 TTL 后归本档
	//	sub 不是合法 ID / role 未知      → 库放行，本包复校后归 ErrUserIDInvalid /
	//	                                  ErrRoleInvalid
	//
	// 缺 iat 尤其要拦死：签发时间是基线时间比对的唯一输入，没有它
	// service/auth 就无法判断该令牌是否已被改密作废——放行等于给了一个
	// 「永远无法被作废」的令牌。
	ErrTokenClaims = errors.New("token: 令牌载荷不符合口径")
)
