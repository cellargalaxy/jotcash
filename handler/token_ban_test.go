package handler

import (
	"sync"
	"testing"
	"time"
)

// 窗口内攒够次数才封，第五次落下的那一刻起封满一个封禁时长
func TestTokenBan(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 1; count < tokenFailMax; count++ {
		if ban.fail(now) {
			t.Fatalf("第%d次失败不该触发封禁", count)
		}
		if remain := ban.check(now); remain != 0 {
			t.Fatalf("第%d次失败后不该被封: remain=%s", count, remain)
		}
	}
	if !ban.fail(now) {
		t.Fatalf("第%d次失败应触发封禁", tokenFailMax)
	}
	if remain := ban.check(now); remain != tokenBanDuration {
		t.Errorf("封禁剩余时长不符: got=%s want=%s", remain, tokenBanDuration)
	}
	if remain := ban.check(now.Add(tokenBanDuration - time.Second)); remain != time.Second {
		t.Errorf("封禁到期前应还在封: got=%s want=%s", remain, time.Second)
	}
	if remain := ban.check(now.Add(tokenBanDuration)); remain != 0 {
		t.Errorf("封禁到期应自动解除: remain=%s", remain)
	}
}

// 失败之间隔得比窗口还开，再多次也不该累积成封禁
func TestTokenBanOutOfWindow(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 0; count < tokenFailMax*2; count++ {
		if ban.fail(now.Add(time.Duration(count) * tokenFailWindow)) {
			t.Fatalf("窗口外的失败不该累积成封禁: 第%d次", count+1)
		}
	}
}

// 封禁期内的请求根本进不到校验，解封之后必须从零开始数，不能被上一轮的旧账立刻再封一次
func TestTokenBanReset(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()

	for count := 0; count < tokenFailMax; count++ {
		ban.fail(now)
	}
	unbanAt := now.Add(tokenBanDuration)
	if remain := ban.check(unbanAt); remain != 0 {
		t.Fatalf("封禁到期应自动解除: remain=%s", remain)
	}
	if ban.fail(unbanAt) {
		t.Errorf("解封后的第一次失败不该立刻又封")
	}
}

// 爆破本来就是并发打进来的，计数不能被并发数冲乱：同一时刻的失败每满一批触发一次封禁
func TestTokenBanConcurrent(t *testing.T) {
	ban := new(tokenBan)
	now := time.Now()
	batch := 4

	var wait sync.WaitGroup
	var lock sync.Mutex
	banned := 0
	for count := 0; count < tokenFailMax*batch; count++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if !ban.fail(now) {
				return
			}
			lock.Lock()
			defer lock.Unlock()
			banned++
		}()
	}
	wait.Wait()

	if banned != batch {
		t.Errorf("触发封禁的次数不符: got=%d want=%d", banned, batch)
	}
}
