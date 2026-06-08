package xhh

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"xhhrobot/ai"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var fallbackLock = &sync.Mutex{}

// subCommentsResp matches the /bbs/app/comment/sub/comments API response
type subCommentsResp struct {
	Status string `json:"status"`
	Result struct {
		Comments []struct {
			CommentID int    `json:"commentid"`
			Text      string `json:"text"`
			User      struct {
				UserID   interface{} `json:"userid"`
				Username string      `json:"username"`
			} `json:"user"`
		} `json:"comments"`
		HasMore bool `json:"has_more"`
		Lastval int  `json:"lastval"`
	} `json:"result"`
}

// matchUserID checks whether the comment's user ID matches the expected numeric UID
func matchUserID(raw interface{}, uid int) bool {
	switch v := raw.(type) {
	case float64:
		return int(v) == uid
	case string:
		n, _ := strconv.Atoi(v)
		return n == uid
	case json.Number:
		n, _ := v.Int64()
		return int(n) == uid
	}
	return false
}

// CheckShadowReply uses the fallback account to verify whether the main account's
// reply is visible in the comment chain. Returns true if the reply is SHADOWED.
func CheckShadowReply(linkID int, rootID int, replyContent string) bool {
	if !IsFallbackAvailable() {
		return false
	}
	if replyContent == "" {
		return false
	}
	// 主账号冷却期间跳过影子检测：回复已是备用账号发出的，无需再检测
	if isMainAccountCoolingDown() {
		loger.Loger.Debug("[备]主账号冷却中，跳过影子检测")
		return false
	}

	fallbackLock.Lock()
	defer fallbackLock.Unlock()
	loger.Loger.Debug("[备]获得备用账号锁")

	delay := config.ConfigStruct.Fallback.CheckDelaySeconds
	loger.Loger.Info("[备]开始检测评论可见性，等待缓存刷新...", zap.Int("delay_s", delay))
	time.Sleep(time.Duration(delay) * time.Second)

	searchKey := extractSearchKey(replyContent)
	if len(searchKey) < 6 {
		// Fallback: use the full reply with HTML stripped, so we don't match
		// against raw HTML attributes (which would never match the API response)
		searchKey = stripHTMLTags(replyContent)
	}
	loger.Loger.Debug("[备]搜索关键词", zap.String("search_key", searchKey))

	comments := searchCommentGroup(linkID, rootID, true)
	if comments == nil {
		loger.Loger.Warn("[备]未找到目标楼层，检测结果不确定",
			zap.Int("link_id", linkID), zap.Int("root_id", rootID))
		return false
	}

	// 在子评论中搜索回复内容
	// 注意：API 返回的评论文本可能包含 HTML 标签（如 <a>），而 searchKey 已剥离 HTML，
	// 所以必须对 API 文本也做 HTML 剥离后再比较，否则 strings.Contains 会因标签中断而失败
	for i := 1; i < len(comments); i++ {
		apiText := stripHTMLTags(comments[i].Text)
		if strings.Contains(apiText, searchKey) {
			loger.Loger.Info("[备]评论可见，检测通过",
				zap.Int("link_id", linkID),
				zap.Int("root_id", rootID),
				zap.Int("found_comment_id", comments[i].CommentID))
			return false
		}
	}

	// 子评论可能分页，翻页继续查找
	if fetchMoreSubComments(linkID, rootID, searchKey) {
		loger.Loger.Info("[备]翻页找到评论，检测通过",
			zap.Int("link_id", linkID),
			zap.Int("root_id", rootID))
		return false
	}

	loger.Loger.Warn("[备]评论未找到，疑似影子评论！",
		zap.Int("link_id", linkID),
		zap.Int("root_id", rootID),
		zap.Int("children_count", len(comments)-1))
	return true
}

// fetchMoreSubComments paginates through sub-comments of a root comment via
// /bbs/app/comment/sub/comments, using lastval cursor pagination for up to 20 pages.
// Returns true if any sub-comment text contains the searchKey.
func fetchMoreSubComments(linkID int, rootCommentID int, searchKey string) bool {
	loger.Loger.Info("[备]开始子评论翻页",
		zap.Int("link_id", linkID),
		zap.Int("root_comment_id", rootCommentID))

	lastval := 0
	totalFetched := 0
	for page := 1; page <= 20; page++ {
		params := fmt.Sprintf("?root_comment_id=%d", rootCommentID)
		if lastval != 0 {
			params += "&lastval=" + strconv.Itoa(lastval)
		}

		resp := SendReqWithFallback("GET", "/bbs/app/comment/sub/comments", nil, params)
		if resp == nil {
			loger.Loger.Warn("[备]子评论翻页请求失败", zap.Int("page", page))
			return false
		}
		data, err := io.ReadAll(resp.Body)
		if resp.Body != nil {
			resp.Body.Close()
		}
		if err != nil {
			loger.Loger.Warn("[备]子评论翻页读取响应失败", zap.Int("page", page), zap.Error(err))
			return false
		}

		if strings.Contains(string(data), "captcha") || strings.Contains(string(data), "ticket") {
			loger.Loger.Warn("[备]子评论翻页触发风控验证码，停止翻页")
			return false
		}

		var result subCommentsResp
		if err := json.Unmarshal(data, &result); err != nil {
			loger.Loger.Warn("[备]子评论翻页解析失败",
				zap.Int("page", page),
				zap.Error(err),
				zap.String("raw", string(data)[:min(len(string(data)), 200)]))
			return false
		}
		if result.Status != "ok" {
			loger.Loger.Warn("[备]子评论翻页API返回非ok",
				zap.Int("page", page),
				zap.String("status", result.Status),
				zap.String("raw", string(data)[:min(len(string(data)), 200)]))
			return false
		}

		totalFetched += len(result.Result.Comments)
		loger.Loger.Info("[备]子评论翻页进度",
			zap.Int("page", page),
			zap.Int("page_count", len(result.Result.Comments)),
			zap.Int("total_fetched", totalFetched),
			zap.Bool("has_more", result.Result.HasMore),
			zap.Int("lastval", result.Result.Lastval))

		for _, c := range result.Result.Comments {
			apiText := stripHTMLTags(c.Text)
			if strings.Contains(apiText, searchKey) {
				loger.Loger.Info("[备]子评论翻页找到目标评论",
					zap.Int("comment_id", c.CommentID),
					zap.Int("page", page),
					zap.Int("total_fetched", totalFetched))
				return true
			}
		}

		if !result.Result.HasMore {
			loger.Loger.Info("[备]子评论翻页到底，未找到目标",
				zap.Int("total_pages", page),
				zap.Int("total_fetched", totalFetched))
			break
		}
		lastval = result.Result.Lastval
		time.Sleep(time.Millisecond * 300)
	}
	return false
}

func TestFetchMoreSubComments(linkID int, rootCommentID int, searchKey string) {
	found := fetchMoreSubComments(linkID, rootCommentID, searchKey)
	loger.Loger.Info("[备]手动测试完成", zap.Bool("found", found),
		zap.Int("link_id", linkID),
		zap.Int("root_comment_id", rootCommentID))
}

// stripHTMLTags removes all content between < and > (inclusive) from a string.
// This properly handles nested tags, attributes, and self-closing tags.
func stripHTMLTags(s string) string {
	var buf strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// extractSearchKey extracts a clean, matchable search key from the reply content.
// It strips all HTML tags, trims leading @-mentions, and returns up to 30 characters
// of the remaining text. The result is used to search for the main account's reply
// in the comment chain via substring matching.
func extractSearchKey(text string) string {
	// 1. Strip all HTML tags properly (the old approach of pre-stripping "<a " and "</a>"
	//    left attribute text behind, producing garbage search keys like "data-user-id=...")
	clean := stripHTMLTags(text)

	// 2. Trim whitespace
	s := strings.TrimSpace(clean)

	// 3. Strip leading @-mentions (e.g. "@雏草姬 ", "@帖主 ", "@someone ")
	//    Keep stripping as long as the string starts with @ followed by text
	for strings.HasPrefix(s, "@") {
		idx := strings.IndexAny(s, " ,，。！？!?\n\t")
		if idx > 0 {
			s = strings.TrimSpace(s[idx+1:])
		} else {
			// Entire remaining string is an @-mention, nothing left to search
			s = ""
			break
		}
	}

	// 4. Take first 30 runes of the meaningful content as the search key
	runes := []rune(s)
	if len(runes) > 30 {
		return string(runes[:30])
	}
	return string(runes)
}

// ReplyWithFallback posts a reply using the fallback account
func ReplyWithFallback(text, linkID, replyID, rootID, iscy string) bool {
	if !IsFallbackAvailable() {
		loger.Loger.Warn("[备]备用账号不可用，无法接替回复")
		return false
	}

	// 备用账号也在冷却中，直接放弃
	if isFallbackAccountCoolingDown() {
		loger.Loger.Warn("[备]备用账号处于冷却期，跳过回复", zap.Duration("剩余冷却", fallbackAccountCooldownRemaining().Round(time.Second)))
		return false
	}

	fallbackLock.Lock()
	defer fallbackLock.Unlock()
	loger.Loger.Debug("[备]获得备用账号锁(接替回复)")

	status, msg, ok := postComment(text, linkID, replyID, rootID, iscy, true)
	if !ok {
		loger.Loger.Error("[备]备用账号发送评论失败")
		return false
	}
	if status != "ok" {
		loger.Loger.Error("[备]备用账号回复失败", zap.String("status", status), zap.String("msg", msg))
		// 【备号限制检测】：备号也被限制时进入冷却
		if isAccountRestricted(msg) {
			enterFallbackAccountCooldown(msg)
		}
		return false
	}

	loger.Loger.Info("[备]备用账号接替回复成功！")
	return true
}

// regenerateWithFallbackPrompt generates a new reply using the fallback persona prompt,
// then applies @-placeholder replacement. Auto-detects edge cases consistent with main logic.
func regenerateWithFallbackPrompt(
	Contents []ai.Content, callerText string, top []ai.Topics, tags []ai.Tags,
	callerUID int, authorID int, rootText string, rootImgUrl string,
	originalFinalText string, targetUID int, targetName string,
) string {
	loger.Loger.Info("[备]使用备用提示词重新生成回复...")
	result := ai.GetAiReplyWithPrompt(Contents, callerText, top, tags, callerUID, authorID, rootText, rootImgUrl, config.ConfigStruct.Fallback.Prompt)
	replyText := result.Text
	if replyText == "" {
		loger.Loger.Warn("[备]备用提示词生成失败，降级使用主账号回复")
		return originalFinalText
	}

	finalText := replyText
	if authorID != 0 {
		authorAt := GenerateAtText(authorID, "楼主")
		finalText = strings.ReplaceAll(finalText, "@帖主 ", authorAt)
		finalText = strings.ReplaceAll(finalText, "@帖主", authorAt)
	}
	if targetUID == callerUID || targetUID == 0 {
		finalText = strings.ReplaceAll(finalText, "@召唤者 ", "")
		finalText = strings.ReplaceAll(finalText, "@召唤者", "")
		finalText = strings.ReplaceAll(finalText, "@层主 ", "")
		finalText = strings.ReplaceAll(finalText, "@层主", "")
		finalText = strings.ReplaceAll(finalText, "@主人 ", "")
		finalText = strings.ReplaceAll(finalText, "@主人", "")
	} else {
		realCallerAt := GenerateAtText(callerUID, "雏草姬")
		finalText = strings.ReplaceAll(finalText, "@召唤者", realCallerAt)
		finalText = strings.ReplaceAll(finalText, "@主人", realCallerAt)
		if targetUID != 0 && targetName != "" {
			if strings.Contains(finalText, "@层主") {
				targetAt := GenerateAtText(targetUID, targetName)
				finalText = strings.ReplaceAll(finalText, "@层主", targetAt)
			}
		}
		finalText = strings.ReplaceAll(finalText, "@层主 ", "")
		finalText = strings.ReplaceAll(finalText, "@层主", "")
	}

	bannedMap := config.ConfigStruct.Xhh.BannedWords
	if len(bannedMap) > 0 {
		for badWord, goodWord := range bannedMap {
			badWord = strings.TrimSpace(badWord)
			if badWord != "" && strings.Contains(finalText, badWord) {
				finalText = strings.ReplaceAll(finalText, badWord, goodWord)
			}
		}
	}

	return finalText
}
