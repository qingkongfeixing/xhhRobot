package xhh

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"xhhrobot/ai"
	"xhhrobot/config"
	"xhhrobot/db"
	"xhhrobot/loger"

	"go.uber.org/zap"
	"fmt"
	"runtime/debug"
	"unicode/utf8"
)

type feedResponse struct {
	Status string `json:"status"`
	Result struct {
		Links []struct {
			LinkID      int              `json:"linkid"`
			Title       string           `json:"title"`
			Description string           `json:"description"`
			Topics      []ai.Topics      `json:"topics"`
			Tags        []ai.Tags        `json:"hashtags"`
			User        struct {
				UserID json.RawMessage `json:"userid"`
			} `json:"user"`
		} `json:"links"`
	} `json:"result"`
}

type feedLink struct {
	LinkID      int
	Title       string
	Description string
	Topics      []ai.Topics
	Tags        []ai.Tags
}

func rawToInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(string(raw), "\"")
	n, _ := strconv.Atoi(s)
	return n
}

func AutoFeedReply() {
	time.Sleep(60 * time.Second) // 启动后等待一分钟再开始首次刷帖
	for {
		processFeedReplyOnce(false)
		interval := config.ConfigStruct.FeedReply.Interval
		if interval <= 0 {
			interval = 900
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func TriggerFeedReplyTest() {
	fmt.Println("\n🧪 [FeedReply] ========== 手动触发刷帖测试 ==========")
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ [FeedReply] 手动测试发生 panic: %v\n%s\n", r, string(debug.Stack()))
		}
	}()
	cfg := &config.ConfigStruct.FeedReply
	saved := cfg.Enabled
	cfg.Enabled = true
	fmt.Printf("📋 [FeedReply] 当前配置: maxPerRun=%d, maxPerDay=%d, dryRun=%v\n", cfg.MaxPerRun, cfg.MaxPerDay, cfg.DryRun)
	processFeedReplyOnce(true)
	cfg.Enabled = saved
	fmt.Println("✅ [FeedReply] ========== 手动测试结束 ==========\n")
}

func fetchFeedLinks() ([]feedLink, bool) {
	if xhhCaptchaCoolingDown("feeds") {
		return nil, false
	}

	resp := SendReq("GET", "/bbs/app/feeds", nil, "?pull=1")
	if resp == nil {
		fmt.Println("❌ [FeedReply] 拉取首页帖子列表失败，请检查登录状态")
		return nil, false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var parsed feedResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		fmt.Printf("❌ [FeedReply] 首页JSON解析失败: %v\n", err)
		return nil, false
	}
	if parsed.Status != "ok" {
		fmt.Printf("❌ [FeedReply] 首页API返回异常 status=%s\n", parsed.Status)
		if isXHHCaptchaStatus(parsed.Status) {
			enterXHHCaptchaCooldown("feeds")
		}
		return nil, false
	}

	links := make([]feedLink, 0, len(parsed.Result.Links))
	for _, l := range parsed.Result.Links {
		links = append(links, feedLink{
			LinkID:      l.LinkID,
			Title:       l.Title,
			Description: l.Description,
			Topics:      l.Topics,
			Tags:        l.Tags,
		})
	}
	return links, true
}

func fallbackFeedContents(link feedLink) []ai.Content {
	text := link.Title
	if strings.TrimSpace(link.Description) != "" {
		text += "\n" + link.Description
	}
	return []ai.Content{{Type: "text", Text: text}}
}

func extractTextContent(contents []ai.Content) string {
	var parts []string
	for _, c := range contents {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func processFeedReplyOnce(skipTimeCheck bool) {
	cfg := config.ConfigStruct.FeedReply
	if !cfg.Enabled {
		loger.Loger.Info("[FeedReply]自动刷帖未开启，跳过")
		return
	}

	// 时间窗口检查：仅在 startHour ~ endHour 之间运行（手动测试跳过）
	if !skipTimeCheck {
		nowHour := time.Now().Hour()
		if cfg.StartHour > 0 || cfg.EndHour > 0 {
			inside := false
			if cfg.StartHour < cfg.EndHour {
				inside = nowHour >= cfg.StartHour && nowHour < cfg.EndHour
			} else {
				// 跨天，如 23~8
				inside = nowHour >= cfg.StartHour || nowHour < cfg.EndHour
			}
			if !inside {
				return
			}
		}
	}

	since := time.Now().Add(-24 * time.Hour).Unix()
	usedToday := db.FeedReplyAttemptsSince(since)
	if cfg.MaxPerDay > 0 && usedToday >= cfg.MaxPerDay {
		loger.Loger.Info("[FeedReply]今日自动刷帖额度已用完", zap.Int("max_per_day", cfg.MaxPerDay))
		return
	}

	links, ok := fetchFeedLinks()
	if !ok {
		return
	}
	fmt.Printf("📋 [FeedReply] 拉取到 %d 条首页帖子\n", len(links))

	processed := 0
	for _, link := range links {
		if processed >= cfg.MaxPerRun {
			break
		}
		if db.FeedReplyRecordExists(int64(link.LinkID)) {
			fmt.Printf("⏭ [FeedReply] 帖子 %d 已回复过，跳过\n", link.LinkID)
			continue
		}

		Contents, top, tags, authorID, postTitle := GetLinkInfo(link.LinkID)
		if len(Contents) == 0 {
			// 第三层：降级兜底，用 Feed 列表的标题和摘要
			fmt.Printf("⏭ [FeedReply] 帖子 %d 详情获取失败，使用 Feed 摘要降级回复\n", link.LinkID)
			Contents = fallbackFeedContents(link)
			top = link.Topics
			tags = link.Tags
			postTitle = link.Title
		}
		fmt.Printf("📝 [FeedReply] 正在处理帖子 #%d: %s\n", link.LinkID, postTitle)

		// 帖子字数过滤器：根据配置跳过字数不符合要求的帖子
		if cfg.MinPostWords > 0 || cfg.MaxPostWords > 0 {
			postText := extractTextContent(Contents)
			wordCount := utf8.RuneCountInString(postText)
			if cfg.MinPostWords > 0 && wordCount < cfg.MinPostWords {
				fmt.Printf("⏭ [FeedReply] 帖子 %d 字数(%d)低于最低要求(%d)，跳过\n", link.LinkID, wordCount, cfg.MinPostWords)
				db.SaveFeedReplyRecord(db.FeedReplyRecord{
					LinkID:       int64(link.LinkID),
					Title:        postTitle,
					PostContent:  postText,
					Status:       "skipped",
					ReplyContent: fmt.Sprintf("[字数过滤] 字数%d < 最低%d", wordCount, cfg.MinPostWords),
					RepliedAt:    time.Now().Unix(),
				})
				continue
			}
			if cfg.MaxPostWords > 0 && wordCount > cfg.MaxPostWords {
				fmt.Printf("⏭ [FeedReply] 帖子 %d 字数(%d)超过最高限制(%d)，跳过\n", link.LinkID, wordCount, cfg.MaxPostWords)
				db.SaveFeedReplyRecord(db.FeedReplyRecord{
					LinkID:       int64(link.LinkID),
					Title:        postTitle,
					PostContent:  postText,
					Status:       "skipped",
					ReplyContent: fmt.Sprintf("[字数过滤] 字数%d > 最高%d", wordCount, cfg.MaxPostWords),
					RepliedAt:    time.Now().Unix(),
				})
				continue
			}
		}

		result := ai.GetAiReply(Contents, "", top, tags, 0, authorID, "【自动刷帖】", "")
			replyText, mainTokens, visionTokens := result.Text, result.MainTokens, result.VisionTokens

		upperReply := strings.ToUpper(strings.Trim(strings.TrimSpace(replyText), " 。.!！?？`\"'"))
		status := "sent"

		if upperReply == "SKIP" || upperReply == "跳过" {
			status = "skipped"
			loger.Loger.Info("[FeedReply]AI判断跳过帖子", zap.Int("link_id", link.LinkID), zap.String("title", postTitle))
		} else if replyText != "" {
			if cfg.DryRun {
				status = "dry_run"
				loger.Loger.Info("[FeedReply]试运行生成回复", zap.Int("link_id", link.LinkID), zap.String("reply", replyText))
			} else {
				// 主账号冷却期间，如果配置了备用提示词，使用备用人格重新生成回复
				if isMainAccountCoolingDown() && IsFallbackAvailable() && config.ConfigStruct.Fallback.Prompt != "" {
					fallbackResult := ai.GetAiReplyWithPrompt(Contents, "", top, tags, 0, authorID, "【自动刷帖】", "", config.ConfigStruct.Fallback.Prompt)
					if fallbackResult.Text != "" {
						replyText = fallbackResult.Text
						mainTokens = fallbackResult.MainTokens
						visionTokens = fallbackResult.VisionTokens
						loger.Loger.Info("[FeedReply]冷却期使用备用提示词重新生成回复", zap.Int("link_id", link.LinkID))
					}
				}
				isok := Reply(replyText, strconv.Itoa(link.LinkID), "0", "0", "0")
				if !isok {
					status = "failed"
					loger.Loger.Warn("[FeedReply]评论发送失败", zap.Int("link_id", link.LinkID))
				} else {
					loger.Loger.Info("[FeedReply]评论发送成功", zap.Int("link_id", link.LinkID), zap.String("reply", replyText))
					db.Replyed(0, replyText, mainTokens, visionTokens)
				WaitReplyInterval()
				}
			}
			processed++
		}

		db.SaveFeedReplyRecord(db.FeedReplyRecord{
			LinkID:       int64(link.LinkID),
			Title:        postTitle,
			PostContent:  extractTextContent(Contents),
			Status:       status,
			ReplyContent: replyText,
			RepliedAt:    time.Now().Unix(),
			MainTokens:   mainTokens,
			VisionTokens: visionTokens,
		})
	}
}
