package xhh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var lock = &sync.Mutex{}
var lastUnlockTime = time.Now()

// postComment 构造表单并发送评论到小黑盒，useFallback 为 true 时使用备用账号
func postComment(text, linkID, replyID, rootID, iscy string, useFallback bool) (status string, msg string, ok bool) {
	if xhhCaptchaCoolingDown("comment_create") {
		return "", "", false
	}

	form := url.Values{}
	form.Add("is_cy", iscy)
	form.Add("link_id", linkID)
	if replyID != "0" && replyID != "" {
		form.Add("reply_id", replyID)
	}
	if rootID != "0" && rootID != "" {
		form.Add("root_id", rootID)
	}
	form.Add("text", text)
	body := form.Encode()

	var resp *http.Response
	if useFallback {
		resp = SendReqWithFallback("POST", "/bbs/app/comment/create", bytes.NewReader([]byte(body)), "")
	} else {
		resp = SendReq("POST", "/bbs/app/comment/create", bytes.NewReader([]byte(body)), "")
	}
	if resp == nil {
		return "", "", false
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		loger.Loger.Error("[XHH]无法解析Body", zap.Error(err))
		return "", "", false
	}

	var resps struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(data, &resps); err != nil {
		loger.Loger.Error("[XHH]无法反序列化", zap.String("body", string(data)), zap.Error(err))
		return "", "", false
	}
	return resps.Status, resps.Msg, true
}

func Reply(text, link_id, reply_id, root_id, iscy string) (isok bool) {
	lock.Lock()
	lockHoldStart := time.Now()
	sinceLast := lockHoldStart.Sub(lastUnlockTime).Truncate(time.Millisecond)
	loger.Loger.Debug("[主]获得发送锁", zap.Duration("距上次解锁", sinceLast))

	// 【主账号冷却期检测】：如果主账号被限制评论，自动切换备用账号
	if isMainAccountCoolingDown() {
		if !IsFallbackAvailable() {
			loger.Loger.Warn("[主]主账号处于冷却期但备用账号不可用，放弃本次回复")
			lastUnlockTime = time.Now()
			lock.Unlock()
			loger.Loger.Warn("[主]发送失败(冷却期无备用账号)，释放锁", zap.Duration("持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
			return false
		}
		loger.Loger.Warn("[主]主账号处于冷却期，使用备用账号回复",
			zap.Duration("剩余冷却", mainAccountCooldownRemaining().Round(time.Second)))
		// 主锁继续持有以串行化回复，备用账号有自己的锁
		fallbackOk := ReplyWithFallback(text, link_id, reply_id, root_id, iscy)
		if !fallbackOk {
			loger.Loger.Error("[主]备用账号回复也失败")
			lastUnlockTime = time.Now()
			lock.Unlock()
			loger.Loger.Warn("[主]发送失败(备用账号失败)，释放锁", zap.Duration("持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
			return false
		}
		loger.Loger.Info("[主]备用账号回复成功", zap.Duration("锁持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
		return true // 锁由 WaitReplyInterval 释放
	}

	status, msg, ok := postComment(text, link_id, reply_id, root_id, iscy, false)
	if !ok {
		lastUnlockTime = time.Now()
		lock.Unlock()
		loger.Loger.Warn("[主]发送失败，释放锁", zap.Duration("持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
		return false
	}
	if status != "ok" {
		if msg == "评论已被删除" {
			loger.Loger.Debug("[主]评论已删除")
			return true // 锁由 WaitReplyInterval 释放
		}
		loger.Loger.Error("[XHH]评论发送失败", zap.String("status", status), zap.String("msg", msg))
		if isXHHCaptchaStatus(status) {
			enterXHHCaptchaCooldown("comment_create")
		}
		// 【主账号限制检测】：检测到账号被限制评论，进入冷却期并切换备用账号
		if isAccountRestricted(msg) {
			enterMainAccountCooldown(msg)
			// 如果备用账号可用，立即使用备用账号重试本次回复
			if IsFallbackAvailable() {
				loger.Loger.Warn("[主]主账号被限制，立即使用备用账号接替本次回复")
				fallbackOk := ReplyWithFallback(text, link_id, reply_id, root_id, iscy)
				if fallbackOk {
					loger.Loger.Info("[主]备用账号接替回复成功")
					return true // 锁由 WaitReplyInterval 释放
				}
				loger.Loger.Error("[主]备用账号接替回复也失败")
			}
		}
		lastUnlockTime = time.Now()
		lock.Unlock()
		loger.Loger.Warn("[主]发送失败，释放锁", zap.Duration("持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
		return false
	}
	loger.Loger.Info("[主]回复发送成功", zap.Duration("锁持有时间", time.Since(lockHoldStart).Truncate(time.Millisecond)))
	return true
}

// WaitReplyInterval 回复间隔等待（Reply()已持有锁，此处sleep后释放）
func WaitReplyInterval() {
	interval := time.Duration(config.ConfigStruct.Xhh.ReplyIntervalSeconds) * time.Second
	loger.Loger.Info("[主]间隔等待", zap.Duration("配置间隔", interval))
	time.Sleep(interval)
	lastUnlockTime = time.Now()
	lock.Unlock()
	loger.Loger.Debug("[主]释放发送锁")
}

// GenerateAtText 生成小黑盒专用的蓝字高亮 @ 标签
func GenerateAtText(uid int, userName string) string {
	// 组装小黑盒特有的 HTML 协议
	template := `<a data-user-id="%d" href="https://api.xiaoheihe.cn/open_inapp/#heybox://%%7B%%22protocol_type%%22%%3A%%22openUser%%22%%2C%%22user_id%%22%%3A%%22%d%%22%%7D" target="_blank">@%s</a> `
	return fmt.Sprintf(template, uid, uid, userName)
}