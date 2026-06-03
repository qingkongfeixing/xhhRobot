package xhh

import (
	"strings"
	"sync/atomic"
	"time"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var mainAccountCooldownUntil atomic.Int64 // 主账号冷却结束的 unix 时间戳

func mainAccountCooldownRemaining() time.Duration {
	until := time.Unix(mainAccountCooldownUntil.Load(), 0)
	return time.Until(until)
}

func isMainAccountCoolingDown() bool {
	return mainAccountCooldownRemaining() > 0
}

func enterMainAccountCooldown(reason string) {
	minutes := config.ConfigStruct.Fallback.MainCooldownMinutes
	if minutes <= 0 {
		minutes = 360 // 默认 6 小时
	}
	duration := time.Duration(minutes) * time.Minute
	until := time.Now().Add(duration).Unix()
	mainAccountCooldownUntil.Store(until)
	loger.Loger.Warn("[主]主账号已被限制评论，进入冷却期并切换备用账号",
		zap.String("原因", reason),
		zap.Duration("冷却时长", duration),
		zap.Time("冷却结束", time.Unix(until, 0)))
}

// isAccountRestricted 检测 API 返回的 msg 是否表示账号被限制评论
func isAccountRestricted(msg string) bool {
	if msg == "" {
		return false
	}
	keywords := []string{
		"无法使用该功能",
		"账号异常",
		"账号被封禁",
		"评论功能被限制",
		"账号已被禁言",
	}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
