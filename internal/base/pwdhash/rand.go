package pwdhash

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// RandBytes 返回 n 个密码学安全的随机字节。
//
// 底座是标准库 crypto/rand，不能用 math/rand：后者是可预测的伪随机数，
// 用于盐或初始密码会直接击穿 A-8 的密码策略。
// 同仓库 go_common/util 的 CreateRandString 正是基于 math/rand，故本包随机源自出、不复用它。
//
// n <= 0 时报错而不返回空切片：调用方可能把返回值直接当盐或密钥使用，
// 静默返回空切片会产出一条「盐为空」的合法凭据。
func RandBytes(ctx context.Context, n int) ([]byte, error) {
	if n <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n}).Error("生成随机字节，长度非正")
		return nil, errors.Errorf("生成随机字节，长度非正: %d", n)
	}
	data := make([]byte, n)
	//crypto/rand.Read 文档承诺「要么填满整个切片，要么不返回」，但仍按项目风格显式判错
	_, err := rand.Read(data)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n, "err": err}).Error("生成随机字节，异常")
		return nil, errors.Errorf("生成随机字节，异常: %+v", err)
	}
	return data, nil
}

// RandIndex 返回 [0, n) 内均匀分布的随机下标。
//
// 用 crypto/rand.Int 而非「随机字节 % n」：后者在 n 不是 2 的幂时有取模偏置，
// 低位下标被选中的概率偏高，会让初始密码的实际熵低于名义值。
// crypto/rand.Int 内部已做拒绝采样，天然无偏。
func RandIndex(ctx context.Context, n int) (int, error) {
	if n <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n}).Error("生成随机下标，上界非正")
		return 0, errors.Errorf("生成随机下标，上界非正: %d", n)
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n, "err": err}).Error("生成随机下标，异常")
		return 0, errors.Errorf("生成随机下标，异常: %+v", err)
	}
	//上界为 int 入参，结果必然在 int 范围内，Int64 转换不会溢出
	return int(index.Int64()), nil
}

// RandString 从 charset 中无偏采样 n 个字符，返回随机串。
//
// 承载口径（同文档 §六 base/pwdhash 行）：本函数是「安全随机串」的对外出口，
// 只供 rule/passwd 采样符合 A-8 策略的初始密码（A-2）。
//
// 字符集由调用方传入而不写死在本包：A-8 的「字母/数字/符号至少含两类」是业务策略，
// 按约定 5「base/ 是纯技术层，带业务语义的类型不得放入」应留在 rule/passwd，
// 本包只负责无偏采样这一件纯技术的事。
//
// 按 rune 而非 byte 采样，保证传入多字节字符集时不会截出半个 UTF-8 字符；
// 返回串的长度按 rune 计恰为 n。
func RandString(ctx context.Context, n int, charset string) (string, error) {
	if n <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n}).Error("生成随机串，长度非正")
		return "", errors.Errorf("生成随机串，长度非正: %d", n)
	}
	runes := []rune(charset)
	if len(runes) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n}).Error("生成随机串，字符集为空")
		return "", errors.Errorf("生成随机串，字符集为空")
	}

	data := make([]rune, n)
	for i := range data {
		index, err := RandIndex(ctx, len(runes))
		if err != nil {
			//随机源异常时不能退化成可预测取值，直接失败
			logrus.WithContext(ctx).WithFields(logrus.Fields{"n": n, "err": err}).Error("生成随机串，异常")
			return "", errors.Errorf("生成随机串，异常: %+v", err)
		}
		data[i] = runes[index]
	}
	return string(data), nil
}
