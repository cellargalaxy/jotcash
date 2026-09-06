package pwdhash

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/argon2"
)

// 说明：Argon2id 的代价参数为包内常量、不可注入（约束 3：没有任何入口能把安全参数调低），
// 因此用例一律只断言**可观测契约**（往返 / 分流语义 / 串格式 / 长度 / 篡改检出 /
// 参数取值 / 并发安全），不构造自定义参数。
// 每次 Hash / Verify 都要跑一遍 64 MiB 的 Argon2id，故用例取样数刻意保持小量。

// testPlaintext 满足 A-8 策略（长度 ≥ 10、含字母+数字+符号）的样例口令，
// 只作哈希输入，不代表任何真实口令。
const testPlaintext = "Jotcash#2026"

func TestHashVerifyRoundTrip(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	ok, err := Verify(ctx, testPlaintext, hash, salt)
	if err != nil {
		t.Fatalf("校验异常: %+v", err)
	}
	if !ok {
		t.Fatalf("同一明文校验不通过，hash=%s", hash)
	}
}

// TestVerifyWrongPassword 密码错误必须是 (false, nil) 而不是 error：
// 它属「用户输入错误」，A-8 据此累计连续失败次数；返回 error 会让调用方误判为系统错误。
func TestVerifyWrongPassword(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	for _, wrong := range []string{"Jotcash#2027", "jotcash#2026", "Jotcash#202", "Jotcash#2026 "} {
		ok, err := Verify(ctx, wrong, hash, salt)
		if err != nil {
			t.Fatalf("明文 %q 校验返回了 error（应为 (false, nil)）: %+v", wrong, err)
		}
		if ok {
			t.Fatalf("错误明文 %q 竟校验通过", wrong)
		}
	}
}

// TestHashSaltIsRandomPerCall 同一明文两次哈希，盐与哈希都必须不同——
// 否则库里会出现「相同密码相同哈希」，可直接看出哪些账户口令相同。
func TestHashSaltIsRandomPerCall(t *testing.T) {
	ctx := context.Background()
	hash1, salt1, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	hash2, salt2, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	if salt1 == salt2 {
		t.Fatalf("两次生成的盐相同: %s", salt1)
	}
	if hash1 == hash2 {
		t.Fatalf("两次生成的哈希相同: %s", hash1)
	}
	// 交叉校验仍须各自成立（哈希与盐一一绑定）
	for _, pair := range []struct{ hash, salt string }{{hash1, salt1}, {hash2, salt2}} {
		ok, err := Verify(ctx, testPlaintext, pair.hash, pair.salt)
		if err != nil || !ok {
			t.Fatalf("自身配对校验失败: ok=%v err=%+v", ok, err)
		}
	}
}

// TestHashFormat 哈希串必须带版本前缀、载荷长度等于参数表的 keyLen（取舍 2 的格式）。
func TestHashFormat(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	wantPrefix := versionSeparator + VersionArgon2idV1 + versionSeparator
	if !strings.HasPrefix(hash, wantPrefix) {
		t.Fatalf("哈希串缺少版本前缀 %q，实际 %q", wantPrefix, hash)
	}
	params := paramsByVersion[VersionArgon2idV1]
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hash, wantPrefix))
	if err != nil {
		t.Fatalf("哈希载荷不是合法 Base64: %+v", err)
	}
	if len(key) != int(params.keyLen) {
		t.Fatalf("哈希长度 = %d，期望 %d", len(key), params.keyLen)
	}
	// 盐不带版本前缀，且解出恰好 saltLen 字节
	if strings.Contains(salt, versionSeparator) {
		t.Fatalf("盐不应包含版本分隔符: %s", salt)
	}
	saltRaw, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		t.Fatalf("盐不是合法 Base64: %+v", err)
	}
	if len(saltRaw) != params.saltLen {
		t.Fatalf("盐长度 = %d 字节，期望 %d", len(saltRaw), params.saltLen)
	}
}

// TestHashLeaksNoPlaintext 明文不得出现在哈希串或盐中（§九 红线行）。
func TestHashLeaksNoPlaintext(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	if strings.Contains(hash, testPlaintext) {
		t.Fatalf("哈希串中出现明文: %s", hash)
	}
	if strings.Contains(salt, testPlaintext) {
		t.Fatalf("盐中出现明文: %s", salt)
	}
	// 盐也不得内联进哈希串（约束 2：盐独立列存，不用自包含格式）
	if strings.Contains(hash, salt) {
		t.Fatalf("哈希串中内联了盐，违反盐独立列存: hash=%s salt=%s", hash, salt)
	}
}

// TestHashEmptyPlaintext 空明文必须被 Hash 拒绝——否则等于给账户开空密码后门。
func TestHashEmptyPlaintext(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, "")
	if err == nil {
		t.Fatalf("空明文竟生成了哈希: hash=%q salt=%q", hash, salt)
	}
	if hash != "" || salt != "" {
		t.Fatalf("出错时应返回空值，实际 hash=%q salt=%q", hash, salt)
	}
}

// TestVerifyEmptyPlaintext 空明文校验是 (false, nil)：不给攻击者额外信息，也不付 Argon2 开销。
func TestVerifyEmptyPlaintext(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	ok, err := Verify(ctx, "", hash, salt)
	if err != nil {
		t.Fatalf("空明文校验返回了 error（应为 (false, nil)）: %+v", err)
	}
	if ok {
		t.Fatalf("空明文竟校验通过")
	}
}

// TestVerifyTamperedHash 篡改哈希载荷一个字符即须校验失败（且是 (false, nil)，不是 error：
// 载荷仍是合法 Base64、长度也没变，属「算出来不匹配」）。
func TestVerifyTamperedHash(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	// 翻转载荷首字符（A↔B），保持 Base64 合法与长度不变
	prefix := versionSeparator + VersionArgon2idV1 + versionSeparator
	payload := strings.TrimPrefix(hash, prefix)
	flipped := "B" + payload[1:]
	if payload[0] == 'B' {
		flipped = "A" + payload[1:]
	}
	ok, err := Verify(ctx, testPlaintext, prefix+flipped, salt)
	if err != nil {
		t.Fatalf("篡改哈希校验返回了 error: %+v", err)
	}
	if ok {
		t.Fatalf("篡改后的哈希竟校验通过")
	}
}

// TestVerifyMalformedHashReturnsError 哈希串本身有问题必须返回 error，不能是 (false, nil)：
// 否则 service/auth 会计入 A-8 失败次数、5 次锁定 10 分钟，用户被锁死却查不出原因。
func TestVerifyMalformedHashReturnsError(t *testing.T) {
	ctx := context.Background()
	_, salt, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	params := paramsByVersion[VersionArgon2idV1]
	validPayload := base64.StdEncoding.EncodeToString(make([]byte, params.keyLen))
	cases := map[string]string{
		"空串":         "",
		"无版本前缀":      validPayload,
		"缺前导分隔符":     VersionArgon2idV1 + versionSeparator + validPayload,
		"版本未知":       versionSeparator + "argon2id-v2" + versionSeparator + validPayload,
		"版本为空":       versionSeparator + versionSeparator + validPayload,
		"载荷为空":       versionSeparator + VersionArgon2idV1 + versionSeparator,
		"载荷非法Base64": versionSeparator + VersionArgon2idV1 + versionSeparator + "!!!not-base64!!!",
		"哈希长度不符":     versionSeparator + VersionArgon2idV1 + versionSeparator + base64.StdEncoding.EncodeToString([]byte("short")),
		"分隔符过多":      versionSeparator + VersionArgon2idV1 + versionSeparator + validPayload + versionSeparator + "extra",
	}
	for name, hash := range cases {
		ok, err := Verify(ctx, testPlaintext, hash, salt)
		if err == nil {
			t.Errorf("用例「%s」应返回 error，实际 (ok=%v, nil)", name, ok)
		}
		if ok {
			t.Errorf("用例「%s」竟校验通过", name)
		}
	}
}

// TestVerifyMalformedSaltReturnsError 盐有问题同样必须返回 error。
// 盐被截断后 Argon2 仍能算出结果，若不校验长度就会静默恒不匹配，
// 表现为「密码明明没错却登不进去」，且被 A-8 计成失败次数。
func TestVerifyMalformedSaltReturnsError(t *testing.T) {
	ctx := context.Background()
	hash, _, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	params := paramsByVersion[VersionArgon2idV1]
	cases := map[string]string{
		"空盐":       "",
		"非法Base64": "!!!not-base64!!!",
		"盐过短":      base64.StdEncoding.EncodeToString(make([]byte, params.saltLen-1)),
		"盐过长":      base64.StdEncoding.EncodeToString(make([]byte, params.saltLen+1)),
	}
	for name, salt := range cases {
		ok, err := Verify(ctx, testPlaintext, hash, salt)
		if err == nil {
			t.Errorf("用例「%s」应返回 error，实际 (ok=%v, nil)", name, ok)
		}
		if ok {
			t.Errorf("用例「%s」竟校验通过", name)
		}
	}
}

// TestVerifySwappedSalt 两个用户的盐互换后必须校验失败——盐与哈希是一对一绑定的。
func TestVerifySwappedSalt(t *testing.T) {
	ctx := context.Background()
	hashA, saltA, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	hashB, saltB, err := Hash(ctx, testPlaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	for _, c := range []struct {
		name, hash, salt string
	}{
		{"A的哈希配B的盐", hashA, saltB},
		{"B的哈希配A的盐", hashB, saltA},
	} {
		ok, err := Verify(ctx, testPlaintext, c.hash, c.salt)
		if err != nil {
			t.Fatalf("用例「%s」返回了 error: %+v", c.name, err)
		}
		if ok {
			t.Fatalf("用例「%s」竟校验通过——盐与哈希未绑定", c.name)
		}
	}
}

// TestParamsPinned 参数取值钉死用例：防止后人「顺手调低」安全参数。
// 取值为 RFC 9106 §4 第二组推荐值（time=3、memory=64 MiB、threads=4）+ OWASP 下限
// （盐 16 字节、哈希 32 字节）。要调**高**参数请新增版本项，不要改 v1
// ——改 v1 会让全部存量哈希对不上，而 A-3 已明确 admin 无恢复路径。
func TestParamsPinned(t *testing.T) {
	params, ok := paramsByVersion[VersionArgon2idV1]
	if !ok {
		t.Fatalf("版本表缺少 %s", VersionArgon2idV1)
	}
	if params.time != 3 {
		t.Errorf("time = %d，期望 3（RFC 9106 §4 第二组）", params.time)
	}
	if params.memory != 64*1024 {
		t.Errorf("memory = %d KiB，期望 %d KiB（64 MiB）", params.memory, 64*1024)
	}
	if params.threads != 4 {
		t.Errorf("threads = %d，期望 4", params.threads)
	}
	if params.saltLen != 16 {
		t.Errorf("saltLen = %d，期望 16", params.saltLen)
	}
	if params.keyLen != 32 {
		t.Errorf("keyLen = %d，期望 32", params.keyLen)
	}
}

// TestThreadsAffectsHash 钉死「threads 参与哈希计算」这一事实：
// 同一密码 + 同一盐在 threads=4 与 threads=8 下算出的哈希**不同**。
// 因此 threads 绝不能取 runtime.NumCPU()——换一台核数不同的机器部署，
// 全部用户的密码校验会立刻全部失败，叠加 A-3「admin 无恢复路径」即永久无法登录。
func TestThreadsAffectsHash(t *testing.T) {
	params := paramsByVersion[VersionArgon2idV1]
	salt := make([]byte, params.saltLen) // 固定盐，只让 threads 变化
	base := argon2.IDKey([]byte(testPlaintext), salt, params.time, params.memory, params.threads, params.keyLen)
	for _, threads := range []uint8{1, 2, 8} {
		other := argon2.IDKey([]byte(testPlaintext), salt, params.time, params.memory, threads, params.keyLen)
		if string(base) == string(other) {
			t.Fatalf("threads=%d 与 threads=%d 得到相同哈希——本断言失效，请复核 threads 不可自适应的结论",
				params.threads, threads)
		}
	}
}

// TestVersionTableConsistent 版本表自洽：currentVersion 必须在表内，且各版本参数均非零。
// threads=0 或 time=0 会让 x/crypto/argon2 直接 panic（deriveKey 的 "too small"/"too low"）。
func TestVersionTableConsistent(t *testing.T) {
	if _, ok := paramsByVersion[currentVersion]; !ok {
		t.Fatalf("currentVersion=%q 不在版本表内", currentVersion)
	}
	for version, params := range paramsByVersion {
		if version == "" {
			t.Errorf("版本表存在空版本名")
		}
		if strings.Contains(version, versionSeparator) {
			t.Errorf("版本名 %q 含分隔符 %q，会让哈希串切分歧义", version, versionSeparator)
		}
		if params.time == 0 || params.memory == 0 || params.threads == 0 {
			t.Errorf("版本 %q 参数含零值（会让 argon2 panic）: %+v", version, params)
		}
		if params.saltLen <= 0 || params.keyLen == 0 {
			t.Errorf("版本 %q 的盐/哈希长度非法: saltLen=%d keyLen=%d", version, params.saltLen, params.keyLen)
		}
	}
}

// TestHashVerifyConcurrent 并发安全：本包无共享可变状态。配合 -race 使用。
func TestHashVerifyConcurrent(t *testing.T) {
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash, salt, err := Hash(ctx, testPlaintext)
			if err != nil {
				errCh <- err
				return
			}
			ok, err := Verify(ctx, testPlaintext, hash, salt)
			if err != nil {
				errCh <- err
				return
			}
			if !ok {
				t.Errorf("并发场景下自身校验不通过")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发场景异常: %+v", err)
	}
}

// TestHashVerifyLongAndUnicode 无长度与编码假设：长明文与含中文/emoji 的明文都要能往返。
// 界面语言支持多语种（K-4），口令里出现非 ASCII 是现实场景。
func TestHashVerifyLongAndUnicode(t *testing.T) {
	ctx := context.Background()
	cases := map[string]string{
		"长明文1KiB":  strings.Repeat("a1#", 342), // 1026 字节
		"中文与emoji": "记账系统口令2026#🔐",
		"含空格":      "  两端有空格的口令 2026#  ",
	}
	for name, plaintext := range cases {
		hash, salt, err := Hash(ctx, plaintext)
		if err != nil {
			t.Errorf("用例「%s」生成哈希异常: %+v", name, err)
			continue
		}
		ok, err := Verify(ctx, plaintext, hash, salt)
		if err != nil {
			t.Errorf("用例「%s」校验异常: %+v", name, err)
			continue
		}
		if !ok {
			t.Errorf("用例「%s」校验不通过", name)
		}
		// 不做任何规范化：去掉空格后即不应再匹配
		if trimmed := strings.TrimSpace(plaintext); trimmed != plaintext {
			ok, err = Verify(ctx, trimmed, hash, salt)
			if err != nil {
				t.Errorf("用例「%s」去空格校验异常: %+v", name, err)
			}
			if ok {
				t.Errorf("用例「%s」去掉空格后竟仍匹配——存在意外的规范化", name)
			}
		}
	}
}
