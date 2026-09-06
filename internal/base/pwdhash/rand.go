package pwdhash

// 密码学安全随机源。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/pwdhash` 行：
// 「对外另出的「安全随机串」**只供 `rule/passwd` 采样初始密码**（A-2）」。
//
// 为什么只出「无偏采样能力」而不出「生成初始密码」函数：
// A-8 的「长度 ≥ 10、字母/数字/符号至少两类」是**业务规则**，字符集属业务语义，
// 按约定 5「`base/` 是纯技术层：任何带业务语义的类型都不得放入」必须留在 `rule/passwd`
// ——§六 L2 表已把「符合策略的随机初始密码生成」判给 `rule/passwd`，并注明
// 「采样自 `base/pwdhash` 的安全随机源」。本包只负责「随机得对」，字符集由它自己持有。
//
// ⚠️ 绝不能改用 `go_common/util` 的 CreateRandString：
// 它底层是 math/rand（util/gen.go 的 rand.Intn），**不是密码学安全随机源**，
// 用来生成盐或 A-2 初始密码会让口令可被预测，直接击穿 A-2「随机生成、仅交付一次」。

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// RandBytes 返回 n 个密码学安全的随机字节，供包内生成盐、以及 rule/passwd 按需取熵。
//
// n ≤ 0 时返回错误而非空切片：长度非法多半是调用方算错了长度，
// 静默返回空切片会让「盐为空」这类致命问题一路滑到落库。
func RandBytes(ctx context.Context, n int) ([]byte, error) {
	if n <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n}).Error("生成随机字节，长度非法")
		return nil, errors.Errorf("生成随机字节，长度非法: %d", n)
	}
	buf := make([]byte, n)
	// crypto/rand.Read 的文档写明「never returns an error」——底层失败时它直接 fatal
	// 崩溃进程。此处仍显式判错，作为防御带保留：签名不承诺永不出错，行为就不依赖它。
	if _, err := rand.Read(buf); err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n, "err": err}).Error("生成随机字节，异常")
		return nil, errors.Errorf("生成随机字节，异常: %+v", err)
	}
	return buf, nil
}

// RandInt 返回 [0, max) 内均匀分布的随机整数，供 rule/passwd 从字符集中采样（A-2）。
//
// 用 crypto/rand.Int 而不是「随机字节 % max」：后者在 max 不是 2 的幂时有**模偏差**
// ——落在余数区间内的取值出现概率更高，A-2 初始密码的实际熵会低于名义值。
// crypto/rand.Int 内部按位掩码 + 拒绝采样，无偏。
//
// max ≤ 0 时返回错误：crypto/rand.Int 对此是 **panic**（crypto/rand/util.go 的 Int：
// 「It panics if max <= 0」），必须在入口挡住，不能让 panic 冒到 HTTP 层。
func RandInt(ctx context.Context, max int) (int, error) {
	if max <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"max": max}).Error("生成随机整数，上界非法")
		return 0, errors.Errorf("生成随机整数，上界非法: %d", max)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"max": max, "err": err}).Error("生成随机整数，异常")
		return 0, errors.Errorf("生成随机整数，异常: %+v", err)
	}
	// n ∈ [0, max) 且 max 为 int，转换必然不溢出
	return int(n.Int64()), nil
}

// RandText 返回一个 26 字符的 base32 随机串（约 128 位熵），
// 供 rule/passwd 在不需要控制字符集时直接取用（A-2 的「安全随机串」）。
//
// 直接复用标准库 crypto/rand.Text，不自行拼装：它已保证密码学安全且无模偏差。
// 无 ctx 入参也无 error 返回，与标准库签名一致——它内部走 rand.Read，
// 失败即 fatal，不存在可供调用方处置的错误分支（go_common 的
// CreateRandString(n int) string 同样是无 ctx 无 error 的先例）。
//
// 注意 base32 字母表不含小写字母与符号，因此**它本身不满足 A-8 的「字母/数字/符号
// 至少两类」**——满不满足由 rule/passwd 判定，需要符号时该用 RandInt 自行采样。
func RandText() string {
	return rand.Text()
}
