package handler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

var validateBan = new(tokenBan)

type tokenBan struct {
	lock     sync.Mutex
	count    int
	expireAt time.Time
}

func (this *tokenBan) check(now time.Time, limit int) bool {
	this.lock.Lock()
	defer this.lock.Unlock()

	return limit <= this.count && now.Before(this.expireAt)
}

func (this *tokenBan) fail(now time.Time, duration time.Duration) int {
	this.lock.Lock()
	defer this.lock.Unlock()

	if !now.Before(this.expireAt) {
		this.count = 0
	}
	this.count++
	this.expireAt = now.Add(duration)
	return this.count
}

func checkTokenBan(c *gin.Context) bool {
	ctx := c.Request.Context()

	object := config.GetConfig(ctx)
	if !validateBan.check(time.Now(), object.TokenFailLimit) {
		return false
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("口令校验，封禁中")
	c.Abort()
	c.JSON(http.StatusOK, util.NewHttpResp(http.StatusTooManyRequests, fmt.Sprintf("口令校验失败过多，请%d分钟后重试", object.TokenBanMinute), nil))
	return true
}

func checkTokenFail(c *gin.Context) {
	ctx := c.Request.Context()

	if util.GetClaims[*model.Claims](ctx) != nil {
		return
	}
	object := config.GetConfig(ctx)
	count := validateBan.fail(time.Now(), time.Duration(object.TokenBanMinute)*time.Minute)
	if count < object.TokenFailLimit {
		return
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"count": count}).Warn("口令校验，失败过多已封禁")
}
