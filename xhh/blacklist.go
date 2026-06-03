package xhh

import (
	"strconv"
	"strings"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var BlacklistedUIDs []int
var blacklistLoaded bool

// IsBlacklisted checks whether a given UID is in the blacklist.
// Returns true if the user should be ignored (not replied to).
func IsBlacklisted(uid int) bool {
	if !blacklistLoaded {
		cfg := config.ConfigStruct.Xhh
		if cfg.Blacklist != "" {
			parts := strings.Split(cfg.Blacklist, ",")
			for _, v := range parts {
				v = strings.TrimSpace(v)
				if v != "" {
					i, err := strconv.Atoi(v)
					if err != nil {
						loger.Loger.Error("[XHH]黑名单配置->" + v + "<-似乎并非数字，已跳过")
						continue
					}
					BlacklistedUIDs = append(BlacklistedUIDs, i)
				}
			}
		}
		blacklistLoaded = true
		if len(BlacklistedUIDs) > 0 {
			loger.Loger.Info("[XHH]黑名单已加载", zap.Int("count", len(BlacklistedUIDs)))
		}
	}

	for _, v := range BlacklistedUIDs {
		if v == uid {
			return true
		}
	}
	return false
}

// ReloadBlacklist clears the cached blacklist so it will be re-parsed from config.
// Call this after config hot-reload.
func ReloadBlacklist() {
	BlacklistedUIDs = nil
	blacklistLoaded = false
}
