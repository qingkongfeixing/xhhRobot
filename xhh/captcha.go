package xhh

import (
	"sync/atomic"
	"time"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var xhhCaptchaCooldownUntil atomic.Int64 // 冷却结束的 unix 时间戳
const xhhCaptchaCooldown = 10 * time.Minute

var lastCooldownLog atomic.Int64 // 上次打印冷却日志的时间

func xhhCaptchaCooldownRemaining() time.Duration {
	until := time.Unix(xhhCaptchaCooldownUntil.Load(), 0)
	return time.Until(until)
}

func xhhCaptchaCoolingDown(endpoint string) bool {
	remaining := xhhCaptchaCooldownRemaining()
	if remaining <= 0 {
		return false
	}
	// 每 60 秒输出一次，避免刷屏
	now := time.Now().Unix()
	if now-lastCooldownLog.Load() >= 60 {
		lastCooldownLog.Store(now)
		loger.Loger.Warn("[XHH]小黑盒请求冷却中，跳过所有 API 调用",
			zap.String("触发端点", endpoint),
			zap.Duration("剩余冷却", remaining.Round(time.Second)),
		)
	}
	return true
}

func enterXHHCaptchaCooldown(trigger string) {
	until := time.Now().Add(xhhCaptchaCooldown).Unix()
	xhhCaptchaCooldownUntil.Store(until)
	loger.Loger.Warn("[XHH]检测到 show_captcha，进入全局冷却",
		zap.String("触发者", trigger),
		zap.Duration("冷却时长", xhhCaptchaCooldown),
	)
	loger.Loger.Warn("[XHH]冷却结束后将自动恢复 API 调用，无需重启进程")
}
