package config

import (
	"encoding/json"
	"os"
	"xhhrobot/loger"
)

var ConfigStruct struct {
	Xhh struct {
		CheckTime int    `json:"checkTime"`
		ReplyTime int    `json:"replyTime"`
		Owner     string `json:"owner"`
		DeviceID  string `json:"deviceID"`
		BaseUrl   string `json:"baseUrl"`
		WebVer    string `json:"webver"`
		Ver       string `json:"version"`
		BannedWords map[string]string `json:"banned_words"`
		Blacklist      string `json:"blacklist"`
		ReplyOnlyOwner bool `json:"reply_only_owner"`
		ReplyStartHour      int               `json:"replyStartHour"`
		ReplyEndHour        int               `json:"replyEndHour"`
		ReplyIntervalSeconds int             `json:"replyIntervalSeconds"`
		TypingJitterMax     int             `json:"typingJitterMax"`
	} `json:"xhh"`
	DataBase struct {
		Type   string `json:"type"`
		Db     string `json:"db"`
		Host   string `json:"host"`
		Port   string `json:"port"`
		User   string `json:"user"`
		Passwd string `json:"passwd"`
	} `json:"database"`
	Ai struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		BaseUrl string `json:"baseUrl"`
		Token   string `json:"token"`
		VisionModel         string `json:"vision_model"`
		VisionPrompt        string `json:"vision_prompt"`
		VisionBaseUrl       string `json:"vision_base_url"`
		VisionToken         string `json:"vision_token"`
		VisionFallbackModel    string `json:"vision_fallback_model"`
		VisionFallbackBaseUrl  string `json:"vision_fallback_base_url"`
		VisionFallbackToken    string `json:"vision_fallback_token"`
		VisionMode          string `json:"vision_mode"`
		EnableVision bool   `json:"enable_vision"`
		EnableSearch bool   `json:"enable_search"`
		EnableThinking bool `json:"enable_thinking"`
		ReasoningEffort string `json:"reasoning_effort"`
		EnableSearchExt bool  `json:"enable_search_extension"`
		MaxPostImages    int `json:"max_post_images"`
		MaxCommentImages int `json:"max_comment_images"`
	} `json:"ai"`
	Fallback struct {
		Enabled             bool   `json:"enabled"`
		CheckDelaySeconds   int    `json:"checkDelaySeconds"`
		CookieFile          string `json:"cookieFile"`
		DeviceID            string `json:"deviceID"`
		Prompt              string `json:"prompt"`
		MainCooldownMinutes int    `json:"mainCooldownMinutes"`
	} `json:"fallback"`
	FeedReply struct {
		Enabled   bool   `json:"enabled"`
		StartHour int    `json:"startHour"`
		EndHour   int    `json:"endHour"`
		Interval  int    `json:"interval"`
		MaxPerRun int    `json:"maxPerRun"`
		MaxPerDay int    `json:"maxPerDay"`
		DryRun       bool   `json:"dryRun"`
			MinPostWords int    `json:"minPostWords"`
			MaxPostWords int    `json:"maxPostWords"`
			SummaryMode  bool   `json:"summaryMode"`
	} `json:"feedReply"`
}

func InitConfig() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	file, err := os.ReadFile(wd + "/config.json")
	if err != nil {
		if os.IsNotExist(err) {
			Data, err := json.Marshal(ConfigStruct)
			if err != nil {
				panic(err)
			}
			os.WriteFile("./config.json", Data, 0644)
			loger.Loger.Fatal("请修改配置文件后重新启动")
		}
		panic(err)
	}
	err = json.Unmarshal(file, &ConfigStruct)
	if err != nil {
		panic(err)
	}

	if ConfigStruct.Xhh.ReplyIntervalSeconds <= 0 {
		ConfigStruct.Xhh.ReplyIntervalSeconds = 15
	}
	if ConfigStruct.Xhh.TypingJitterMax <= 0 {
		ConfigStruct.Xhh.TypingJitterMax = 4
	}

	if ConfigStruct.Ai.VisionMode == "" {
		ConfigStruct.Ai.VisionMode = "dual"
	}

	if ConfigStruct.Fallback.CheckDelaySeconds <= 0 {
		ConfigStruct.Fallback.CheckDelaySeconds = 12
	}
	if ConfigStruct.Fallback.CookieFile == "" {
		ConfigStruct.Fallback.CookieFile = "cookie2.json"
	}
	if ConfigStruct.Fallback.DeviceID == "" {
		ConfigStruct.Fallback.DeviceID = ConfigStruct.Xhh.DeviceID
	}
	if ConfigStruct.Fallback.MainCooldownMinutes <= 0 {
		ConfigStruct.Fallback.MainCooldownMinutes = 360
	}

	if ConfigStruct.FeedReply.Interval == 0 {
		ConfigStruct.FeedReply.Interval = 900
		ConfigStruct.FeedReply.MaxPerRun = 1
		ConfigStruct.FeedReply.MaxPerDay = 10
		ConfigStruct.FeedReply.DryRun = true
		ConfigStruct.FeedReply.StartHour = 8
		ConfigStruct.FeedReply.EndHour = 23
		ConfigStruct.FeedReply.MinPostWords = 0
		ConfigStruct.FeedReply.MaxPostWords = 0
		ConfigStruct.FeedReply.SummaryMode = false
	}

	loger.Loger.Info("[CFG]Init OK")
}
