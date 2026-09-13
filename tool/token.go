package tool

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	TokenLen     = 24
	TokenMinLen  = 12
	tokenDigits  = "23456789"
	tokenLetters = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"
)

// 口令即数据库密钥，只能用crypto/rand；go_common的GenRandStr是math/rand且只有字母，不满足口令强度
func GenToken(ctx context.Context, length int) (string, error) {
	if length < TokenMinLen {
		length = TokenMinLen
	}
	runes := []rune(tokenLetters + tokenDigits)
	for {
		token := make([]rune, length)
		for i := range token {
			index, err := rand.Int(rand.Reader, big.NewInt(int64(len(runes))))
			if err != nil {
				logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("生成口令，随机数异常")
				return "", errors.Errorf("生成口令，随机数异常: %+v", err)
			}
			token[i] = runes[index.Int64()]
		}
		//随机结果可能全是字母，不满足强度就重来
		if CheckToken(ctx, string(token)) == nil {
			return string(token), nil
		}
	}
}

// 口令强度：长度不少于12，且不能是纯数字或纯字母
func CheckToken(ctx context.Context, token string) error {
	if len([]rune(token)) < TokenMinLen {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"len": len([]rune(token))}).Warn("校验口令，长度不足")
		return errors.Errorf("校验口令，长度不足%d位", TokenMinLen)
	}
	var hasDigit, hasOther bool
	for _, one := range token {
		if '0' <= one && one <= '9' {
			hasDigit = true
		} else {
			hasOther = true
		}
	}
	if !hasDigit || !hasOther {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("校验口令，不能为纯数字或纯字母")
		return errors.Errorf("校验口令，不能为纯数字或纯字母")
	}
	return nil
}
