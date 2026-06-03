package xhh

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"xhhrobot/ai"
	"xhhrobot/config"
	"xhhrobot/db"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

var Info struct {
	Cookie   string `json:"cookie"`
	HeyBoxId string `json:"heyboxId"`
	Time     int    `json:"time"`
}
var CheckTime int
var ReplyTime int

var afkUntil time.Time

func Init() {
	file, err := os.ReadFile("./cookie.json")
	if err != nil {
		loger.Loger.Info("[XHH]未检测到Cookie")
		return
	}
	CheckTime = config.ConfigStruct.Xhh.CheckTime
	ReplyTime = config.ConfigStruct.Xhh.ReplyTime
	if CheckTime == 0 {
		loger.Loger.Warn("[XHH]您的设置中未设置检查时间，已默认为30s")
		CheckTime = 30
	}
	if ReplyTime == 0 {
		loger.Loger.Warn("[XHH]您的设置中未设置回复间隔，已默认为10s")
		ReplyTime = 10
	}
	if config.ConfigStruct.Xhh.ReplyIntervalSeconds == 0 {
		loger.Loger.Warn("[XHH]您的设置中未设置回复间隔，已默认为15秒")
		config.ConfigStruct.Xhh.ReplyIntervalSeconds = 15
	}
	json.Unmarshal(file, &Info)
	InitFallback()
}

type Msg struct {
	CommentID     int    `json:"comment_a_id"`
	CommentText   string `json:"comment_a_text"`
	MsgID         int    `json:"message_id"`
	RootCommentID int    `json:"root_comment_id"`
	LinkID        int    `json:"linkid"`
	UserID        int    `json:"userid_a"`

	// 【新增】：用于捕获发帖召唤时藏起来的帖子数据
	Link struct {
		LinkID int    `json:"linkid"`
		Text   string `json:"text"`
	} `json:"link"`

	// 兼容 user_a / user 两种结构，并尽可能捕获昵称字段
	UserA struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Name     string `json:"name"`
	} `json:"user_a"`
	User struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Name     string `json:"name"`
	} `json:"user"`
	Nickname string `json:"nickname"`
	Username string `json:"username"`
}

type Respo struct {
	Msg    string `json:"msg"`
	Result struct {
		Messages []Msg `json:"messages"`
	} `json:"result"`
	Stat    string `json:"stat"`
	Version string `json:"version"`
}

var DontReply bool

// IsReady 检查核心配置是否就绪，未就绪时静默等待，避免刷屏报错
func IsReady() bool {
	if Info.Cookie == "" || config.ConfigStruct.Xhh.BaseUrl == "" || config.ConfigStruct.Xhh.Ver == "" {
		return false
	}
	if !db.IsInited() {
		return false
	}
	return true
}

func CheckAt() {
	for {
		if !IsReady() {
			time.Sleep(10 * time.Second)
			continue
		}
		var offset int
		nomore := "false"
		other := fmt.Sprintf("?message_type=16&offset=%v&limit=20&no_more=%s", offset, nomore)
		resp := SendReq("GET", "/bbs/app/user/message", nil, other)

		if resp == nil {
			loger.Loger.Error("[XHH]检查消息时链接发送失败，等待重试")
			time.Sleep(time.Duration(CheckTime) * time.Second)
			continue
		}

		var data Respo
		Dbyte, err := io.ReadAll(resp.Body)
		if resp.Body != nil {
			resp.Body.Close()
		}

		if err != nil {
			loger.Loger.Error("[XHH]无法读取Body", zap.Error(err))
			time.Sleep(time.Duration(CheckTime) * time.Second)
			continue
		}

		err = json.Unmarshal(Dbyte, &data)
		if err != nil {
			loger.Loger.Error("[XHH]无法反序列化", zap.Error(err))
			time.Sleep(time.Duration(CheckTime) * time.Second)
			continue
		}

		for _, v := range data.Result.Messages {
			// =============== 【核心修复开始】 ===============
			// 如果是发帖召唤，顶层的 linkid 为 0，我们从嵌套对象里把真正的帖子ID抢救回来！
			if v.LinkID == 0 && v.Link.LinkID != 0 {
				v.LinkID = v.Link.LinkID
			}
			if v.CommentText == "" && v.Link.Text != "" {
				v.CommentText = v.Link.Text
			}
			// =============== 【核心修复结束】 ===============
			// 【终极修复】：地毯式搜索用户的名字，只要有就抓出来
			name := v.UserA.Nickname
			if name == "" {
				name = v.UserA.Username
			}
			if name == "" {
				name = v.User.Nickname
			}
			if name == "" {
				name = v.User.Username
			}

			if IsBlacklisted(v.UserID) {
				loger.Loger.Info("[XHH]黑名单用户已被自动忽略", zap.Int("UID", v.UserID), zap.String("name", name))
				db.Insert(v.MsgID, v.CommentID, v.RootCommentID, v.LinkID, v.UserID, name, v.CommentText, true)
			} else if DontReply {
				db.Insert(v.MsgID, v.CommentID, v.RootCommentID, v.LinkID, v.UserID, name, v.CommentText, true)
			} else {
				db.Insert(v.MsgID, v.CommentID, v.RootCommentID, v.LinkID, v.UserID, name, v.CommentText, false)
			}
		}

		DontReply = false

		jitter := time.Duration(rand.Intn(6)) * time.Second
		time.Sleep(time.Duration(CheckTime)*time.Second + jitter)
	}
}

func AutoReply() {
	for {
		if !IsReady() {
			time.Sleep(10 * time.Second)
			continue
		}

		// 【核心修改】：传入配置好的主人 UID 列表，以便在数据库直接置顶主人的消息
		Arr := db.GetComm(config.ConfigStruct.Xhh.Owner)
		if len(Arr) == 0 {
			time.Sleep(time.Duration(ReplyTime) * time.Second)
			continue
		}

		currentHour := time.Now().Hour()
		sh := config.ConfigStruct.Xhh.ReplyStartHour
		eh := config.ConfigStruct.Xhh.ReplyEndHour
		inReplyWindow := true // 默认全天回复
		if sh > 0 || eh > 0 {
			if sh < eh {
				inReplyWindow = currentHour >= sh && currentHour < eh
			} else {
				// 跨天，如 23~8：23点之后或8点之前
				inReplyWindow = currentHour >= sh || currentHour < eh
			}
		}
		isAfk := time.Now().Before(afkUntil)

		var wg sync.WaitGroup
		wg.Add(len(Arr))
		for _, v := range Arr {
			go func(v db.CommStruct) {
				defer wg.Done()
				// 【核心修复】：直接干掉了 if v.CommentID != 0 { 的限制！彻底释放发帖召唤能力！
				var isok bool
				isOwner := Check(v.Uid)

				// 先判断时间/AFK窗口
				if !inReplyWindow || isAfk {
					if isOwner {
						loger.Loger.Info("[XHH]塔菲正在睡觉/摸鱼，但被主人强行叫醒，VIP通道秒回！", zap.Int("UID", v.Uid))
					} else {
						return // 路人消息留在队列，等窗口开放后再处理
					}
				}

				Contents, top, tags, authorID, postTitle := GetLinkInfo(v.LinkID)
				// 【恢复】：把拿到的标题存入数据库，给前端用！
				if postTitle != "" {
					db.UpdateLinkTitle(v.LinkID, postTitle)
				}
				if len(Contents) == 0 {
					loger.Loger.Warn("[XHH]帖子内容无法解析(可能是视频帖)，降级为仅根据评论上下文回复")
				}

				// 1. 获取层主的上下文和图片
				RootText, rootImgUrl, targetUID, targetName := GetRootComment(v.LinkID, v.RootID)

				// =======================================================
				fmt.Printf("\n\033[1;36m============= 收到新的召唤 =============\033[0m\n")
				fmt.Printf("🔗 帖子链接: https://www.xiaoheihe.cn/community/1/list/%d\n", v.LinkID)
				fmt.Printf("🗣️ 召唤者说: %s\n", v.Text)
				if targetName != "" {
					fmt.Printf("👤 锁定层主: %s\n", targetName)
				}
				if RootText != "" {
					fmt.Printf("💬 层主原话: %s\n", RootText)
				}
				if rootImgUrl != "" {
					fmt.Printf("🖼️ 附带图片: [已获取]\n")
				}
				fmt.Printf("\033[1;36m========================================\033[0m\n")

				if v.RootID != 0 && targetUID == 0 {
					loger.Loger.Warn("[XHH]找不到目标评论(可能被系统吞图或删除)，已直接丢弃！", zap.Int("RootID", v.RootID))
					db.Replyed(v.MsgID, "【系统提示：该评论被吞或删除，已跳过处理】", 0, 0)
					return
				}
				// =======================================================

				if targetUID == v.Uid {
					RootText = "【单人模式】"
				}

				// 2. 将图文数据完整传给 AI！
				result := ai.GetAiReply(Contents, v.Text, top, tags, v.Uid, authorID, RootText, rootImgUrl)
					ReplyText, mainTokens, visionTokens := result.Text, result.MainTokens, result.VisionTokens
				if ReplyText == "" {
					loger.Loger.Warn("[XHH]Ai返回了空结果，丢弃任务避免死循环！")
					db.Replyed(v.MsgID, "【系统提示：AI大模型返回空结果，已跳过处理】", 0, 0)
					return
				}

				// =============== 【替换开始】 ===============
				var finalText = ReplyText

				if authorID != 0 {
					authorAt := GenerateAtText(authorID, "楼主")
					finalText = strings.ReplaceAll(finalText, "@帖主 ", authorAt)
					finalText = strings.ReplaceAll(finalText, "@帖主", authorAt)
				}

				if targetUID == v.Uid || targetUID == 0 {
					loger.Loger.Info("[XHH]=> 触发【单聊直回模式】，省略多余的艾特蓝字")
					finalText = strings.ReplaceAll(finalText, "@召唤者 ", "")
					finalText = strings.ReplaceAll(finalText, "@召唤者", "")
					finalText = strings.ReplaceAll(finalText, "@层主 ", "")
					finalText = strings.ReplaceAll(finalText, "@层主", "")
					finalText = strings.ReplaceAll(finalText, "@主人 ", "")
					finalText = strings.ReplaceAll(finalText, "@主人", "")
				} else {
					realCallerAt := GenerateAtText(v.Uid, "雏草姬")
					finalText = strings.ReplaceAll(finalText, "@召唤者", realCallerAt)
					finalText = strings.ReplaceAll(finalText, "@主人", realCallerAt)

					if targetUID != 0 && targetName != "" {
						if strings.Contains(finalText, "@层主") {
							loger.Loger.Info("[XHH]=> 触发【智能多目标艾特模式】")
							targetAt := GenerateAtText(targetUID, targetName)
							finalText = strings.ReplaceAll(finalText, "@层主", targetAt)
						} else {
							loger.Loger.Info("[XHH]=> AI判断话题与层主无关，跳过艾特层主")
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
							loger.Loger.Info("[XHH]=> 触发防封词库替换", zap.String("原词", badWord), zap.String("替换为", goodWord))
							finalText = strings.ReplaceAll(finalText, badWord, goodWord)
						}
					}
				}

				typingTime := len([]rune(finalText)) / 10
				if typingTime < 3 {
					typingTime = 3
				}
				if typingTime > 12 {
					typingTime = 12
				}

				realDelay := time.Duration(typingTime + rand.Intn(4)) * time.Second
				loger.Loger.Info("[XHH]模拟人类打字中...", zap.Duration("预计耗时", realDelay))
				time.Sleep(realDelay)

				// 发送回复：对于帖子，v.CommentID 是 "0"，正好是以楼层形式回复！
				// 主账号冷却期间，如果配置了备用提示词，使用备用人格重新生成回复
				if isMainAccountCoolingDown() && IsFallbackAvailable() && config.ConfigStruct.Fallback.Prompt != "" {
					fallbackText := regenerateWithFallbackPrompt(
						Contents, v.Text, top, tags, v.Uid, authorID,
						RootText, rootImgUrl, finalText, targetUID, targetName)
					if fallbackText != "" {
						finalText = fallbackText
						loger.Loger.Info("[主]冷却期使用备用提示词重新生成回复")
					}
				}
				isok = Reply(finalText, strconv.Itoa(v.LinkID), strconv.Itoa(v.CommentID), strconv.Itoa(v.RootID), "")

				if isok {
					db.Replyed(v.MsgID, finalText, mainTokens, visionTokens)
					WaitReplyInterval()

					// 影子评论检测 + 备用账号接替回复（异步，不阻塞下一批）
					// 主账号冷却期间跳过影子检测：回复已是备用账号发出的，无需再检测
					if config.ConfigStruct.Fallback.Enabled && !isMainAccountCoolingDown() {
						go func() {
							shadowed := CheckShadowReply(v.LinkID, v.RootID, finalText)
							if shadowed {
								loger.Loger.Warn("[XHH]检测到影子评论！备用账号接替回复中...",
									zap.Int("link_id", v.LinkID),
									zap.Int("root_id", v.RootID))

								fallbackText := finalText
								if config.ConfigStruct.Fallback.Prompt != "" {
									fallbackText = regenerateWithFallbackPrompt(
										Contents, v.Text, top, tags, v.Uid, authorID,
										RootText, rootImgUrl, finalText, targetUID, targetName)
								}

								fallbackOk := ReplyWithFallback(fallbackText,
									strconv.Itoa(v.LinkID),
									strconv.Itoa(v.CommentID),
									strconv.Itoa(v.RootID), "")
								if fallbackOk {
									db.MarkFallbackReply(v.MsgID, fallbackText)
								}
							}
						}()
					}

				} else {
					loger.Loger.Error("[XHH]无法回复评论(接口或网络错误)")
				}
			}(v)
		}
		wg.Wait()

		jitter := time.Duration(rand.Intn(4)) * time.Second
		time.Sleep(time.Duration(ReplyTime)*time.Second + jitter)

		// ==========================================
		// 【防风控策略 4】：不卡死线程的摸鱼机制
		// ==========================================
		// 如果现在没在睡觉，也没在摸鱼，就有 8% 概率去摸鱼
		if inReplyWindow && time.Now().After(afkUntil) && rand.Intn(100) < 0 {
			afkMinutes := rand.Intn(2) + 1

			statusMsg := "去打派派了"
			randomStatus := rand.Intn(3)
			if randomStatus == 1 {
				statusMsg = "去吃大餐了"
			} else if randomStatus == 2 {
				statusMsg = "去看番了"
			}

			loger.Loger.Info(fmt.Sprintf("[XHH]塔菲%s，暂停营业。路人消息已排队，主人随时可唤醒她...", statusMsg), zap.Int("摸鱼时长(分钟)", afkMinutes))
			afkUntil = time.Now().Add(time.Duration(afkMinutes) * time.Minute)
		}
	}
}
