package rdb

import (
	"sync"
	"testing"

	"github.com/cellargalaxy/go_common/util"
)

// 探针：建库期间盘上会短暂出现一个0字节占位文件，而open把0字节当空库、任何口令都开得起来。
// 读锁要是没罩住整个checkToken，探针就可能在那个窗口里对着占位文件给出「口令正确」的错误答案。
func TestProbeDuringCreate(t *testing.T) {
	for round := 0; round < 20; round++ {
		newTestDb(t)
		ctx := util.GenCtx()
		var wg sync.WaitGroup
		var wrong int
		var lock sync.Mutex
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3000; i++ {
				if err := CheckToken(newTokenCtx("bogus-client-token")); err == nil {
					lock.Lock()
					wrong++
					lock.Unlock()
				}
			}
		}()
		if err := Create(ctx); err != nil {
			t.Fatalf("建库异常: %+v", err)
		}
		wg.Wait()
		if wrong > 0 {
			t.Logf("round=%d 探针对着半成品库给出了 %d 次错误的「口令正确」", round, wrong)
			return
		}
	}
	t.Logf("20轮都没抓到")
}
