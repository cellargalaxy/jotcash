package rdb

import (
	"crypto/rand"
	"sync"

	"github.com/ncruces/go-sqlite3/vfs"
	vfs_adiantum "github.com/ncruces/go-sqlite3/vfs/adiantum"
	"golang.org/x/crypto/argon2"
	"lukechampine.com/adiantum"
	"lukechampine.com/adiantum/hbsh"
)

const vfsName = "jotcash"

// 派生参数必须与adiantum包默认的KDF逐字一致，差一个字节存量库文件就全打不开
const (
	kdfPepper  = "github.com/ncruces/go-sqlite3/vfs/adiantum"
	kdfTime    = 3
	kdfMemory  = 64 * 1024
	kdfThreads = 4
	kdfKeyLen  = 32
)

var keyCreator = new(tokenKeyCreator)

func registerVfs() {
	vfs.Register(vfsName, vfs_adiantum.Wrap(vfs.Find(""), keyCreator))
}

// 派生一次密钥要64MiB内存、几十毫秒，而每个请求都现开一条连接、都跑一遍PRAGMA textkey，
// 刷一次页面的几个并发请求就能把堆顶到几百MB，所以把口令算出来的密钥缓存下来
type tokenKeyCreator struct {
	lock sync.Mutex
	keys map[string][]byte
}

func (this *tokenKeyCreator) KDF(token string) []byte {
	if token == "" {
		key := make([]byte, kdfKeyLen)
		rand.Read(key)
		return key
	}

	//锁要罩住「查缓存→算→写回」整段：只罩住map的话，冷缓存时并发请求会各算一遍，缓存等于白加
	this.lock.Lock()
	defer this.lock.Unlock()
	if key := this.keys[token]; key != nil {
		return key
	}
	key := argon2.IDKey([]byte(token), []byte(kdfPepper), kdfTime, kdfMemory, kdfThreads, kdfKeyLen)
	if this.keys == nil {
		this.keys = make(map[string][]byte)
	}
	this.keys[token] = key
	return key
}

func (this *tokenKeyCreator) HBSH(key []byte) *hbsh.HBSH {
	if len(key) != kdfKeyLen {
		return nil
	}
	return adiantum.New(key)
}

// Argon2那64MiB与几十毫秒本就是给暴力猜口令设的门槛，错口令留在缓存里等于把门槛抹掉
func (this *tokenKeyCreator) drop(token string) {
	this.lock.Lock()
	defer this.lock.Unlock()
	delete(this.keys, token)
}
