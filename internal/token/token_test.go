package token

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/cellargalaxy/jotcash/internal/base/idgen"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// 本文件覆盖本包的运行期行为。用例按「承载条款 → 实测坑」两条线组织：
// 前半验证承载写明的四项载荷与 12h 绝对过期，后半逐条钉住包注释里记录的
// 每一个库默认行为陷阱——那些陷阱是本包存在的理由，回归时必须失败。

// 测试用密钥。长度刻意取 MinSecretLength 以上，且两把互不相同，
// 用于「换密钥即全体失效」的用例。
const (
	testSecret  = "test-secret-0123456789abcdefghijklmn"
	otherSecret = "other-secret-0123456789abcdefghijklmn"
)

// 固定基准时刻。所有时间断言以它为原点，不用 time.Now()——
// 有效期边界只差 1 秒，靠真实时间无法确定性验证。
var baseTime = time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

// 一个合法的用户ID，取真实 ID 的量级（18 位，超过 float64 安全整数上界）。
const testUserID int64 = 260906160443337625

func newIssuer(t *testing.T) *Issuer {
	t.Helper()
	issuer, err := New(testSecret)
	if err != nil {
		t.Fatalf("构造 Issuer 失败：%v", err)
	}
	return issuer
}

// issueAt 签发一个默认载荷的令牌，返回令牌串与实际载荷。
func issueAt(t *testing.T, issuer *Issuer, role enum.Role, now time.Time) (string, Payload) {
	t.Helper()
	tk, got, err := issuer.Issue(Payload{UserID: testUserID, Role: role}, now)
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	return tk, got
}

// ---------- 一、承载：四项载荷往返 ----------

// TestIssueParseRoundTrip 签发再解析，四项载荷逐项相等。
//
// 对应分层 §六 L3 token 行的载荷列「用户ID / 角色 / 签发时间 / 过期时间」。
// 两个角色都测：管理员与普通用户在本包无差别待遇（前提 3 下管理员无业务数据特权，
// 本包只如实携带角色），这条用例同时钉住「本包不对 admin 做任何特殊处理」。
func TestIssueParseRoundTrip(t *testing.T) {
	issuer := newIssuer(t)

	for _, role := range enum.Roles() {
		tk, issued := issueAt(t, issuer, role, baseTime)

		parsed, err := issuer.Parse(tk, baseTime)
		if err != nil {
			t.Fatalf("角色 %s：解析失败 %v", role.Code(), err)
		}
		if parsed.UserID != testUserID {
			t.Errorf("角色 %s：用户ID 往返不等，签发 %d 解析 %d", role.Code(), testUserID, parsed.UserID)
		}
		if parsed.Role != role {
			t.Errorf("角色 %s：角色往返不等，解析得 %q", role.Code(), parsed.Role.Code())
		}
		if !parsed.IssuedAt.Equal(issued.IssuedAt) {
			t.Errorf("角色 %s：签发时间往返不等，签发 %v 解析 %v", role.Code(), issued.IssuedAt, parsed.IssuedAt)
		}
		if !parsed.ExpiresAt.Equal(issued.ExpiresAt) {
			t.Errorf("角色 %s：过期时间往返不等，签发 %v 解析 %v", role.Code(), issued.ExpiresAt, parsed.ExpiresAt)
		}
	}
}

// TestIssuedPayloadMatchesToken 断言 Issue 的第二个返回值与令牌内的值**逐字节一致**。
//
// 这是本包对 service/auth 的核心契约：基线时间比对拿的就是这个时间，
// 差半秒就会误杀刚签发的令牌（实测坑见包注释「秒级截断」）。
// 用带亚秒的 now 才能暴露问题——整秒输入下截断是恒等的，测不出任何东西。
func TestIssuedPayloadMatchesToken(t *testing.T) {
	issuer := newIssuer(t)

	//刻意带上亚秒：987654321 纳秒。
	now := baseTime.Add(987654321 * time.Nanosecond)
	tk, issued, err := issuer.Issue(Payload{UserID: testUserID, Role: enum.RoleAdmin}, now)
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}

	//返回的时间必须是整秒。
	if issued.IssuedAt.Nanosecond() != 0 {
		t.Errorf("返回的签发时间应为整秒，实际 %v（纳秒 %d）", issued.IssuedAt, issued.IssuedAt.Nanosecond())
	}
	if issued.ExpiresAt.Nanosecond() != 0 {
		t.Errorf("返回的过期时间应为整秒，实际 %v", issued.ExpiresAt)
	}

	//且必须与令牌内的值相等——从令牌里独立解出来比，不信任 Issue 的自述。
	parsed, err := issuer.Parse(tk, now)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !parsed.IssuedAt.Equal(issued.IssuedAt) {
		t.Errorf("Issue 返回的签发时间 %v 与令牌内的 %v 不一致——"+
			"基线时间比对会因此误判（见包注释「秒级截断」）", issued.IssuedAt, parsed.IssuedAt)
	}
	if !parsed.ExpiresAt.Equal(issued.ExpiresAt) {
		t.Errorf("Issue 返回的过期时间 %v 与令牌内的 %v 不一致", issued.ExpiresAt, parsed.ExpiresAt)
	}
}

// TestExpiresAtEqualsIssuedAtPlusTTL 断言恒等式 ExpiresAt == IssuedAt + TTL，
// 且 TTL 恰为 12 小时（终版 A-5）。
//
// 覆盖带亚秒的 now：先截断再加 TTL，保证恒等式精确成立而非碰巧成立。
func TestExpiresAtEqualsIssuedAtPlusTTL(t *testing.T) {
	if TTL != 12*time.Hour {
		t.Fatalf("TTL 应为 12 小时（终版 A-5），实际 %v", TTL)
	}

	issuer := newIssuer(t)
	for _, offset := range []time.Duration{0, time.Nanosecond, 500 * time.Millisecond, 999999999 * time.Nanosecond} {
		_, issued, err := issuer.Issue(Payload{UserID: testUserID, Role: enum.RoleUser}, baseTime.Add(offset))
		if err != nil {
			t.Fatalf("偏移 %v：签发失败 %v", offset, err)
		}
		if got := issued.ExpiresAt.Sub(issued.IssuedAt); got != TTL {
			t.Errorf("偏移 %v：过期时间 - 签发时间 = %v，应恒为 %v", offset, got, TTL)
		}
	}
}

// TestIssuerSignatures 钉住两个方法的签名形状。
//
// 接口断言只能证明「这两个方法存在且签名没变」，**不能**证明「没有第三个方法」——
// 接口满足性是结构性的，多出方法照样满足。因此「不得新增续期入口」那一条由
// consumer_check_test.go 的 TestExportedSurface（导出面全等比对）与
// TestNoCarriageOverreach（标识名走查 Refresh/Renew）负责，本用例只兜签名这一层：
// 若有人给 Issue 加个参数或改返回值，下游 service/auth 与 middleware 的调用
// 会一起崩，此处先失败能更早暴露。
func TestIssuerSignatures(t *testing.T) {
	var _ interface {
		Issue(Payload, time.Time) (string, Payload, error)
		Parse(string, time.Time) (Payload, error)
	} = (*Issuer)(nil)
}

// ---------- 二、12h 绝对过期的边界 ----------

// TestExpiryBoundary 逐点验证有效期是**左闭右开** [IssuedAt, IssuedAt+TTL)。
//
// 边界只差 1 秒（JWT 时间精度为秒），故必须用注入的 now 而非 sleep。
// 实测库的判据是「exp <= now 即过期」，本用例把它钉死：
// 签发瞬间通过、11h59m59s 通过、12h 整过期、12h+1s 过期。
func TestExpiryBoundary(t *testing.T) {
	issuer := newIssuer(t)
	tk, _ := issueAt(t, issuer, enum.RoleUser, baseTime)

	cases := []struct {
		name    string
		elapsed time.Duration
		expired bool
	}{
		{"签发瞬间", 0, false},
		{"1 秒后", time.Second, false},
		{"11h59m58s", 12*time.Hour - 2*time.Second, false},
		{"11h59m59s（最后一个有效秒）", 12*time.Hour - time.Second, false},
		{"12h 整（右开边界）", 12 * time.Hour, true},
		{"12h1s", 12*time.Hour + time.Second, true},
		{"一年后", 365 * 24 * time.Hour, true},
	}

	for _, c := range cases {
		_, err := issuer.Parse(tk, baseTime.Add(c.elapsed))
		switch {
		case c.expired && !errors.Is(err, ErrTokenExpired):
			t.Errorf("%s：应报 ErrTokenExpired，实际 %v", c.name, err)
		case !c.expired && err != nil:
			t.Errorf("%s：应通过，实际 %v", c.name, err)
		}
	}
}

// TestParseBeforeIssuedAt 断言签发时间在未来的令牌被拒。
//
// 库**默认不校验 iat**（实测 iat 在未来 1 小时的令牌 err 为 nil），
// 本包靠 WithIssuedAt 拦下。未来签发时间会让基线时间比对失去意义——
// 一个 iat 永远大于基线的令牌无法被作废。
func TestParseBeforeIssuedAt(t *testing.T) {
	issuer := newIssuer(t)
	//在 baseTime+1h 签发，却在 baseTime 校验。
	tk, _ := issueAt(t, issuer, enum.RoleUser, baseTime.Add(time.Hour))

	_, err := issuer.Parse(tk, baseTime)
	if !errors.Is(err, ErrTokenClaims) {
		t.Errorf("未来签发的令牌应报 ErrTokenClaims，实际 %v", err)
	}
}

// ---------- 三、密钥校验（实测：库对密钥毫无门槛）----------

// TestNewRejectsWeakSecret 断言弱密钥在**构造时**即被拒。
//
// 实测库对密钥长度毫无门槛：1/8/16/31/32/64 字节全部正常工作，
// 连空串与 nil 都能签出可验证的令牌。这道门槛只有本包能设。
func TestNewRejectsWeakSecret(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		want   error
	}{
		{"空串", "", ErrSecretEmpty},
		{"全空格", "        ", ErrSecretEmpty},
		{"制表与换行", "\t\n\r ", ErrSecretEmpty},
		{"1 字节", "k", ErrSecretTooShort},
		{"31 字节（差一个）", strings.Repeat("k", MinSecretLength-1), ErrSecretTooShort},
	}
	for _, c := range cases {
		issuer, err := New(c.secret)
		if !errors.Is(err, c.want) {
			t.Errorf("%s：应报 %v，实际 %v", c.name, c.want, err)
		}
		if issuer != nil {
			t.Errorf("%s：构造失败时必须返回 nil Issuer，否则调用方可能拿它签发", c.name)
		}
	}

	//恰好达到下限应通过。
	if _, err := New(strings.Repeat("k", MinSecretLength)); err != nil {
		t.Errorf("%d 字节密钥应通过，实际 %v", MinSecretLength, err)
	}
}

// TestNewSecretErrorHidesSecret 断言密钥校验的错误里**不含密钥内容**。
//
// 错误会流向日志（base/errs 包注释：错误同时流向日志与前端），
// 把密钥写进去就是持久化泄露。错误只应含长度。
func TestNewSecretErrorHidesSecret(t *testing.T) {
	const secret = "short-but-recognizable-secret!!"
	if len(secret) >= MinSecretLength {
		t.Fatalf("用例前提不成立：该密钥应短于 %d 字节，实际 %d", MinSecretLength, len(secret))
	}
	_, err := New(secret)
	if err == nil {
		t.Fatal("用例前提不成立：短密钥应报错")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("错误信息泄露了密钥原文：%v", err)
	}
}

// TestNewPreservesSecretWhitespace 断言密钥**原样使用**，不做 TrimSpace。
//
// 与 base/config「原样保留」密钥同口径。悄悄裁掉首尾空白会让
// 「配置里多敲一个空格」变成「换了一把密钥」，而症状是全部令牌失效却查不出原因。
// 判据：带空白的密钥与裁掉空白的密钥必须**互不通用**。
func TestNewPreservesSecretWhitespace(t *testing.T) {
	padded := "  " + testSecret + "  "
	withSpace, err := New(padded)
	if err != nil {
		t.Fatalf("带空白的密钥应可构造：%v", err)
	}
	trimmed := newIssuer(t)

	tk, _ := issueAt(t, withSpace, enum.RoleUser, baseTime)
	if _, err := trimmed.Parse(tk, baseTime); !errors.Is(err, ErrTokenSignature) {
		t.Errorf("若密钥被 TrimSpace，两把密钥会等价。应报签名失败，实际 %v", err)
	}
}

// ---------- 四、业务码值校验（实测：库一概不管）----------

// TestIssueRejectsInvalidUserID 断言非法用户ID 在签发侧即被拒。
//
// 实测库对 sub 为 ""、"0"、"-1"、"abc"、"007" 一概放行。
// 签发侧不拦的后果最严重：非法载荷被**签上了合法签名**，此后无人能判定它非法。
func TestIssueRejectsInvalidUserID(t *testing.T) {
	issuer := newIssuer(t)
	for _, id := range []int64{0, -1, -260906160443337625} {
		tk, payload, err := issuer.Issue(Payload{UserID: id, Role: enum.RoleUser}, baseTime)
		if !errors.Is(err, ErrUserIDInvalid) {
			t.Errorf("用户ID %d：应报 ErrUserIDInvalid，实际 %v", id, err)
		}
		if tk != "" {
			t.Errorf("用户ID %d：失败时不得返回令牌串", id)
		}
		if !payload.IsZero() {
			t.Errorf("用户ID %d：失败时必须返回零值载荷", id)
		}
	}
}

// TestIssueRejectsInvalidRole 断言非法角色在签发侧被拒。
//
// 重点是 enum.Role("root") 这类**强转**：enum.Role 底层是 string，
// 包外强转编译通过（见 enum 包注释），只判空拦不住，必须用 Valid()。
func TestIssueRejectsInvalidRole(t *testing.T) {
	issuer := newIssuer(t)
	for _, role := range []enum.Role{"", "root", "ADMIN", " admin", "administrator"} {
		_, payload, err := issuer.Issue(Payload{UserID: testUserID, Role: role}, baseTime)
		if !errors.Is(err, ErrRoleInvalid) {
			t.Errorf("角色 %q：应报 ErrRoleInvalid，实际 %v", string(role), err)
		}
		if !payload.IsZero() {
			t.Errorf("角色 %q：失败时必须返回零值载荷", string(role))
		}
	}
}

// TestParseRejectsInvalidClaimValues 断言**签名有效但载荷非法**的令牌被拒。
//
// 这是本包最要紧的一类用例：用同一把密钥自签各种非法载荷，模拟
// 「拿到密钥的一方」或「改错了签发逻辑的将来版本」。实测库对以下全部放行。
func TestParseRejectsInvalidClaimValues(t *testing.T) {
	issuer := newIssuer(t)

	cases := []struct {
		name string
		sub  string
		role string
		iat  *jwt.NumericDate
		exp  *jwt.NumericDate
		want error
	}{
		{"sub 为空", "", enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrUserIDInvalid},
		{"sub 为 0", "0", enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrUserIDInvalid},
		{"sub 为负", "-1", enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrUserIDInvalid},
		{"sub 非数字", "abc", enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrUserIDInvalid},
		{"sub 带前导零（非规范形态）", "007", enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrUserIDInvalid},
		{"role 为空", idgen.String(testUserID), "",
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrRoleInvalid},
		{"role 未知码值", idgen.String(testUserID), "root",
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrRoleInvalid},
		{"role 大小写不符", idgen.String(testUserID), "Admin",
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(TTL)), ErrRoleInvalid},
		{"缺 iat（无法比对基线时间）", idgen.String(testUserID), enum.RoleAdmin.Code(),
			nil, jwt.NewNumericDate(baseTime.Add(TTL)), ErrTokenClaims},
		{"缺 exp（永不过期）", idgen.String(testUserID), enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), nil, ErrTokenClaims},
		{"有效期 365 天（绕过 A-5）", idgen.String(testUserID), enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(365 * 24 * time.Hour)), ErrTokenClaims},
		{"有效期 13 小时（多一小时）", idgen.String(testUserID), enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(13 * time.Hour)), ErrTokenClaims},
		{"有效期 1 秒（少于 TTL）", idgen.String(testUserID), enum.RoleAdmin.Code(),
			jwt.NewNumericDate(baseTime), jwt.NewNumericDate(baseTime.Add(time.Second)), ErrTokenClaims},
	}

	for _, c := range cases {
		//用同一把密钥自签，故签名必然有效——被拒的只能是载荷。
		forged := signRaw(t, testSecret, issuedClaims{
			Role: c.role,
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: c.sub, IssuedAt: c.iat, ExpiresAt: c.exp,
			},
		})
		payload, err := issuer.Parse(forged, baseTime)
		if !errors.Is(err, c.want) {
			t.Errorf("%s：应报 %v，实际 %v", c.name, c.want, err)
		}
		if !payload.IsZero() {
			t.Errorf("%s：失败时必须返回零值载荷，实际 %s", c.name, payload)
		}
	}
}

// ---------- 五、签名与算法混淆 ----------

// TestParseRejectsTamperedToken 断言篡改令牌被拒，且**不返回任何载荷**。
//
// 载荷篡改那一条是本包 Parse「失败即零值」设计的直接依据：实测库在签名校验
// 失败时**已经把载荷填进了传入的 claims**，把 sub 改成别人后能读出被改的值。
// 若本包连同错误一起返回载荷，调用方一个疏忽就会采信攻击者的用户ID。
func TestParseRejectsTamperedToken(t *testing.T) {
	issuer := newIssuer(t)
	tk, _ := issueAt(t, issuer, enum.RoleUser, baseTime)
	segs := strings.Split(tk, ".")

	//① 篡改签名段。
	badSig := tk[:len(tk)-4] + "AAAA"

	//② 篡改载荷：把 sub 换成另一个用户，role 提权成 admin。
	raw, err := base64.RawURLEncoding.DecodeString(segs[1])
	if err != nil {
		t.Fatalf("解码载荷失败：%v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("反序列化载荷失败：%v", err)
	}
	claims["sub"] = "260906160443337626"
	claims["role"] = enum.RoleAdmin.Code()
	altered, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("序列化载荷失败：%v", err)
	}
	badPayload := segs[0] + "." + base64.RawURLEncoding.EncodeToString(altered) + "." + segs[2]

	for _, c := range []struct{ name, token string }{
		{"签名段被改", badSig},
		{"载荷被改（提权为 admin）", badPayload},
	} {
		payload, err := issuer.Parse(c.token, baseTime)
		if !errors.Is(err, ErrTokenSignature) {
			t.Errorf("%s：应报 ErrTokenSignature，实际 %v", c.name, err)
		}
		if !payload.IsZero() {
			t.Errorf("%s：失败时必须返回零值载荷，实际 %s——"+
				"库在签名失败时已填充 claims，此处泄露即等于采信攻击者数据", c.name, payload)
		}
	}
}

// TestParseRejectsOtherSecret 断言换密钥后存量令牌全部失效。
//
// 与 base/config 对 TokenSecret 的注释「修改此值会使所有已签发的令牌立即失效」
// 互为印证。
func TestParseRejectsOtherSecret(t *testing.T) {
	mine := newIssuer(t)
	theirs, err := New(otherSecret)
	if err != nil {
		t.Fatalf("构造另一个 Issuer 失败：%v", err)
	}

	tk, _ := issueAt(t, theirs, enum.RoleAdmin, baseTime)
	if _, err := mine.Parse(tk, baseTime); !errors.Is(err, ErrTokenSignature) {
		t.Errorf("另一把密钥签的令牌应报 ErrTokenSignature，实际 %v", err)
	}
}

// TestParseRejectsAlgorithmConfusion 断言算法混淆的四种形态全部被拒。
//
// 这是 JWT 最经典的漏洞族，本包靠 WithValidMethods 白名单收口。
func TestParseRejectsAlgorithmConfusion(t *testing.T) {
	issuer := newIssuer(t)
	valid, _ := issueAt(t, issuer, enum.RoleUser, baseTime)
	segs := strings.Split(valid, ".")

	claims := issuedClaims{
		Role: enum.RoleAdmin.Code(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   idgen.String(testUserID),
			IssuedAt:  jwt.NewNumericDate(baseTime),
			ExpiresAt: jwt.NewNumericDate(baseTime.Add(TTL)),
		},
	}

	//① alg=none 且无签名段。
	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("构造 alg=none 令牌失败：%v", err)
	}

	//② 把合法令牌的 header 改成 none，保留原签名段。
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	strippedAlg := hdr + "." + segs[1] + "."

	//③ HS512 签发（同为 HMAC，密钥类型相同，只有 alg 不同）——
	//   这一条专门验证白名单是按 alg 精确匹配，而不是「只要是 HMAC 就放行」。
	hs512, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("构造 HS512 令牌失败：%v", err)
	}

	//④ RS256 签发（非对称）。
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败：%v", err)
	}
	rs256, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(rsaKey)
	if err != nil {
		t.Fatalf("构造 RS256 令牌失败：%v", err)
	}

	for _, c := range []struct{ name, token string }{
		{"alg=none 无签名", noneToken},
		{"header 改成 none 保留签名", strippedAlg},
		{"HS512（同族但非白名单）", hs512},
		{"RS256（非对称）", rs256},
	} {
		payload, err := issuer.Parse(c.token, baseTime)
		if !errors.Is(err, ErrTokenSignature) {
			t.Errorf("%s：应报 ErrTokenSignature，实际 %v", c.name, err)
		}
		if !payload.IsZero() {
			t.Errorf("%s：失败时必须返回零值载荷", c.name)
		}
	}
}

// ---------- 六、畸形输入 ----------

// TestParseRejectsMalformed 断言各类畸形输入被拒且不 panic。
//
// 特别包含 "Bearer <token>"：那是 HTTP 头的形态，剥前缀归 middleware，
// 本包只收裸令牌；若哪天有人把整个头传进来，应当明确失败而不是玄学通过。
// 也包含 100 万个句点——v5 实测 1µs 完成（v3 时代此处有分配放大问题）。
func TestParseRejectsMalformed(t *testing.T) {
	issuer := newIssuer(t)
	valid, _ := issueAt(t, issuer, enum.RoleUser, baseTime)

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"空串", "", ErrTokenEmpty},
		{"单个空格", " ", ErrTokenMalformed},
		{"一个句点", ".", ErrTokenMalformed},
		{"两个句点", "..", ErrTokenMalformed},
		{"三段非 base64", "a.b.c", ErrTokenMalformed},
		{"一百万个句点", strings.Repeat(".", 1000000), ErrTokenMalformed},
		{"带 Bearer 前缀", "Bearer " + valid, ErrTokenMalformed},
		{"多一段", valid + ".x", ErrTokenMalformed},
		{"少一段", strings.Join(strings.Split(valid, ".")[:2], "."), ErrTokenMalformed},
		{"载荷段带 base64 填充", func() string {
			s := strings.Split(valid, ".")
			return s[0] + "." + s[1] + "=." + s[2]
		}(), ErrTokenMalformed},
		{"首尾有空白的合法令牌", " " + valid + " ", ErrTokenMalformed},
	}

	for _, c := range cases {
		payload, err := issuer.Parse(c.token, baseTime)
		if !errors.Is(err, c.want) {
			t.Errorf("%s：应报 %v，实际 %v", c.name, c.want, err)
		}
		if !payload.IsZero() {
			t.Errorf("%s：失败时必须返回零值载荷", c.name)
		}
	}
}

// TestParseRejectsWrongClaimTypes 断言载荷字段类型不符时失败而非静默取零值。
//
// 这是选结构体 claims 而非 jwt.MapClaims 的理由：MapClaims 取值要逐个类型断言，
// 断言失败时静默得到零值；结构体让 json 解码在类型不符时直接失败。
func TestParseRejectsWrongClaimTypes(t *testing.T) {
	issuer := newIssuer(t)

	for _, c := range []struct {
		name   string
		claims jwt.MapClaims
	}{
		{"role 是数字", jwt.MapClaims{
			"sub": idgen.String(testUserID), "role": 123,
			"iat": baseTime.Unix(), "exp": baseTime.Add(TTL).Unix()}},
		{"sub 是数字（前端精度陷阱）", jwt.MapClaims{
			"sub": testUserID, "role": enum.RoleAdmin.Code(),
			"iat": baseTime.Unix(), "exp": baseTime.Add(TTL).Unix()}},
		{"role 是数组", jwt.MapClaims{
			"sub": idgen.String(testUserID), "role": []string{"admin"},
			"iat": baseTime.Unix(), "exp": baseTime.Add(TTL).Unix()}},
	} {
		tk, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c.claims).SignedString([]byte(testSecret))
		if err != nil {
			t.Fatalf("%s：构造令牌失败 %v", c.name, err)
		}
		payload, err := issuer.Parse(tk, baseTime)
		if err == nil {
			t.Errorf("%s：应失败，实际通过并得到 %s", c.name, payload)
		}
		if !payload.IsZero() {
			t.Errorf("%s：失败时必须返回零值载荷", c.name)
		}
	}
}

// ---------- 七、载荷不得泄露密钥材料 ----------

// TestTokenPayloadIsPlaintextAndCarriesOnlyFourFields 断言载荷恰含四个字段。
//
// 前置事实（实测）：载荷是明文 base64，无需密钥即可读出。JWT 只保证不可篡改、
// 不保证不可读。因此本用例同时是一条红线检查——载荷里多出任何字段都要在此失败，
// 尤其是密码哈希、盐、基线时间（分层 §九 红线只给密码值留了三个出口，令牌不在其中）。
func TestTokenPayloadIsPlaintextAndCarriesOnlyFourFields(t *testing.T) {
	issuer := newIssuer(t)
	tk, _ := issueAt(t, issuer, enum.RoleAdmin, baseTime)

	//无密钥直接解出载荷——证明它是明文。
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(tk, ".")[1])
	if err != nil {
		t.Fatalf("解码载荷失败：%v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化载荷失败：%v", err)
	}

	want := map[string]bool{"sub": true, "role": true, "iat": true, "exp": true}
	for key := range got {
		if !want[key] {
			t.Errorf("载荷出现了承载之外的字段 %q——载荷是明文可读的，"+
				"分层 §九 红线不允许密码值、基线时间等进入令牌", key)
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("载荷缺少字段 %q", key)
		}
	}

	//sub 必须是字符串：int64 ID 经 float64 往返会变值（idgen.String 的注释）。
	if _, ok := got["sub"].(string); !ok {
		t.Errorf("sub 必须是字符串（18 位 ID 超过 float64 安全整数上界），实际 %T", got["sub"])
	}
}

// TestPayloadStringExcludesToken 断言 Payload.String 不含令牌串。
//
// 令牌等价于口令：打进日志即等于把登录态写入持久介质。
func TestPayloadStringExcludesToken(t *testing.T) {
	issuer := newIssuer(t)
	tk, issued := issueAt(t, issuer, enum.RoleAdmin, baseTime)

	text := issued.String()
	if strings.Contains(text, tk) {
		t.Errorf("Payload.String 泄露了令牌串：%s", text)
	}
	//令牌的签名段单独查一遍：即便不含完整串，含签名段也足以被重放。
	if sig := strings.Split(tk, ".")[2]; strings.Contains(text, sig) {
		t.Errorf("Payload.String 泄露了令牌签名段：%s", text)
	}
	if strings.Contains(text, testSecret) {
		t.Errorf("Payload.String 泄露了密钥：%s", text)
	}
}

// ---------- 八、并发与确定性 ----------

// TestIssuerConcurrentUse 断言构造后的 Issuer 可并发使用。
//
// A-6 每请求调 Parse，这条是必要前提。用 -race 跑才有意义（CI 应带 -race）。
func TestIssuerConcurrentUse(t *testing.T) {
	issuer := newIssuer(t)
	tk, _ := issueAt(t, issuer, enum.RoleUser, baseTime)

	const n = 64
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			if i%2 == 0 {
				_, err := issuer.Parse(tk, baseTime)
				done <- err
				return
			}
			_, _, err := issuer.Issue(Payload{UserID: testUserID, Role: enum.RoleAdmin}, baseTime)
			done <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Errorf("并发调用出错：%v", err)
		}
	}
}

// TestIssueIsDeterministic 记录并钉住「令牌不是会话标识」这一事实。
//
// 实测同一 (用户, 秒, 密钥) 两次签发得到**完全相同**的令牌（HMAC 确定性，
// 载荷无 jti）。这不是缺陷，而是终版 §五 3「单端登出需令牌黑名单，本轮不建」的
// 直接体现——令牌无法标识单个会话。将来若要支持单端登出，此用例会失败，
// 提示实现者「加了 jti 就必须同时建黑名单」。
func TestIssueIsDeterministic(t *testing.T) {
	issuer := newIssuer(t)
	first, _ := issueAt(t, issuer, enum.RoleAdmin, baseTime)
	second, _ := issueAt(t, issuer, enum.RoleAdmin, baseTime)
	if first != second {
		t.Errorf("同一用户同一秒两次签发应得到相同令牌（无 jti，令牌不标识会话）；" +
			"若已刻意引入 jti，请同时确认令牌黑名单的设计（终版 §五 3 本轮不建）")
	}
}

// ---------- 九、错误分档 ----------

// TestClassifyOrderIsLoadBearing 钉住 classify 的 switch **顺序**。
//
// 前置事实（实测 + 库源码 parser.go:120）：`ErrTokenInvalidClaims` 是
// 「过期 / 必需 claim 缺失 / 未来签发」三者的**父错误**——过期令牌的
// errors.Is(err, ErrTokenInvalidClaims) 同样为真。因此 classify 里那条
// InvalidClaims 分支必须留在最后：一旦有人把它上移，所有过期令牌都会被归成
// ErrTokenClaims，「登录已过期，请重新登录」这句用户提示就再也不会出现，
// 而测试若只断言「解析失败」是发现不了的。
//
// 本用例直接喂库的原始错误给 classify，绕开构造令牌那一层，
// 这样顺序被改动时失败点会精确落在这里。
func TestClassifyOrderIsLoadBearing(t *testing.T) {
	//模拟库的错误包装形态：父错误 + 具体原因（与实测的 err 文本结构一致）。
	cases := []struct {
		name string
		err  error
		want error
	}{
		{
			//过期：同时命中 ErrTokenExpired 与 ErrTokenInvalidClaims，
			//必须归过期档——它是唯一的正常事件，也是唯一需要区分对待的档。
			name: "过期（同时命中 InvalidClaims 父错误）",
			err:  errors.Join(jwt.ErrTokenInvalidClaims, jwt.ErrTokenExpired),
			want: ErrTokenExpired,
		},
		{
			name: "缺必需 claim（同时命中 InvalidClaims 父错误）",
			err:  errors.Join(jwt.ErrTokenInvalidClaims, jwt.ErrTokenRequiredClaimMissing),
			want: ErrTokenClaims,
		},
		{
			name: "未来签发（同时命中 InvalidClaims 父错误）",
			err:  errors.Join(jwt.ErrTokenInvalidClaims, jwt.ErrTokenUsedBeforeIssued),
			want: ErrTokenClaims,
		},
		{
			name: "仅 InvalidClaims（无更具体的原因）",
			err:  jwt.ErrTokenInvalidClaims,
			want: ErrTokenClaims,
		},
		{
			name: "同时过期与未来签发（实测两个 Is 均为真）",
			err:  errors.Join(jwt.ErrTokenExpired, jwt.ErrTokenUsedBeforeIssued),
			want: ErrTokenExpired,
		},
		{
			name: "签名非法",
			err:  jwt.ErrTokenSignatureInvalid,
			want: ErrTokenSignature,
		},
		{
			name: "无法验签",
			err:  jwt.ErrTokenUnverifiable,
			want: ErrTokenSignature,
		},
		{
			name: "密钥类型不符",
			err:  jwt.ErrInvalidKeyType,
			want: ErrTokenSignature,
		},
		{
			name: "格式非法",
			err:  jwt.ErrTokenMalformed,
			want: ErrTokenMalformed,
		},
	}

	for _, c := range cases {
		if got := classify(c.err); !errors.Is(got, c.want) {
			t.Errorf("%s：应归到 %v，实际 %v", c.name, c.want, got)
		}
	}
}

// TestClassifyFallback 断言未枚举的库错误兜底归**格式非法**而非系统错误。
//
// 走到兜底说明库给出了一个本包未枚举的失败原因。归到未登录一侧（上层映射为
// KindUnauthorized）而非系统错误档，是因为后者会让一个畸形令牌返 500——
// 既暴露内部状态，也让攻击者能用畸形输入刷错误日志。
func TestClassifyFallback(t *testing.T) {
	unknown := errors.New("将来某个库版本新增的失败原因")
	got := classify(unknown)
	if !errors.Is(got, ErrTokenMalformed) {
		t.Errorf("未枚举的错误应兜底归 ErrTokenMalformed，实际 %v", got)
	}
	//原始错误应保留在文本里，否则排错时无从下手。
	if !strings.Contains(got.Error(), unknown.Error()) {
		t.Errorf("兜底应保留原始错误信息以便排错，实际 %v", got)
	}
	//但不得把未知错误升成系统错误档。
	for _, sentinel := range []error{ErrTokenExpired, ErrTokenSignature, ErrTokenClaims} {
		if errors.Is(got, sentinel) {
			t.Errorf("兜底不应命中 %v", sentinel)
		}
	}
}

// TestSentinelsAreDistinct 断言九个哨兵互不相等，且不会彼此命中。
//
// 哨兵之间若有包装关系，调用方按档分流就会串档——例如 middleware 若把
// ErrTokenExpired 与 ErrSecretTooShort 混在一起，启动期配置错误会被当成
// 用户未登录，返回 401 而不是让进程启动失败。
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrSecretEmpty":    ErrSecretEmpty,
		"ErrSecretTooShort": ErrSecretTooShort,
		"ErrUserIDInvalid":  ErrUserIDInvalid,
		"ErrRoleInvalid":    ErrRoleInvalid,
		"ErrTokenEmpty":     ErrTokenEmpty,
		"ErrTokenMalformed": ErrTokenMalformed,
		"ErrTokenSignature": ErrTokenSignature,
		"ErrTokenExpired":   ErrTokenExpired,
		"ErrTokenClaims":    ErrTokenClaims,
	}
	for nameA, a := range sentinels {
		if a == nil {
			t.Errorf("%s 为 nil", nameA)
			continue
		}
		if a.Error() == "" {
			t.Errorf("%s 的错误信息为空", nameA)
		}
		for nameB, b := range sentinels {
			if nameA == nameB {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("哨兵 %s 命中了 %s——档位会串，调用方无法按档分流", nameA, nameB)
			}
		}
	}
}

// signRaw 用给定密钥直接签出一个自定义 claims 的令牌，绕过 Issue 的入参校验。
//
// 它模拟的是「持有密钥的一方自签非法载荷」——这正是 Parse 侧复校存在的理由。
func signRaw(t *testing.T, secret string, claims issuedClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("自签令牌失败：%v", err)
	}
	return signed
}
