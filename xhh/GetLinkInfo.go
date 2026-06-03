package xhh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xhhrobot/ai"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

// CommentInfo 单条评论的完整信息
type CommentInfo struct {
	CommentID int    `json:"commentid"`
	UserID    int    `json:"userid"`
	Text      string `json:"text"`
	ReplyID   int    `json:"replyid"`
	FloorNum  int    `json:"floor_num"`
	User      struct {
		UserName string `json:"username"`
	} `json:"user"`
	Imgs []struct {
		Url string `json:"url"`
	} `json:"imgs"`
	ReplyUser struct {
		UserName string `json:"username"`
	} `json:"replyuser"`
}

type commentGroup struct {
	Comment []CommentInfo `json:"comment"`
}

// LinkInfoS 帖子详情 + 评论区第一页的完整响应
type LinkInfoS struct {
	Msg    string `json:"msg"`
	Result struct {
		Comments      []commentGroup `json:"comments"`
		TotalPage     int            `json:"total_page"`
		HasMoreFloors int            `json:"has_more_floors"`
		Link          struct {
			Title  string      `json:"title"`
			Text   string      `json:"text"`
			Topics []ai.Topics `json:"topics"`
			Tags   []ai.Tags   `json:"hashtags"`
			User   struct {
				UserID   int    `json:"userid"`
				UserName string `json:"username"`
			} `json:"user"`
		} `json:"link"`
	} `json:"result"`
	Stat string `json:"status"`
}

// SubCommentsS 子评论翻页响应
type SubCommentsS struct {
	Msg    string `json:"msg"`
	Result struct {
		HasMore  bool          `json:"has_more"`
		LastVal  int           `json:"lastval"`
		Comments []CommentInfo `json:"comments"`
	} `json:"result"`
	Stat string `json:"status"`
}

type TextDetail struct {
	Text string `json:"text"`
	Type string `json:"type"`
	Url  string `json:"url"`
}

// fetchLinkInfoPage 请求帖子详情 + 评论区单页数据。
// forceFallback 为 true 时强制使用备用账号（影子检测专用），否则自动依据 IsFallbackAvailable 选择。
func fetchLinkInfoPage(linkID int, page int, forceFallback bool) (LinkInfoS, bool) {
	var data LinkInfoS
	if xhhCaptchaCoolingDown("link_tree") {
		return data, false
	}

	isFirst := "0"
	if page == 1 {
		isFirst = "1"
	}
	other := "?h_src&link_id=" + strconv.Itoa(linkID) + "&page=" + strconv.Itoa(page) + "&is_first=" + isFirst + "&index=1&limit=20&owner_only=0"

	var resp *http.Response
	if forceFallback {
		resp = SendReqWithFallback("GET", "/bbs/app/link/tree", nil, other)
	} else if IsFallbackAvailable() {
		resp = SendReqWithFallback("GET", "/bbs/app/link/tree", nil, other)
	} else {
		resp = SendReq("GET", "/bbs/app/link/tree", nil, other)
	}
	if resp == nil {
		loger.Loger.Error("[XHH]获取LinkInfo失败，请求未成功发送", zap.Int("link_id", linkID), zap.Int("page", page))
		return data, false
	}
	defer resp.Body.Close()

	Dbyte, err := io.ReadAll(resp.Body)
	if err != nil {
		loger.Loger.Error("[XHH]无法读取响应体", zap.Error(err), zap.Int("link_id", linkID), zap.Int("page", page))
		return data, false
	}

	if strings.Contains(string(Dbyte), "captcha") || strings.Contains(string(Dbyte), "ticket") {
		loger.Loger.Error("[XHH]触发了小黑盒的风控验证码拦截！请手动过滑块或稍后再试。")
		enterXHHCaptchaCooldown("link_tree")
		return data, false
	}

	err = json.Unmarshal(Dbyte, &data)
	if err != nil {
		loger.Loger.Error("[XHH]反序列化失败", zap.Error(err), zap.Any("data", string(Dbyte)))
		return data, false
	}
	if data.Stat != "ok" {
		if isXHHCaptchaStatus(data.Stat) {
			enterXHHCaptchaCooldown("link_tree")
			return data, false
		}
		loger.Loger.Error("[XHH]返回了错误的内容", zap.String("status", data.Stat), zap.String("msg", data.Msg))
		return data, false
	}

	return data, true
}

// findCommentGroup 在评论组列表中定位匹配 rootCommentID 的楼层
func findCommentGroup(groups []commentGroup, rootCommentID int) []CommentInfo {
	if rootCommentID == 0 {
		return nil
	}
	for _, group := range groups {
		if len(group.Comment) == 0 {
			continue
		}
		if group.Comment[0].CommentID == rootCommentID {
			comments := make([]CommentInfo, len(group.Comment))
			copy(comments, group.Comment)
			return comments
		}
	}
	return nil
}

// paginateSubComments 当目标评论不在楼层的首批子评论中时，翻页拉取更多子评论
func paginateSubComments(rootCommentID int, targetCommentID int, comments []CommentInfo) []CommentInfo {
	if rootCommentID == 0 || len(comments) == 0 {
		return comments
	}
	for _, c := range comments {
		if c.CommentID == targetCommentID {
			return comments
		}
	}

	lastVal := comments[len(comments)-1].CommentID
	for i := 0; i < 20; i++ {
		if xhhCaptchaCoolingDown("sub_comments") {
			return comments
		}
		other := "?root_comment_id=" + strconv.Itoa(rootCommentID) + "&lastval=" + strconv.Itoa(lastVal)
		resp := SendReq("GET", "/bbs/app/comment/sub/comments", nil, other)
		if resp == nil {
			return comments
		}

		Dbyte, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			loger.Loger.Error("[XHH]无法读取子评论响应体", zap.Error(err), zap.Int("root_comment_id", rootCommentID))
			return comments
		}

		if strings.Contains(string(Dbyte), "captcha") || strings.Contains(string(Dbyte), "ticket") {
			enterXHHCaptchaCooldown("sub_comments")
			return comments
		}

		var data SubCommentsS
		err = json.Unmarshal(Dbyte, &data)
		if err != nil {
			loger.Loger.Error("[XHH]子评论反序列化失败", zap.Error(err))
			return comments
		}
		if isXHHCaptchaStatus(data.Stat) {
			enterXHHCaptchaCooldown("sub_comments")
			return comments
		}
		if data.Stat != "ok" || len(data.Result.Comments) == 0 {
			return comments
		}

		comments = append(comments, data.Result.Comments...)
		for _, c := range data.Result.Comments {
			if c.CommentID == targetCommentID {
				return comments
			}
		}
		if !data.Result.HasMore {
			return comments
		}
		if data.Result.LastVal != 0 && data.Result.LastVal != lastVal {
			lastVal = data.Result.LastVal
		} else {
			lastVal = data.Result.Comments[len(data.Result.Comments)-1].CommentID
		}
	}
	return comments
}

// GetLinkInfo 获取帖子正文信息
func GetLinkInfo(LinkID int) (Contents []ai.Content, Topics []ai.Topics, Tags []ai.Tags, AuthorID int, PostTitle string) {
	data, ok := fetchLinkInfoPage(LinkID, 1, false)
	if !ok {
		return
	}

	var Content []TextDetail
	err := json.Unmarshal([]byte(data.Result.Link.Text), &Content)
	if err != nil {
		loger.Loger.Error("[XHH]无法解析内容", zap.Error(err))
		return
	}
	for _, v := range Content {
		var content ai.Content
		if v.Type == "html" {
			content.Type = "text"
			content.Text = v.Text
			Contents = append(Contents, content)
			continue
		}
		if v.Type != "text" {
			if v.Url == "" {
				continue
			}
			content.Type = "image_url"
			content.ImgUrl.Url = v.Url
			Contents = append(Contents, content)
			continue
		}
		content.Type = "text"
		content.Text = v.Text
		Contents = append(Contents, content)
	}
	loger.Loger.Debug("[XHH]获取到帖子全文", zap.Any("Contents", Contents))

	return Contents, data.Result.Link.Topics, data.Result.Link.Tags, data.Result.Link.User.UserID, data.Result.Link.Title
}

const maxCommentPages = 30

// searchCommentGroup 在帖子评论区翻页查找指定 rootID 的楼层，最多翻 maxCommentPages 页
func searchCommentGroup(linkID int, rootID int, forceFallback bool) []CommentInfo {
	data, ok := fetchLinkInfoPage(linkID, 1, forceFallback)
	if !ok {
		return nil
	}

	comments := findCommentGroup(data.Result.Comments, rootID)
	if comments != nil {
		return comments
	}

	maxPage := data.Result.TotalPage
	if maxPage <= 0 {
		maxPage = 1
	}
	if maxPage > maxCommentPages {
		maxPage = maxCommentPages
	}
	for page := 2; page <= maxPage; page++ {
		time.Sleep(time.Millisecond * 300)
		pageData, ok := fetchLinkInfoPage(linkID, page, forceFallback)
		if !ok {
			continue
		}
		comments = findCommentGroup(pageData.Result.Comments, rootID)
		if comments != nil {
			return comments
		}
		if pageData.Result.HasMoreFloors == 0 {
			break
		}
	}
	return nil
}

// GetRootComment 遍历帖子评论区，获取层主（根评论）的文字、图片和昵称
func GetRootComment(linkID int, rootID int) (rootText string, rootImgUrl string, targetUID int, targetUserName string) {
	if rootID == 0 {
		return "", "", 0, ""
	}
	if xhhCaptchaCoolingDown("link_tree_page") {
		return "", "", 0, ""
	}

	comments := searchCommentGroup(linkID, rootID, false)
	if comments == nil || len(comments) == 0 {
		loger.Loger.Warn("[XHH]未能找到层主，可能API缓存未刷新", zap.Int("目标RootID", rootID))
		return "", "", 0, ""
	}

	c := comments[0]
	rootText = c.Text

	if len(c.Imgs) > 0 {
		rootImgUrl = c.Imgs[0].Url
	}

	targetUID = c.UserID
	targetUserName = c.User.UserName

	loger.Loger.Debug("[XHH]成功锁定层主", zap.String("层主昵称", targetUserName), zap.String("发言内容", rootText), zap.String("包含图片", rootImgUrl))
	return rootText, rootImgUrl, targetUID, targetUserName
}

// GetCommentContext 获取评论区的上下文（根评论楼层内的所有子评论），
// 用于 AI 理解当前讨论语境。返回格式化后的评论文本。
func GetCommentContext(linkID int, rootID int, commentID int) string {
	if rootID == 0 {
		return ""
	}

	comments := searchCommentGroup(linkID, rootID, false)
	if comments == nil {
		return ""
	}

	// 如果目标子评论不在首批中，翻页拉取
	comments = paginateSubComments(rootID, commentID, comments)

	var lines []string
	for _, c := range comments {
		if c.Text == "" {
			continue
		}
		name := c.User.UserName
		if name == "" {
			name = "用户"
		}
		line := fmt.Sprintf("[user_id:%d] %s", c.UserID, name)
		if c.ReplyUser.UserName != "" {
			line += " 回复 " + c.ReplyUser.UserName
		}
		line += "：" + c.Text
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// GetUserName 通过用户ID获取用户名
func GetUserName(userID int) string {
	resp := SendReq("GET", "/bbs/app/user/profile", nil, "?userid="+strconv.Itoa(userID))
	if resp == nil {
		loger.Loger.Debug("[XHH]获取用户名失败：请求未成功发送", zap.Int("userID", userID))
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		loger.Loger.Debug("[XHH]获取用户名失败：无法读取响应", zap.Error(err), zap.Int("userID", userID))
		return ""
	}

	var result struct {
		Status string `json:"status"`
		Result struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"result"`
	}

	err = json.Unmarshal(data, &result)
	if err != nil {
		loger.Loger.Debug("[XHH]解析用户名JSON失败", zap.Error(err), zap.String("响应", string(data)), zap.Int("userID", userID))
		return ""
	}

	if result.Status != "ok" {
		loger.Loger.Debug("[XHH]API返回非ok状态", zap.String("status", result.Status), zap.Int("userID", userID))
		return ""
	}

	if result.Result.User.Username != "" {
		loger.Loger.Debug("[XHH]成功获取用户名", zap.String("username", result.Result.User.Username), zap.Int("userID", userID))
	}
	return result.Result.User.Username
}

func isXHHCaptchaStatus(status string) bool {
	return status == "show_captcha" || status == "error_captcha"
}
