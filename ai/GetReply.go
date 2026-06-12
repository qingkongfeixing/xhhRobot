package ai

import (
	"fmt"
	"strings"
	"xhhrobot/config"
	"sync" // 【新增】：引入并发锁包
	"xhhrobot/loger"

	"go.uber.org/zap"
)


// 【新增】：带容量限制的视觉缓存池 (防止内存溢出)
var visionCache = make(map[string]string)
var cacheKeys []string
var visionLock sync.Mutex
const MaxCacheSize = 100 // 最高记忆 100 个不同的帖子图片，超出则忘掉最旧的

type Topics struct {
	Name string `json:"name"`
}
type Tags struct {
	Name string `json:"name"`
}

type ReplyResult struct {
	Text         string
	MainTokens   int
	VisionTokens int
	VisionDesc   string // 视觉模型输出
	Reasoning    string // 主脑模型思考过程
}

// RunVisionPipeline 仅执行视觉识别流水线，返回图片描述和Token消耗
func RunVisionPipeline(Contents []Content, ParentImgUrl string) (description string, tokens int) {
	enableVision := config.ConfigStruct.Ai.EnableVision
	visionModel := config.ConfigStruct.Ai.VisionModel
	visionPrompt := config.ConfigStruct.Ai.VisionPrompt

	var vContents []Content
	hasImage := false
	cacheKey := ""

	if visionPrompt == "" {
		visionPrompt = "请客观描述以下图片的内容和文字。"
	}
	vContents = append(vContents, Content{Type: "text", Text: visionPrompt})

	maxPostImg := config.ConfigStruct.Ai.MaxPostImages
	if maxPostImg <= 0 {
		maxPostImg = 5
	}
	maxCommentImg := config.ConfigStruct.Ai.MaxCommentImages
	if maxCommentImg <= 0 {
		maxCommentImg = 3
	}

	postImgCount := 0
	for _, c := range Contents {
		if c.Type == "image_url" && c.ImgUrl.Url != "" {
			if postImgCount >= maxPostImg {
				break
			}
			vContents = append(vContents, Content{Type: "text", Text: "\n【这是<角色1: 帖主>发布的图片】:"})
			vContents = append(vContents, c)
			hasImage = true
			cacheKey += c.ImgUrl.Url + "|"
			postImgCount++
		}
	}

	if ParentImgUrl != "" {
		urls := strings.Split(ParentImgUrl, ",")
		commentImgCount := 0
		for _, u := range urls {
			u = strings.TrimSpace(u)
			if u != "" && commentImgCount < maxCommentImg {
				vContents = append(vContents, Content{Type: "text", Text: "\n【这是<角色2: 层主>发布的图片】:"})
				vContents = append(vContents, Content{
					Type:   "image_url",
					ImgUrl: struct{ Url string `json:"url"` }{Url: u},
				})
				hasImage = true
				cacheKey += u + "|"
				commentImgCount++
			}
		}
	}

	if hasImage && enableVision && visionModel != "" {
		visionLock.Lock()
		cachedDesc, ok := visionCache[cacheKey]
		visionLock.Unlock()

		if ok {
			loger.Loger.Info("[Ai]⚡ 命中视觉记忆缓存，隔空识图成功，瞬间出图！")
			return cachedDesc, 0
		}

		loger.Loger.Info("[Ai]检测到新图片，正在启用【视觉模型】进行流水线解构...")
		visionMsgs := []any{
			Messages[[]Content]{Role: "user", Content: vContents},
		}

		// 【视觉模型主通道】
		visionBaseUrl := config.ConfigStruct.Ai.VisionBaseUrl
		visionToken := config.ConfigStruct.Ai.VisionToken
		if visionBaseUrl == "" {
			visionBaseUrl = config.ConfigStruct.Ai.BaseUrl
		}
		if visionToken == "" {
			visionToken = config.ConfigStruct.Ai.Token
		}
		visionResp := SendVisionReq(visionModel, visionBaseUrl, visionToken, visionMsgs)

		// 【视觉模型备用通道】：主模型解析失败时，尝试备用模型
		if len(visionResp.Choices) == 0 {
			fallbackModel := config.ConfigStruct.Ai.VisionFallbackModel
			fallbackBaseUrl := config.ConfigStruct.Ai.VisionFallbackBaseUrl
			fallbackToken := config.ConfigStruct.Ai.VisionFallbackToken

			if fallbackModel != "" {
				if fallbackBaseUrl == "" {
					fallbackBaseUrl = visionBaseUrl
				}
				if fallbackToken == "" {
					fallbackToken = visionToken
				}
				loger.Loger.Warn("[Ai]主视觉模型解析失败，正在启用【备用视觉模型】重试...",
					zap.String("fallback_model", fallbackModel))
				visionResp = SendVisionReq(fallbackModel, fallbackBaseUrl, fallbackToken, visionMsgs)
			}
		}

		if len(visionResp.Choices) > 0 {
			description = visionResp.Choices[0].Msg.Content
			tokens = visionResp.Usage.TotalToken
			loger.Loger.Info("[Ai]视觉解构完成", zap.Int("视觉Token消耗", tokens))

			visionLock.Lock()
			if _, exists := visionCache[cacheKey]; !exists {
				visionCache[cacheKey] = description
				cacheKeys = append(cacheKeys, cacheKey)
				if len(cacheKeys) > MaxCacheSize {
					oldestKey := cacheKeys[0]
					cacheKeys = cacheKeys[1:]
					delete(visionCache, oldestKey)
				}
			}
			visionLock.Unlock()
		} else {
			loger.Loger.Warn("[Ai]视觉模型及备用模型均解析失败")
		}
	}
	return
}

// GetAiReplyWithPrompt 同 GetAiReply，但允许传入自定义 system prompt（避免修改全局配置造成竞态）
func GetAiReplyWithPrompt(Contents []Content, UserSay string, Topics []Topics, Tags []Tags, uid int, authorID int, ParentText string, ParentImgUrl string, customPrompt string) ReplyResult {
	return getAiReplyImpl(Contents, UserSay, Topics, Tags, uid, authorID, ParentText, ParentImgUrl, customPrompt)
}

func GetAiReply(Contents []Content, UserSay string, Topics []Topics, Tags []Tags, uid int, authorID int, ParentText string, ParentImgUrl string) ReplyResult {
	return getAiReplyImpl(Contents, UserSay, Topics, Tags, uid, authorID, ParentText, ParentImgUrl, config.ConfigStruct.Ai.Prompt)
}

func getAiReplyImpl(Contents []Content, UserSay string, Topics []Topics, Tags []Tags, uid int, authorID int, ParentText string, ParentImgUrl string, prompt string) ReplyResult {
	loger.Loger.Info("[Ai]正在准备询问Ai（包含图片视觉分析）")
	var SMsg Messages[string]
	var UMsg Messages[[]Content]
	var Msgs []any

	SMsg.Role = "system"
	var topStr strings.Builder
	for _, v := range Topics {
		topStr.WriteString(v.Name)
	}
	prompt = strings.ReplaceAll(prompt, "?!top!?", topStr.String())
	var tagStr strings.Builder
	for _, v := range Tags {
		tagStr.WriteString(v.Name)
	}
	prompt = strings.ReplaceAll(prompt, "?!tag!?", tagStr.String())
	SMsg.Content = prompt

	isOwner := false
	owners := strings.Split(config.ConfigStruct.Xhh.Owner, ",")
	for _, o := range owners {
		if o == fmt.Sprintf("%d", uid) {
			isOwner = true
			break
		}
	}
	// 【新增】：如果开启了"仅回复主人"，且当前触发者不是主人，直接拦截并返回空（机器人将无视）
	// 自动刷帖不受此限制
	if ParentText != "【自动刷帖】" && config.ConfigStruct.Xhh.ReplyOnlyOwner && !isOwner {
		loger.Loger.Info("[Ai]当前开启了【仅回复主人】模式，已无视其他用户的召唤")
		return ReplyResult{}
	}

	isAuthor := false
	if uid == authorID && authorID != 0 {
		isAuthor = true
	}

	userIdentity := "一名普通的雏草姬"
	if isOwner {
		userIdentity = "这台机器人的开发者（但你也要把TA当成“雏草姬”，绝对不要叫主人）"
	} else if isAuthor {
		userIdentity = "这篇帖子的作者（也是一名雏草姬）"
	}

	UMsg.Role = "user"
	var dynamicPrompt string

	// 提前定义关系提示词，避免 Go 语言报错变量未使用
	relationPrompt := "【身份警告】：@你的人【绝对不是】帖主！TA只是个吃瓜群众。"
	if isAuthor {
		relationPrompt = "【身份警告】：@你的人【就是】帖主本人！TA在自己发的帖子下面跟你互动。"
	} else if isOwner {
		relationPrompt = "【身份警告】：@你的人是开发者，且TA【绝对不是】这篇帖子的作者！"
	}

	// 【新增】：智能判断是否为单人模式
	isSinglePlayer := false
	if ParentText == "" && ParentImgUrl == "" {
		isSinglePlayer = true
	}
	isFeedReply := ParentText == "【自动刷帖】"
	if ParentText == "【单人模式】" || isFeedReply {
		isSinglePlayer = true
		ParentText = "" // 识别成功后擦除标记，防止干扰AI
	}

		if isFeedReply {
		// 自动刷帖专用：直接评论帖子，无召唤者/层主，禁止任何 @ 占位符
		if config.ConfigStruct.FeedReply.SummaryMode {
			// 【动态字数限制】：根据原帖正文长度和配置比例计算总结字数上限
			postTextLen := 0
			for _, c := range Contents {
				if c.Type == "text" {
					postTextLen += len([]rune(c.Text))
				}
			}
			ratio := config.ConfigStruct.FeedReply.SummaryRatio
			if ratio <= 0 || ratio > 1 {
				ratio = 0.5
			}
			minWords := config.ConfigStruct.FeedReply.SummaryMinWords
			if minWords <= 0 {
				minWords = 50
			}
			maxWords := config.ConfigStruct.FeedReply.SummaryMaxWords
			if maxWords <= 0 {
				maxWords = 800
			}
			summaryLimit := int(float64(postTextLen) * ratio)
			if summaryLimit < minWords {
				summaryLimit = minWords
			}
			if summaryLimit > maxWords {
				summaryLimit = maxWords
			}

			// 总结模式：保留基础口癖，尽量完整总结帖子文本内容
			dynamicPrompt = fmt.Sprintf(`
		======【场景设定】======
		你正在浏览社区帖子，你需要对这篇帖子进行内容总结。

		<角色: 帖主>
		身份：上方【帖子原内容】的发布者。
		</角色: 帖主>

		【系统最高强制指令】：
		1. 【总结为核心】：提取帖子的【核心事件/观点/结论】，用最少的文字覆盖最重要的信息，砍掉例子、修饰、重复。
		2. 【口癖保留】：回复时保留你的基础人设口癖（如"雏草姬"、"喵"等语气词），但整体以总结内容为主。
		3. 【结构清晰】：按帖子内容的逻辑顺序进行总结，可以分段但不要过度换行（最多2次换行）。
		4. 【绝对禁止艾特】：不要输出"@帖主"、"@召唤者"、"@层主"、"@主人"等任何艾特占位符或蓝字链接。
		5. 【字数要求】：总结字数控制在%d字以内，宁可精简也别水字数。
		6. 【SKIP 指令】：如果帖子内容为空或无法总结，请只输出 SKIP
		`, summaryLimit)
		} else {
			// 正常评论模式
			dynamicPrompt = `
		======【场景设定】======
		你正在浏览社区帖子，请保持你的人设风格，对这篇帖子发表一条自然、有信息量的短评论。

		<角色: 帖主>
		身份：上方【帖子原内容】的发布者。
		</角色: 帖主>

		【系统最高强制指令】：
		1. 【评论为核心】：针对帖子内容本身发表看法、吐槽或补充，用你的人设风格自然评论。
		2. 【绝对禁止艾特】：不要输出"@帖主"、"@召唤者"、"@层主"、"@主人"等任何艾特占位符或蓝字链接，直接发纯文字评论即可。
		3. 【字数与排版生死线（极度重要）】：回复必须极度精简！字数【绝对不可超过200字】严禁连续换行，全篇最多1个emoji！
		4. 【SKIP 指令】：如果不适合回复或缺乏信息量，请只输出 SKIP
		`
		}
	} else if !isSinglePlayer {
		if ParentText == "" {
			ParentText = "（层主未发送文字，仅发送了一张图片）"
		}

		dynamicPrompt = fmt.Sprintf(`
		======【场景设定】======
		当前发生了一个多角色互动场景，请严格根据以下XML标签区分人物身份，绝不能发生记忆串边错乱！

		<角色1: 帖主>
		身份：上方【帖子原内容】的发布者。
		</角色1: 帖主>

		<角色2: 层主>
		身份：在评论区发言的路人。
		TA的评论内容：「%s」
		</角色2: 层主>

		<角色3: 召唤者>
		身份：%s（UID:%d）
		%s
		TA在评论中召唤了你，对你说：「%s」
		</角色3: 召唤者>

		【系统最高强制指令】：
		1. 【理清逻辑，切勿张冠李戴】：绝不能把这三个角色搞混！评价帖子内容针对<角色1>，回应指令针对<角色3>，并根据指令去制裁<角色2>。
		2. 【绝对服从召唤者指令（最重要）】：
		    - 只有当<角色3>【明确下达指令】让你去"骂/嘲讽/反驳"<角色2>或<角色1>时，你才可以对其进行适度的毒舌攻击，但请把握好玩梗的尺度，不要进行人身辱骂。\n"
		    - 如果<角色3>（召唤者）是在对你表白、闲聊或夸赞你，请开心地用傲娇语气回应TA即可，不要随意攻击无辜的路人。<角色2>的问题可直接无视，也不要去艾特他。\n"
		3. 【有问必答】：如果<角色3>（注意是召唤者，不是层主）提出了明确的问题，你【必须】正面解答。
		4. 【字数与排版生死线（极度重要）】：你的回复必须极度精简！字数【绝对不可超过200字】废话太多会被系统抹杀！严禁连续换行，全篇最多1个emoji！不准叫"主人"，句子里统一称呼为"雏草姬"。
		5. 【智能艾特与占位符铁律（绝对禁止修改）】：
		   - 对【召唤者】说话时，句首【必须原封不动】输出"@召唤者 "！
		   - 对【帖主】说话时，句首【必须原封不动】输出"@帖主 "！
		   - 【核心决策】：你需要智能判断召唤者的话是否和【层主】或【帖主】相关。如果召唤者让你去吐槽/攻击帖主，你必须在句首输出"@帖主 "！如果和他们无关，【绝对不要】输出这些占位符！
		   （最高警告："@召唤者"、"@层主"、"@帖主"是底层生成蓝字的代码，绝不准私自改成真实人名，必须原样输出这几个字！）
		   - 自由决定先对谁说话。
			6. 【强制思考路径】：在输出正式回复前，你必须在深层思考（<think>标签内）完成以下三步推演：
			   - ① 帖主的文字内容说了一件什么事？
			   - ② 视觉报告里的图片和帖主的事有什么关联？（如果毫无关联，说明是帖主乱配的表情包）。
			   - ③ 综合以上两点，召唤者叫我"评价一下"，我作为塔菲，最应该吐槽帖主的遭遇，还是图片的生草点？
			   完成思考后，再生成符合字数限制的最终回复。
		`, ParentText, userIdentity, uid, relationPrompt, UserSay)

	} else {
		// 单人互动场景（召唤者直接在帖子里@你，或者回复自己的评论）
		dynamicPrompt = fmt.Sprintf(`
		======【场景设定】======
		当前发生了一个互动场景，请严格根据以下XML标签区分人物身份：

		<角色1: 帖主>
		身份：上方【帖子原内容】的发布者。
		</角色1: 帖主>

		<角色3: 召唤者>
		身份：%s（UID:%d）
		%s
		TA在评论区@了你，对你说：「%s」
		</角色3: 召唤者>

		【系统最高强制指令】：
		1. 【理清逻辑】：认清<角色3>是谁，不要把TA和<角色1>搞混！
		2. 【核心决策】：你需要智能判断召唤者的话是否和【帖主】相关。如果召唤者让你去吐槽、解答或攻击帖主（例如"小菲给帖主唱生日歌"），你必须在输出时带上"@帖主 "！如果和帖主完全无关，【绝对不要】输出这些占位符！
		3. 【字数与排版生死线（极度重要）】：你的回复必须极度精简！字数【绝对不可超过200字】废话太多会被系统抹杀！严禁连续换行，全篇最多1个emoji！不准叫"主人"，句子里统一称呼为"雏草姬"。
		4. 【底层占位符铁律（程序生死线）】：
		   - 对【召唤者】说话时，句首【必须原封不动】输出"@召唤者 "！
		   - 对【帖主】说话时，句首【必须原封不动】输出"@帖主 "！
		   【最高警告】：哪怕帖子正文里出现了其他任何人的名字，你也【绝对禁止】去艾特他们！绝不准私自改成真实人名，必须原样输出占位符！绝对服从！
			5. 【强制思考路径】：在输出正式回复前，你必须在深层思考（<think>标签内）完成以下三步推演：
			   - ① 帖主的文字内容说了一件什么事？
			   - ② 视觉报告里的图片和帖主的事有什么关联？（如果毫无关联，说明是帖主乱配的表情包）。
			   - ③ 综合以上两点，召唤者叫我"评价一下"，我作为塔菲，最应该吐槽帖主的遭遇，还是图片的生草点？
			   完成思考后，再生成符合字数限制的最终回复。
		`, userIdentity, uid, relationPrompt, UserSay)
	}

	// 【流水线】：根据视觉模式选择路径
	visionMode := config.ConfigStruct.Ai.VisionMode
	if visionMode == "" {
		visionMode = "dual"
	}
	enableVision := config.ConfigStruct.Ai.EnableVision

	var imageDescription string
	var visionTokens int

	hasImage := false
	for _, c := range Contents {
		if c.Type == "image_url" && c.ImgUrl.Url != "" {
			hasImage = true
			break
		}
	}
	if !hasImage && ParentImgUrl != "" {
		hasImage = true
	}

	if visionMode == "single" {
		loger.Loger.Info("[Ai]单模态模式，图片将直接发送给主模型")
	} else {
		imageDescription, visionTokens = RunVisionPipeline(Contents, ParentImgUrl)
	}

	var FinalContents []Content

	FinalContents = append(FinalContents, Content{
		Type: "text",
		Text: "======【这是帖主发布的帖子原内容】======",
	})

	if visionMode == "single" && enableVision {
		// 单模态模式：保留图片直接发送给主模型
		maxPostImg := config.ConfigStruct.Ai.MaxPostImages
		if maxPostImg <= 0 {
			maxPostImg = 5
		}
		maxCommentImg := config.ConfigStruct.Ai.MaxCommentImages
		if maxCommentImg <= 0 {
			maxCommentImg = 3
		}

		postImgCount := 0
		for _, c := range Contents {
			if c.Type == "image_url" && c.ImgUrl.Url != "" {
				if postImgCount >= maxPostImg {
					continue
				}
				FinalContents = append(FinalContents, Content{Type: "text", Text: "\n【这是<角色1: 帖主>发布的图片】:"})
				FinalContents = append(FinalContents, c)
				postImgCount++
			} else if c.Type == "text" {
				FinalContents = append(FinalContents, c)
			}
		}

		if ParentImgUrl != "" {
			urls := strings.Split(ParentImgUrl, ",")
			commentImgCount := 0
			for _, u := range urls {
				u = strings.TrimSpace(u)
				if u != "" && commentImgCount < maxCommentImg {
					FinalContents = append(FinalContents, Content{Type: "text", Text: "\n【这是<角色2: 层主>发布的图片】:"})
					FinalContents = append(FinalContents, Content{
						Type:   "image_url",
						ImgUrl: struct{ Url string `json:"url"` }{Url: u},
					})
					commentImgCount++
				}
			}
		}
	} else {
		// 双模型模式：剥离图片，只保留纯文本为主大脑减负
		for _, c := range Contents {
			if c.Type == "text" {
				FinalContents = append(FinalContents, c)
			}
		}

		// 注入视觉模型生成的文字报告
		if imageDescription != "" {
			FinalContents = append(FinalContents, Content{
				Type: "text",
				Text: fmt.Sprintf("\n======【系统视觉分析报告】======\n当前场景中包含图片。以下是视觉模型对帖内配图的客观描述（请务必综合上方的【帖子原内容】与下方的【图片描述】，理解完整的事件语境后再生成回复）：\n%s\n=========================\n", imageDescription),
			})
		} else if hasImage {
			FinalContents = append(FinalContents, Content{
				Type: "text",
				Text: "\n[系统提示：对方发送了图片，但视觉解析失败或未开启，请直接吐槽你看不见图]\n",
			})
		}
	}

	FinalContents = append(FinalContents, Content{
		Type: "text",
		Text: "\n======【以上为历史素材参考】======\n" + dynamicPrompt,
	})

	UMsg.Content = FinalContents
	Msgs = append(Msgs, SMsg)
	Msgs = append(Msgs, UMsg)

	aiModel := config.ConfigStruct.Ai.Model
	resp := SendReq(aiModel, Msgs)
	if len(resp.Choices) == 0 {
		return ReplyResult{}
	}
	text := resp.Choices[0].Msg.Content
	reasoning := resp.Choices[0].Msg.Reason

	// 【终端极简排版】
	fmt.Printf("\n\033[1;35m============= 塔菲大脑响应 =============\033[0m\n")
	fmt.Printf("🤖 塔菲回复:\n%s\n", text)
	fmt.Printf("📊 消耗算力: %d Tokens\n", resp.Usage.TotalToken)
	fmt.Printf("\033[1;35m========================================\033[0m\n\n")

	// 【全量日志保留】改为 Debug 写入日志文件
	loger.Loger.Debug("[Ai]Ai说：", zap.String("text", text), zap.Int("本次消耗token", resp.Usage.TotalToken))
	return ReplyResult{
		Text:         text,
		MainTokens:   resp.Usage.TotalToken,
		VisionTokens: visionTokens,
		VisionDesc:   imageDescription,
		Reasoning:    reasoning,
	}

}