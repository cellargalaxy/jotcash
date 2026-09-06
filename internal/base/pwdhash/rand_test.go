package pwdhash

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// base32Alphabet crypto/rand.Text 使用的字母表（RFC 4648 base32，去掉填充）。
// 用于断言 RandText 的取值范围，与标准库 crypto/rand/text.go 的 base32alphabet 一致。
const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

func TestRandBytesLength(t *testing.T) {
	ctx := context.Background()
	for _, n := range []int{1, 16, 32, 1024} {
		buf, err := RandBytes(ctx, n)
		if err != nil {
			t.Fatalf("n=%d 生成异常: %+v", n, err)
		}
		if len(buf) != n {
			t.Errorf("n=%d 实际长度 %d", n, len(buf))
		}
	}
}

// TestRandBytesInvalidLength n ≤ 0 必须报错而非静默返回空切片——
// 静默返回会让「盐为空」这类致命问题一路滑到落库。
func TestRandBytesInvalidLength(t *testing.T) {
	ctx := context.Background()
	for _, n := range []int{0, -1, -16} {
		buf, err := RandBytes(ctx, n)
		if err == nil {
			t.Errorf("n=%d 应报错，实际返回 %d 字节", n, len(buf))
		}
		if buf != nil {
			t.Errorf("n=%d 出错时应返回 nil，实际 %d 字节", n, len(buf))
		}
	}
}

// TestRandBytesDiffers 两次取样不得相同——这是「随机」的最低可观测证据。
// 32 字节全同的概率为 2^-256，可安全断言。
func TestRandBytesDiffers(t *testing.T) {
	ctx := context.Background()
	first, err := RandBytes(ctx, 32)
	if err != nil {
		t.Fatalf("生成异常: %+v", err)
	}
	second, err := RandBytes(ctx, 32)
	if err != nil {
		t.Fatalf("生成异常: %+v", err)
	}
	if string(first) == string(second) {
		t.Fatalf("两次取样完全相同，随机源可疑")
	}
}

// TestRandBytesNotAllZero 全零切片说明随机源没写入——make 出来本就是全零，
// 若实现忘了调用 rand.Read，前面的长度断言仍会通过，只有本用例能发现。
func TestRandBytesNotAllZero(t *testing.T) {
	ctx := context.Background()
	buf, err := RandBytes(ctx, 32)
	if err != nil {
		t.Fatalf("生成异常: %+v", err)
	}
	allZero := true
	for _, b := range buf {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatalf("32 字节全为零，随机源未写入")
	}
}

func TestRandIntInRange(t *testing.T) {
	ctx := context.Background()
	for _, max := range []int{1, 2, 10, 62, 95, 1000} {
		for i := 0; i < 200; i++ {
			n, err := RandInt(ctx, max)
			if err != nil {
				t.Fatalf("max=%d 生成异常: %+v", max, err)
			}
			if n < 0 || n >= max {
				t.Fatalf("max=%d 返回 %d，越出 [0, %d)", max, n, max)
			}
		}
	}
}

// TestRandIntMaxOne max=1 时唯一合法取值是 0（crypto/rand.Int 的 bitLen==0 分支）。
func TestRandIntMaxOne(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		n, err := RandInt(ctx, 1)
		if err != nil {
			t.Fatalf("生成异常: %+v", err)
		}
		if n != 0 {
			t.Fatalf("max=1 返回 %d，期望 0", n)
		}
	}
}

// TestRandIntInvalidMax max ≤ 0 必须报错。
// 关键点：crypto/rand.Int 对 max <= 0 是 **panic**（crypto/rand/util.go：
// 「It panics if max <= 0」），入口不挡住就会让 panic 冒到 HTTP 层。
// 本用例若触发 panic，测试进程会直接崩，即为回归失败。
func TestRandIntInvalidMax(t *testing.T) {
	ctx := context.Background()
	for _, max := range []int{0, -1, -100} {
		n, err := RandInt(ctx, max)
		if err == nil {
			t.Errorf("max=%d 应报错，实际返回 %d", max, n)
		}
		if n != 0 {
			t.Errorf("max=%d 出错时应返回 0，实际 %d", max, n)
		}
	}
}

// TestRandIntCoversAllValues 小上界下全部取值可达：
// 若实现有模偏差或漏掉边界（如恒不返回 max-1），本用例会失败。
// max=4 取 400 次，每个值缺失的概率约 (3/4)^400 ≈ 1e-50。
func TestRandIntCoversAllValues(t *testing.T) {
	ctx := context.Background()
	const max = 4
	seen := make(map[int]int, max)
	for i := 0; i < 400; i++ {
		n, err := RandInt(ctx, max)
		if err != nil {
			t.Fatalf("生成异常: %+v", err)
		}
		seen[n]++
	}
	for want := 0; want < max; want++ {
		if seen[want] == 0 {
			t.Errorf("取值 %d 在 400 次取样中从未出现，分布可疑: %v", want, seen)
		}
	}
}

// TestRandIntNoObviousSkew 无明显偏斜：max 取非 2 的幂（模偏差最容易暴露之处），
// 检查每个桶的频次落在期望值的 ±50% 内。
// 阈值刻意放宽，只用于捕获「系统性偏斜」这类实现错误，不做严格统计检验。
func TestRandIntNoObviousSkew(t *testing.T) {
	ctx := context.Background()
	const (
		max     = 3 // 非 2 的幂
		samples = 3000
	)
	counts := make([]int, max)
	for i := 0; i < samples; i++ {
		n, err := RandInt(ctx, max)
		if err != nil {
			t.Fatalf("生成异常: %+v", err)
		}
		counts[n]++
	}
	expected := samples / max
	for value, count := range counts {
		if count < expected/2 || count > expected*3/2 {
			t.Errorf("取值 %d 出现 %d 次，期望约 %d 次（±50%%），分布偏斜: %v",
				value, count, expected, counts)
		}
	}
}

// TestRandTextFormat 长度恒 26、字符全在 base32 字母表内（约 128 位熵）。
func TestRandTextFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		text := RandText()
		if len(text) != 26 {
			t.Fatalf("长度 = %d，期望 26：%q", len(text), text)
		}
		for _, char := range text {
			if !strings.ContainsRune(base32Alphabet, char) {
				t.Fatalf("出现字母表外的字符 %q：%q", char, text)
			}
		}
	}
}

func TestRandTextDiffers(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		text := RandText()
		if _, dup := seen[text]; dup {
			t.Fatalf("%d 次取样内出现重复：%q", n, text)
		}
		seen[text] = struct{}{}
	}
}

// TestRandConcurrent 并发安全：三个随机源入口无共享可变状态。配合 -race 使用。
func TestRandConcurrent(t *testing.T) {
	ctx := context.Background()
	const n = 32
	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := RandBytes(ctx, 16); err != nil {
				errCh <- err
			}
			if _, err := RandInt(ctx, 62); err != nil {
				errCh <- err
			}
			_ = RandText()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发场景异常: %+v", err)
	}
}
