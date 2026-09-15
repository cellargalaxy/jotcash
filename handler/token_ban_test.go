package handler

import (
	"sync"
	"testing"
	"time"
)

const (
	testFailLimit   = 5
	testBanDuration = 5 * time.Minute
)

// 失败计数是包级的，而测试里每条用例各起一套engine，计数得跟着归零，否则上一条用例的失败会把下一条封掉
func ResetValidateBan() {
	validateBan = new(tokenBan)
}

// 攒够次数才封，封到最后一次失败起算的失效时刻
func TestTokenBan(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 1; count < testFailLimit; count++ {
		if got := ban.fail(now, testBanDuration); got != count {
			t.Fatalf("累计次数不符: got=%d want=%d", got, count)
		}
		if ban.check(now, testFailLimit) {
			t.Fatalf("第%d次失败后不该被封", count)
		}
	}

	if got := ban.fail(now, testBanDuration); got != testFailLimit {
		t.Fatalf("累计次数不符: got=%d want=%d", got, testFailLimit)
	}
	if !ban.check(now, testFailLimit) {
		t.Fatalf("第%d次失败应触发封禁", testFailLimit)
	}
	if !ban.check(now.Add(testBanDuration-time.Second), testFailLimit) {
		t.Errorf("封禁到期前应还在封")
	}
	if ban.check(now.Add(testBanDuration), testFailLimit) {
		t.Errorf("封禁到期应自动解除")
	}
}

// 失败之间隔得比封禁时长还开，计数每次都归零，再多次也不该攒成封禁
func TestTokenBanExpire(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 0; count < testFailLimit*2; count++ {
		failAt := now.Add(time.Duration(count) * testBanDuration)
		if got := ban.fail(failAt, testBanDuration); got != 1 {
			t.Fatalf("失效之后的失败应从1重新数: got=%d", got)
		}
		if ban.check(failAt, testFailLimit) {
			t.Fatalf("隔开的失败不该攒成封禁: 第%d次", count+1)
		}
	}
}

// 解封之后必须从零开始数，不能被上一轮的旧账立刻再封一次
func TestTokenBanReset(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 0; count < testFailLimit; count++ {
		ban.fail(now, testBanDuration)
	}
	unbanAt := now.Add(testBanDuration)
	if ban.check(unbanAt, testFailLimit) {
		t.Fatalf("封禁到期应自动解除")
	}
	if ban.fail(unbanAt, testBanDuration); ban.check(unbanAt, testFailLimit) {
		t.Errorf("解封后的第一次失败不该立刻又封")
	}
}

// 爆破本来就是并发打进来的，计数不能被并发冲掉
func TestTokenBanConcurrent(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()
	total := testFailLimit * 4

	var wait sync.WaitGroup
	for count := 0; count < total; count++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ban.fail(now, testBanDuration)
		}()
	}
	wait.Wait()

	if ban.count != total {
		t.Errorf("并发累计次数不符: got=%d want=%d", ban.count, total)
	}
}
