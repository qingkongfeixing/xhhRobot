package xhh

import (
	"encoding/json"
	"os"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var FallbackInfo struct {
	Cookie   string `json:"cookie"`
	HeyBoxId string `json:"heyboxId"`
	Time     int    `json:"time"`
}

var fallbackAvailable bool

func InitFallback() {
	if !config.ConfigStruct.Fallback.Enabled {
		loger.Loger.Info("[XHH]备用账号未启用，跳过加载")
		return
	}
	file, err := os.ReadFile(config.ConfigStruct.Fallback.CookieFile)
	if err != nil {
		loger.Loger.Warn("[XHH]未检测到备用账号Cookie，影子检测功能不可用", zap.String("file", config.ConfigStruct.Fallback.CookieFile))
		return
	}
	if err := json.Unmarshal(file, &FallbackInfo); err != nil {
		loger.Loger.Warn("[XHH]备用账号Cookie解析失败", zap.Error(err))
		return
	}
	if FallbackInfo.Cookie == "" {
		loger.Loger.Warn("[XHH]备用账号Cookie为空")
		return
	}
	fallbackAvailable = true
	loger.Loger.Info("[XHH]备用账号Cookie加载成功，影子检测已就绪", zap.String("heybox_id", FallbackInfo.HeyBoxId))
}

func IsFallbackAvailable() bool {
	return fallbackAvailable && config.ConfigStruct.Fallback.Enabled
}
