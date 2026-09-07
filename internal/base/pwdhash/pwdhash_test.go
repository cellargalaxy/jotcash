package pwdhash

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"golang.org/x/crypto/argon2"
)

// TestHashFormat 锁定哈希列与盐列的对外形态。
//
// 三条性质被下游依赖：哈希列带版本前缀（Verify 靠它查参数）、盐恰 16 字节、
// 派生密钥恰 32 字节（store 的列宽与 Verify 的长度校验都按此）。
func TestHashFormat(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, "Str0ng-Passw0rd!")
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}

	version, keyText, ok := strings.Cut(hash, versionSeparator)
	if !ok {
		t.Fatalf("哈希列缺少版本分隔符: 长度 %d", len(hash))
	}
	if version != Version {
		t.Errorf("哈希列版本 = %s, 期望 %s", version, Version)
	}

	par := paramTable[Version]
	keyData, err := base64.RawStdEncoding.Strict().DecodeString(keyText)
	if err != nil {
		t.Fatalf("派生密钥无法 base64 解码: %+v", err)
	}
	if len(keyData) != int(par.keyLen) {
		t.Errorf("派生密钥长度 = %d, 期望 %d", len(keyData), par.keyLen)
	}

	saltData, err := base64.RawStdEncoding.Strict().DecodeString(salt)
	if err != nil {
		t.Fatalf("盐无法 base64 解码: %+v", err)
	}
	if len(saltData) != int(par.saltLen) {
		t.Errorf("盐长度 = %d, 期望 %d", len(saltData), par.saltLen)
	}

	//盐必须独立成列，不得内嵌进哈希列（终版 §四 User.盐 是单列属性）
	if strings.Contains(hash, salt) {
		t.Errorf("哈希列内含盐，违反「盐独立列存」口径")
	}
	//Argon2 参数不得出现在哈希列里（承载口径：参数写死在包内，不进哈希串）
	for _, bad := range []string{"m=", "t=", "p=", "v=19"} {
		if strings.Contains(hash, bad) {
			t.Errorf("哈希列内含参数片段 %q，应只有版本前缀 + 派生密钥", bad)
		}
	}
}

// TestHashSaltIsRandom 验证随机盐真的生效。
//
// 若盐退化为常量（例如误用固定字节或 math/rand 未播种），同一明文两次哈希会得到
// 相同结果，等于给撞库攻击预置了彩虹表。本用例是那种退化的直接探针。
func TestHashSaltIsRandom(t *testing.T) {
	ctx := context.Background()
	const password = "Str0ng-Passw0rd!"

	hash1, salt1, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("首次生成哈希异常: %+v", err)
	}
	hash2, salt2, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("再次生成哈希异常: %+v", err)
	}

	if salt1 == salt2 {
		t.Errorf("同一明文两次生成的盐相同，随机盐未生效")
	}
	if hash1 == hash2 {
		t.Errorf("同一明文两次生成的哈希相同，随机盐未生效")
	}
	//两者都必须能各自校验通过
	for i, pair := range [][2]string{{hash1, salt1}, {hash2, salt2}} {
		ok, err := Verify(ctx, password, pair[0], pair[1])
		if err != nil {
			t.Fatalf("第 %d 组校验异常: %+v", i+1, err)
		}
		if !ok {
			t.Errorf("第 %d 组校验不通过", i+1)
		}
	}
}

// TestHashRejectEmptyPassword 空明文必须报错，不得产出一条合法凭据。
//
// 这不是 A-8 策略校验（那属 rule/passwd），而是「禁止把空口令写成凭据」的
// 失败关闭兜底。
func TestHashRejectEmptyPassword(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, "")
	if err == nil {
		t.Fatalf("空明文未报错，得到哈希长度 %d、盐长度 %d", len(hash), len(salt))
	}
	if hash != "" || salt != "" {
		t.Errorf("空明文报错时仍返回了非空结果")
	}
}

// TestVerifyMatch 正确明文必须校验通过，含多字节字符与超长口令。
func TestVerifyMatch(t *testing.T) {
	ctx := context.Background()
	passwords := []string{
		"Str0ng-Passw0rd!",
		"最短也要十位以上的口令A1!",           // 多字节字符
		strings.Repeat("Aa1!", 64), // 256 字节长口令
		" 前后有空格的口令A1! ",            // 空格是口令的一部分，不得被裁剪
		"含\x00空字节的口令A1!",           // 空字节不得截断
	}
	for _, password := range passwords {
		hash, salt, err := Hash(ctx, password)
		if err != nil {
			t.Fatalf("生成哈希异常（口令长度 %d）: %+v", len(password), err)
		}
		ok, err := Verify(ctx, password, hash, salt)
		if err != nil {
			t.Fatalf("校验异常（口令长度 %d）: %+v", len(password), err)
		}
		if !ok {
			t.Errorf("正确口令校验不通过（口令长度 %d）", len(password))
		}
	}
}

// TestVerifyMismatch 错误明文必须判不匹配，且**不报错**。
//
// 不报错很关键：这是 A-8 连续失败计数要走的正常业务分支，若返回 error 会被
// 上层映射成「系统错误」，登录失败就不会计数、也不会触发 10 分钟锁定。
func TestVerifyMismatch(t *testing.T) {
	ctx := context.Background()
	const password = "Str0ng-Passw0rd!"
	hash, salt, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}

	wrongs := []struct {
		name  string
		input string
	}{
		{"完全不同", "An0ther-Passw0rd!"},
		{"大小写差异", "str0ng-passw0rd!"},
		{"尾部多空格", password + " "},
		{"首部多空格", " " + password},
		{"少最后一位", password[:len(password)-1]},
		{"多一位", password + "a"},
	}
	for _, w := range wrongs {
		ok, err := Verify(ctx, w.input, hash, salt)
		if err != nil {
			t.Errorf("%s：校验返回了 error（应为正常的不匹配）: %+v", w.name, err)
		}
		if ok {
			t.Errorf("%s：错误口令被判为匹配", w.name)
		}
	}
}

// TestVerifyWrongSalt 证明盐真的参与了计算。
//
// 用「正向 + 反向」一对断言，缺一不可：
//   - 正向：用配对的盐必须判匹配。若实现忽略盐（例如误传全零盐），这条会失败；
//   - 反向：换成另一次生成的**合法**盐必须判不匹配。
//
// 只有反向断言是不够的——实测过：若 Verify 把盐替换成全零，反向断言依然「通过」
// （因为算出的密钥与存量哈希本就不同），缺陷会被漏掉。正向断言才是那种缺陷的探针。
func TestVerifyWrongSalt(t *testing.T) {
	ctx := context.Background()
	const password = "Str0ng-Passw0rd!"
	hash, salt, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	_, otherSalt, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("生成另一组哈希异常: %+v", err)
	}
	if salt == otherSalt {
		t.Fatalf("两次生成的盐相同，用例前提不成立")
	}

	//正向：配对的盐必须匹配
	ok, err := Verify(ctx, password, hash, salt)
	if err != nil {
		t.Fatalf("配对盐校验异常: %+v", err)
	}
	if !ok {
		t.Errorf("配对的盐校验不通过，盐疑似未被正确使用")
	}

	//反向：换盐必须不匹配
	ok, err = Verify(ctx, password, hash, otherSalt)
	if err != nil {
		t.Fatalf("换盐校验异常: %+v", err)
	}
	if ok {
		t.Errorf("换盐后仍判匹配，说明盐未参与计算")
	}
}

// TestVerifyEmptyPassword 空明文校验返回 (false, nil)。
//
// 明文来自用户输入，空口令是正常业务分支；且 Hash 拒绝空明文，空明文不可能
// 匹配任何存量哈希，判不匹配是安全的。若此处报错，会把用户输入问题误报成系统故障。
func TestVerifyEmptyPassword(t *testing.T) {
	ctx := context.Background()
	hash, salt, err := Hash(ctx, "Str0ng-Passw0rd!")
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}

	ok, err := Verify(ctx, "", hash, salt)
	if err != nil {
		t.Errorf("空明文校验返回了 error（应为正常的不匹配）: %+v", err)
	}
	if ok {
		t.Errorf("空明文被判为匹配")
	}
}

// TestVerifyBrokenHashOrSalt 哈希列或盐列本身不可用时必须**报错**。
//
// 这两个值来自库内存量数据，异常即数据损坏或跨版本不兼容。必须与「密码错误」
// 区分开——否则数据损坏会被静默显示成「密码错误」，运维无从发现。
func TestVerifyBrokenHashOrSalt(t *testing.T) {
	ctx := context.Background()
	const password = "Str0ng-Passw0rd!"
	goodHash, goodSalt, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	_, keyText, _ := strings.Cut(goodHash, versionSeparator)

	cases := []struct {
		name string
		hash string
		salt string
	}{
		{"哈希为空", "", goodSalt},
		{"盐为空", goodHash, ""},
		{"两者皆空", "", ""},
		{"哈希无版本前缀", keyText, goodSalt},
		{"未知版本", "argon2id_v99" + versionSeparator + keyText, goodSalt},
		{"版本大小写不符", "ARGON2ID_V1" + versionSeparator + keyText, goodSalt},
		{"哈希 base64 非法", Version + versionSeparator + "!!!not-base64!!!", goodSalt},
		{"盐 base64 非法", goodHash, "!!!not-base64!!!"},
		{"哈希带 base64 填充", Version + versionSeparator + base64.StdEncoding.EncodeToString(make([]byte, 32)), goodSalt},
		{"盐长度不符", goodHash, base64.RawStdEncoding.EncodeToString(make([]byte, 8))},
		{"派生密钥长度不符", Version + versionSeparator + base64.RawStdEncoding.EncodeToString(make([]byte, 16)), goodSalt},
		{"哈希列为空前缀", versionSeparator + keyText, goodSalt},
	}
	for _, c := range cases {
		ok, err := Verify(ctx, password, c.hash, c.salt)
		if err == nil {
			t.Errorf("%s：未报错（数据损坏会被误显示成密码错误）", c.name)
		}
		if ok {
			t.Errorf("%s：被判为匹配", c.name)
		}
	}

	//空明文也不能掩盖数据损坏：解析失败优先于「空明文判不匹配」
	if _, err := Verify(ctx, "", "", ""); err == nil {
		t.Errorf("空明文 + 空哈希未报错，数据损坏被掩盖")
	}
}

// TestParamsPinned 是**参数回归**用例，把「参数写死在包内」这条承载钉成可执行断言。
//
// 两件事被锁定：
//  1. 参数表取值恰为 RFC 9106 §4 第二推荐档（t=3 / m=64MiB / p=4）+ 128 位盐 +
//     256 位派生密钥。任何人改动参数都会让本用例失败，从而必须显式新增版本条目
//     而不是就地改值——就地改值会让全部存量哈希失效。
//  2. Verify 的计算与直接调 argon2.IDKey 一致，即本包没有在算法上加私货。
func TestParamsPinned(t *testing.T) {
	ctx := context.Background()

	par, ok := paramTable[Version]
	if !ok {
		t.Fatalf("当前版本 %s 在参数表中缺失", Version)
	}
	if par.time != 3 {
		t.Errorf("迭代次数 = %d, 期望 3（RFC 9106 §4 第二推荐档）", par.time)
	}
	if par.memory != 64*1024 {
		t.Errorf("内存 = %d KiB, 期望 %d KiB（64 MiB）", par.memory, 64*1024)
	}
	if par.threads != 4 {
		t.Errorf("并行度 = %d, 期望 4（必须为常量，不得取 runtime.NumCPU）", par.threads)
	}
	if par.saltLen != 16 {
		t.Errorf("盐长度 = %d 字节, 期望 16（128 位）", par.saltLen)
	}
	if par.keyLen != 32 {
		t.Errorf("派生密钥长度 = %d 字节, 期望 32（256 位）", par.keyLen)
	}

	//固定盐 + 固定明文，比对 Verify 与官方库直算的结果
	const password = "Str0ng-Passw0rd!"
	saltData := []byte("0123456789abcdef")
	wantKey := argon2.IDKey([]byte(password), saltData, par.time, par.memory, par.threads, par.keyLen)
	hash := Version + versionSeparator + base64.RawStdEncoding.EncodeToString(wantKey)
	salt := base64.RawStdEncoding.EncodeToString(saltData)

	ok, err := Verify(ctx, password, hash, salt)
	if err != nil {
		t.Fatalf("校验异常: %+v", err)
	}
	if !ok {
		t.Errorf("Verify 与 argon2.IDKey 直算结果不一致，参数或算法被改动")
	}

	//并行度参与计算的证据：换 p 必须校验不通过。这条性质正是 threads 不能取
	//runtime.NumCPU() 的原因——跨机器核数不同会让存量哈希全部失效
	otherKey := argon2.IDKey([]byte(password), saltData, par.time, par.memory, par.threads/2, par.keyLen)
	if string(otherKey) == string(wantKey) {
		t.Errorf("改并行度未改变派生密钥，与实测不符")
	}
}

// TestNoPasswordInLog 是**密码红线**回归用例（分层方案 §九 红线行）。
//
// 逐个触发 Hash / Verify 的全部失败分支，抓取本包产生的所有日志，断言明文、
// 哈希与盐均未出现。红线无编译期兜底，只能靠这类断言 + 评审。
func TestNoPasswordInLog(t *testing.T) {
	ctx := context.Background()
	const password = "Str0ng-Passw0rd-In-Log!"

	goodHash, goodSalt, err := Hash(ctx, password)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}
	_, keyText, _ := strings.Cut(goodHash, versionSeparator)

	//挂 test hook 抓日志。logrus 是全局单例，故先存后复原，避免污染其他用例
	hook := logrustest.NewGlobal()
	oldLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.TraceLevel)
	defer func() {
		logrus.SetLevel(oldLevel)
		hook.Reset()
	}()

	//覆盖所有会打日志的分支
	Hash(ctx, "")
	Verify(ctx, password, "", goodSalt)
	Verify(ctx, password, goodHash, "")
	Verify(ctx, password, keyText, goodSalt)
	Verify(ctx, password, "argon2id_v99"+versionSeparator+keyText, goodSalt)
	Verify(ctx, password, Version+versionSeparator+"!!!bad!!!", goodSalt)
	Verify(ctx, password, goodHash, "!!!bad!!!")
	Verify(ctx, password, goodHash, base64.RawStdEncoding.EncodeToString(make([]byte, 8)))
	Verify(ctx, password, Version+versionSeparator+base64.RawStdEncoding.EncodeToString(make([]byte, 16)), goodSalt)
	RandBytes(ctx, 0)
	RandIndex(ctx, 0)
	RandString(ctx, 0, "abc")
	RandString(ctx, 8, "")

	if len(hook.AllEntries()) == 0 {
		t.Fatalf("未抓到任何日志，用例失去意义（检查 test hook 是否生效）")
	}

	//把每条日志的消息与全部字段值拼成一行做检查
	secrets := map[string]string{
		"明文":   password,
		"哈希列":  goodHash,
		"派生密钥": keyText,
		"盐":    goodSalt,
	}
	for i, entry := range hook.AllEntries() {
		var sb strings.Builder
		sb.WriteString(entry.Message)
		for k, v := range entry.Data {
			sb.WriteString(" ")
			sb.WriteString(k)
			sb.WriteString("=")
			//err 字段是 error，用 %v 会走 Error()，与实际落盘形态一致
			if e, ok := v.(error); ok {
				sb.WriteString(e.Error())
				continue
			}
			if s, ok := v.(string); ok {
				sb.WriteString(s)
				continue
			}
			sb.WriteString(entry.Message) // 非字符串字段（长度、版本等）不含密码，占位即可
		}
		line := sb.String()
		for name, secret := range secrets {
			if secret == "" {
				continue
			}
			if strings.Contains(line, secret) {
				t.Errorf("第 %d 条日志含%s，违反密码红线: %s", i, name, entry.Message)
			}
		}
	}
}

// TestHashVerifyConcurrent 并发下 Hash / Verify 无竞态、结果正确。
//
// 本包无包级可变状态，本用例主要是给 -race 提供覆盖，同时确认 Argon2 的
// 并行实现（threads=4，内部起 goroutine）在并发调用下不互相干扰。
func TestHashVerifyConcurrent(t *testing.T) {
	ctx := context.Background()
	const goroutines = 8

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			password := "Str0ng-Passw0rd!" + string(rune('A'+g))
			hash, salt, err := Hash(ctx, password)
			if err != nil {
				errCh <- err
				return
			}
			ok, err := Verify(ctx, password, hash, salt)
			if err != nil {
				errCh <- err
				return
			}
			if !ok {
				errCh <- errors.Errorf("并发第 %d 个 goroutine 校验不通过", g)
				return
			}
			//交叉校验：别人的口令不该匹配自己的哈希
			other, err := Verify(ctx, "Str0ng-Passw0rd!Z", hash, salt)
			if err != nil {
				errCh <- err
				return
			}
			if other {
				errCh <- errors.Errorf("并发第 %d 个 goroutine 误判他人口令为匹配", g)
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发用例失败: %+v", err)
	}
}
