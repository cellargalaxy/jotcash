package pwdhash

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestRandBytes 长度正确、两次取值不同、非正长度报错。
//
// 非正长度必须报错而不是返回空切片：调用方可能把返回值直接当盐使用，
// 静默返回空切片会产出一条「盐为空」的合法凭据。
func TestRandBytes(t *testing.T) {
	ctx := context.Background()

	for _, n := range []int{1, 8, 16, 32, 1024} {
		data, err := RandBytes(ctx, n)
		if err != nil {
			t.Fatalf("生成 %d 字节异常: %+v", n, err)
		}
		if len(data) != n {
			t.Errorf("长度 = %d, 期望 %d", len(data), n)
		}
	}

	//两次取值不同（16 字节相同的概率为 2^-128，可忽略）
	first, err := RandBytes(ctx, 16)
	if err != nil {
		t.Fatalf("首次生成异常: %+v", err)
	}
	second, err := RandBytes(ctx, 16)
	if err != nil {
		t.Fatalf("再次生成异常: %+v", err)
	}
	if string(first) == string(second) {
		t.Errorf("两次生成的随机字节相同，随机源疑似失效")
	}

	//不得返回全零切片（例如误用 make 而没真正填充）
	allZero := true
	for _, b := range first {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Errorf("生成的随机字节全为零，随机源未真正填充")
	}

	for _, n := range []int{0, -1, -16} {
		data, err := RandBytes(ctx, n)
		if err == nil {
			t.Errorf("长度 %d 未报错，返回长度 %d", n, len(data))
		}
		if data != nil {
			t.Errorf("长度 %d 报错时仍返回了非 nil 切片", n)
		}
	}
}

// TestRandIndex 取值恒落 [0, n)、上界非正报错、n=1 恒为 0。
func TestRandIndex(t *testing.T) {
	ctx := context.Background()

	for _, n := range []int{1, 2, 7, 10, 62, 95} {
		for i := 0; i < 200; i++ {
			index, err := RandIndex(ctx, n)
			if err != nil {
				t.Fatalf("上界 %d 生成异常: %+v", n, err)
			}
			if index < 0 || index >= n {
				t.Fatalf("上界 %d 得到越界下标 %d", n, index)
			}
		}
	}

	//n = 1 时唯一合法取值是 0
	for i := 0; i < 20; i++ {
		index, err := RandIndex(ctx, 1)
		if err != nil {
			t.Fatalf("上界 1 生成异常: %+v", err)
		}
		if index != 0 {
			t.Fatalf("上界 1 得到 %d, 期望恒为 0", index)
		}
	}

	for _, n := range []int{0, -1, -10} {
		index, err := RandIndex(ctx, n)
		if err == nil {
			t.Errorf("上界 %d 未报错，返回 %d", n, index)
		}
		if index != 0 {
			t.Errorf("上界 %d 报错时返回了非零下标 %d", n, index)
		}
	}
}

// TestRandIndexCoverage 粗查取值分布：足量采样应覆盖全部取值。
//
// 只查「每个取值都出现过」，不做严格的统计均匀性检验——无偏性由标准库
// crypto/rand.Int 的拒绝采样保证，本用例只拦「实现退化成常量或只取低位」
// 这类明显缺陷。
func TestRandIndexCoverage(t *testing.T) {
	ctx := context.Background()
	const n = 10
	const rounds = 2000

	seen := make(map[int]int, n)
	for i := 0; i < rounds; i++ {
		index, err := RandIndex(ctx, n)
		if err != nil {
			t.Fatalf("生成异常: %+v", err)
		}
		seen[index]++
	}
	for v := 0; v < n; v++ {
		if seen[v] == 0 {
			t.Errorf("取值 %d 在 %d 次采样中从未出现，分布疑似有偏", v, rounds)
		}
	}
}

// TestRandString 长度按 rune 计正确、字符全部来自字符集、非法入参报错。
func TestRandString(t *testing.T) {
	ctx := context.Background()
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*"

	for _, n := range []int{1, 10, 16, 64} {
		got, err := RandString(ctx, n, charset)
		if err != nil {
			t.Fatalf("生成长度 %d 异常: %+v", n, err)
		}
		if utf8.RuneCountInString(got) != n {
			t.Errorf("长度（按 rune）= %d, 期望 %d", utf8.RuneCountInString(got), n)
		}
		for _, r := range got {
			if !strings.ContainsRune(charset, r) {
				t.Errorf("出现字符集外的字符 %q", r)
			}
		}
	}

	//两次生成应不同（长度 16、字符集 63 时碰撞概率可忽略）
	first, err := RandString(ctx, 16, charset)
	if err != nil {
		t.Fatalf("首次生成异常: %+v", err)
	}
	second, err := RandString(ctx, 16, charset)
	if err != nil {
		t.Fatalf("再次生成异常: %+v", err)
	}
	if first == second {
		t.Errorf("两次生成的随机串相同，随机源疑似失效")
	}

	for _, n := range []int{0, -1} {
		got, err := RandString(ctx, n, charset)
		if err == nil {
			t.Errorf("长度 %d 未报错，返回长度 %d", n, len(got))
		}
		if got != "" {
			t.Errorf("长度 %d 报错时仍返回了非空串", n)
		}
	}
	if got, err := RandString(ctx, 8, ""); err == nil {
		t.Errorf("空字符集未报错，返回 %q", got)
	}
}

// TestRandStringMultiByteCharset 多字节字符集不得产出乱码。
//
// 若实现按 byte 而非 rune 采样，会截出半个 UTF-8 字符——结果既不是合法字符串，
// 长度也不对。本用例是那种缺陷的直接探针。
func TestRandStringMultiByteCharset(t *testing.T) {
	ctx := context.Background()
	//混合单字节与多字节字符，含 4 字节的 emoji
	const charset = "aB1!汉字符号αβ✓🙂"

	got, err := RandString(ctx, 32, charset)
	if err != nil {
		t.Fatalf("生成异常: %+v", err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("生成的串不是合法 UTF-8，说明按 byte 而非 rune 采样")
	}
	if utf8.RuneCountInString(got) != 32 {
		t.Errorf("长度（按 rune）= %d, 期望 32", utf8.RuneCountInString(got))
	}
	for _, r := range got {
		if !strings.ContainsRune(charset, r) {
			t.Errorf("出现字符集外的字符 %q", r)
		}
	}
}

// TestRandStringSingleCharCharset 单字符字符集必须产出该字符的重复串。
//
// 这是 RandIndex 上界为 1 的边界在 RandString 上的体现，确认边界处不退化为报错。
func TestRandStringSingleCharCharset(t *testing.T) {
	ctx := context.Background()
	got, err := RandString(ctx, 5, "x")
	if err != nil {
		t.Fatalf("生成异常: %+v", err)
	}
	if got != "xxxxx" {
		t.Errorf("得到 %q, 期望 \"xxxxx\"", got)
	}
}
