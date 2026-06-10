package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
	"xhhrobot/config"
	"xhhrobot/loger"

	"go.uber.org/zap"
)

// 定义搜索的高级选项结构体
type SearchOpts struct {
	ForcedSearch          bool `json:"forced_search,omitempty"`
	EnableSearchExtension bool `json:"enable_search_extension,omitempty"`
}

type ReqBody struct {
	Model           string      `json:"model"`
	Messages        []any       `json:"messages"`
	Stream          bool        `json:"stream"`
	EnableSearch    bool        `json:"enable_search,omitempty"`
	SearchOptions   *SearchOpts `json:"search_options,omitempty"`
	EnableThinking  *bool       `json:"enable_thinking,omitempty"`
	ReasoningEffort string      `json:"reasoning_effort,omitempty"`
}

type Content struct {
	Type   string `json:"type"`
	ImgUrl struct {
		Url string `json:"url"`
	} `json:"image_url,omitempty"`
	Text string `json:"text"`
}

type Messages[T []Content | string] struct {
	Role    string `json:"role"`
	Content T      `json:"content"`
}

type SysMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type choice struct {
	Index int `json:"index"`
	Msg   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Reason  string `json:"reasoning_content"`
	} `json:"message"`
}

type respStruct struct {
	Choices []choice `json:"choices"`
	Usage   struct {
		TotalToken int `json:"total_tokens"`
	} `json:"usage"`
}

// SendVisionReq 纯视觉请求，不经过主脑路由，用于视觉模型专用通道和备用模型切换
func SendVisionReq(Model string, BaseUrl string, Token string, Msg []any) (Jresp respStruct) {
	if Model == "" || BaseUrl == "" {
		loger.Loger.Error("[Ai]视觉模型配置不完整，跳过视觉请求")
		return
	}

	falseVal := false
	body := ReqBody{
		Model:          Model,
		Messages:       Msg,
		Stream:         false,
		EnableThinking: &falseVal,
	}

	reqbody, err := json.Marshal(body)
	if err != nil {
		loger.Loger.Error("[Ai-Vision]无法序列化JSON", zap.Error(err))
		return
	}

	loger.Loger.Debug("[Ai-Vision]底层网络请求", zap.String("model", Model), zap.String("url", BaseUrl), zap.String("Request", string(reqbody)))

	req, err := http.NewRequest("POST", BaseUrl, bytes.NewReader(reqbody))
	if err != nil {
		loger.Loger.Error("[Ai-Vision]无法创建请求", zap.Error(err))
		return
	}

	req.Header.Set("Authorization", "Bearer "+Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		loger.Loger.Error("[Ai-Vision]请求失败！", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	Dresp, err := io.ReadAll(resp.Body)
	loger.Loger.Debug("[Ai-Vision]底层网络响应", zap.String("Response", string(Dresp)))

	if resp.StatusCode != http.StatusOK {
		loger.Loger.Error("[Ai-Vision]请求被拒绝或服务器异常",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(Dresp)),
		)
		return
	}

	if len(Dresp) == 0 {
		loger.Loger.Error("[Ai-Vision]接口返回了空的响应体")
		return
	}

	err = json.Unmarshal(Dresp, &Jresp)
	if err != nil {
		loger.Loger.Error("[Ai-Vision]无法反序列化JSON", zap.Error(err), zap.String("body", string(Dresp)))
		return
	}

	return Jresp
}

func SendReq(Model string, Msg []any) (Jresp respStruct) {
	if Model == "" {
		loger.Loger.Fatal("[Ai]请确保配置文件中的模型是存在的")
	}

	cfg := config.ConfigStruct.Ai

	var body ReqBody
	body.Model = Model
	body.Messages = Msg
	body.Stream = false

// ==========================================
	// 【流水线核心】：智能判断当前任务与 API 路由
	// ==========================================
	isVisionCall := (Model == cfg.VisionModel && cfg.VisionModel != "")

	targetUrl := cfg.BaseUrl
	targetToken := cfg.Token

	// 如果当前是视觉模型（阿里云/豆包），切换专属通道
	if isVisionCall {
		if cfg.VisionBaseUrl != "" {
			targetUrl = cfg.VisionBaseUrl
		}
		if cfg.VisionToken != "" {
			targetToken = cfg.VisionToken
		}

		// 视觉模型：强制捏死思考功能，让body保持最纯净状态
		falseVal := false
		body.EnableThinking = &falseVal

	} else {
		// 【解封】：主脑模型专属配置！把属于 DeepSeek 的搜索和思考功能还给它！
		body.EnableThinking = &cfg.EnableThinking
		if cfg.EnableThinking && cfg.ReasoningEffort != "" {
			body.ReasoningEffort = cfg.ReasoningEffort
		}
		if cfg.EnableSearch {
			body.EnableSearch = true
			body.SearchOptions = &SearchOpts{
				ForcedSearch:          true,
				EnableSearchExtension: cfg.EnableSearchExt,
			}
		}
	}

	reqbody, err := json.Marshal(body)
	if err != nil {
		loger.Loger.Error("[AI]无法序列化JSON", zap.Error(err))
		return
	}

	loger.Loger.Debug("[Ai]底层网络请求", zap.String("Request", string(reqbody)))

	// 注意：这里使用的是智能切换后的 targetUrl
	req, err := http.NewRequest("POST", targetUrl, bytes.NewReader(reqbody))
	if err != nil {
		loger.Loger.Error("[AI]无法创建请求", zap.Error(err))
		return
	}

	// 注意：这里使用的是智能切换后的 targetToken
	req.Header.Set("Authorization", "Bearer "+targetToken)
	req.Header.Set("Content-Type", "application/json")
	// 【核心修复】：为大模型请求加上 60 秒的绝对超时时间，防止接口卡住导致机器人挂起
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		loger.Loger.Error("[AI]请求失败！", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	Dresp, err := io.ReadAll(resp.Body)

	loger.Loger.Debug("[Ai]底层网络响应", zap.String("Response", string(Dresp)))

	if resp.StatusCode != http.StatusOK {
		loger.Loger.Error("[Ai]请求被拒绝或服务器异常",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(Dresp)),
		)
		return
	}

	if len(Dresp) == 0 {
		loger.Loger.Error("[Ai]接口返回了空的响应体")
		return
	}

	err = json.Unmarshal(Dresp, &Jresp)
	if err != nil {
		loger.Loger.Error("[Ai]无法反序列化JSON", zap.Error(err), zap.String("body", string(Dresp)))
		return
	}

	return Jresp
}