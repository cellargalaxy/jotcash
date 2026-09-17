package rdb

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

// 自建VFS只把派生密钥缓存起来，派生结果必须与adiantum包默认的KDF一字不差，否则升级上去存量库文件全打不开
func TestVfsCompatible(t *testing.T) {
	ctx := newTokenCtx(testClientToken)
	dbPath := t.TempDir() + "/adiantum.db"
	if err := os.WriteFile(dbPath, nil, 0640); err != nil {
		t.Fatalf("写库文件异常: %+v", err)
	}

	//用adiantum包自己注册的那个VFS建库写数据，当作升级前落在盘上的存量库
	sqlDb, err := driver.Open(fmt.Sprintf("file:%s?vfs=adiantum&_txlock=immediate", dbPath), func(conn *sqlite3.Conn) error {
		return conn.Exec(fmt.Sprintf("PRAGMA textkey=%s;", sqlite3.Quote(testClientToken)))
	})
	if err != nil {
		t.Fatalf("adiantum建库异常: %+v", err)
	}
	if _, err = sqlDb.ExecContext(ctx, "create table t(a integer);insert into t values(42);"); err != nil {
		t.Fatalf("adiantum写数据异常: %+v", err)
	}
	util.CloseIo(ctx, sqlDb)

	gormDb, err := open(ctx, dbPath, testClientToken)
	if err != nil {
		t.Fatalf("自建VFS打不开adiantum建的存量库: %+v", err)
	}
	var value int
	err = gormDb.Raw("select a from t").Scan(&value).Error
	util.CloseDb(ctx, gormDb)
	if err != nil || value != 42 {
		t.Errorf("存量库里的数据读不回来: value=%d err=%+v", value, err)
	}

	if gormDb, err = open(ctx, dbPath, "wrong-client-token"); err == nil {
		util.CloseDb(ctx, gormDb)
		t.Errorf("错口令仍不应打开存量库")
	}
}

// 锁只罩住map、把Argon2留在锁外的话，冷缓存时并发请求会各算一遍，刷一次页面照样几百MB
func TestKeyCreatorConcurrent(t *testing.T) {
	token := "concurrent-kdf-token"
	defer keyCreator.drop(token)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	keys := make([][]byte, 6)
	var wait sync.WaitGroup
	for index := range keys {
		wait.Add(1)
		go func() {
			defer wait.Done()
			keys[index] = keyCreator.KDF(token)
		}()
	}
	wait.Wait()
	runtime.ReadMemStats(&after)

	//一次Argon2要64MiB，算两次就会顶破这条线
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 96<<20 {
		t.Errorf("并发首次派生密钥应只算一次Argon2: got=%dMB want<96MB", alloc>>20)
	}
	for index := range keys {
		if len(keys[index]) != kdfKeyLen || !bytes.Equal(keys[0], keys[index]) {
			t.Fatalf("并发派生出来的密钥不一致: %x %x", keys[0], keys[index])
		}
	}
}

// Argon2那64MiB与几十毫秒本就是给暴力猜口令设的门槛，错口令留在缓存里等于把门槛抹掉
func TestKeyCreatorDropOnFail(t *testing.T) {
	ctx := newTestCtx(t)
	token := "wrong-client-token"

	for round := 0; round < 2; round++ {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if gormDb, err := open(ctx, config.DbPath, token); err == nil {
			util.CloseDb(ctx, gormDb)
			t.Fatalf("错口令应报错")
		}
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc < 32<<20 {
			t.Errorf("第%d次用错口令开库仍应重算Argon2: got=%dMB want>32MB", round+1, alloc>>20)
		}
	}
}
