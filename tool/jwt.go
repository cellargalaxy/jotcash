package tool

import (
	"context"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func EnJwt(ctx context.Context, serverToken, clientToken string, expire time.Duration) (string, error) {
	if serverToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("签发jwt，后端口令为空")
		return "", errors.Errorf("签发jwt，后端口令为空")
	}

	now := time.Now()
	var claims model.Claims
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(expire).Unix()
	claims.Ip = util.GetIP()
	claims.ServerName = util.GetServerName()
	claims.LogId = util.GetLogId(ctx)
	claims.ClientToken = clientToken
	return util.EnJwt(ctx, serverToken, claims)
}

func DeJwt(ctx context.Context, serverToken, token string) (*model.Claims, error) {
	if serverToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("校验jwt，后端口令为空")
		return nil, errors.Errorf("校验jwt，后端口令为空")
	}

	var claims model.Claims
	jwtToken, err := util.DeJwt(ctx, token, serverToken, &claims)
	if err != nil {
		return nil, err
	}
	if jwtToken == nil || !jwtToken.Valid {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("校验jwt，非法")
		return nil, errors.Errorf("校验jwt，非法")
	}
	return &claims, nil
}
