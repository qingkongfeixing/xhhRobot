package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"xhhrobot/ai"
	"xhhrobot/config"
	"xhhrobot/db"
	"xhhrobot/loger"
	"xhhrobot/xhh"

	"go.uber.org/zap"
)

const AdminUser = "admin"
const AdminPass = "admin123"
const AdminToken = "admin-token-change-me"

const GuestUser = "guest"
const GuestPass = "guest"
const GuestToken = "guest-token-change-me"

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != AdminToken && token != GuestToken {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func requireAdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != AdminToken {
			http.Error(w, `{"error": "Forbidden: Guest account cannot perform this action"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func StartServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "index.html")
	})

	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") == "" {
            http.Error(w, "Unauthorized", 401)
            return
        }
        period := r.URL.Query().Get("period")
        customDateStr := r.URL.Query().Get("date")
        if period == "" { period = "day" }
        stats := db.GetStats(period, customDateStr)

        // 附加全局计数
        overview := db.GetOverviewCounts()

        type combined struct {
            db.StatsResult
            db.OverviewCounts
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(combined{
            StatsResult: stats,
            OverviewCounts: overview,
        })
    })

	http.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Username == AdminUser && req.Password == AdminPass {
			json.NewEncoder(w).Encode(map[string]string{"success": "true", "token": AdminToken, "role": "admin"})
			loger.Loger.Info("[Web] 超级管理员登录成功")
		} else if req.Username == GuestUser && req.Password == GuestPass {
			json.NewEncoder(w).Encode(map[string]string{"success": "true", "token": GuestToken, "role": "guest"})
			loger.Loger.Info("[Web] 游客登录成功")
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"success": "false", "message": "账号或密码错误"})
			loger.Loger.Warn("[Web] 尝试登录失败", zap.String("user", req.Username))
		}
	})

	http.HandleFunc("/api/msgs", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
        query := r.URL.Query()

        // 按 msg_id 列表精确拉取（用于状态同步）
        if idsStr := query.Get("ids"); idsStr != "" {
            var ids []int
            for _, s := range strings.Split(idsStr, ",") {
                if id, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
                    ids = append(ids, id)
                }
            }
            msgs := db.GetWebMsgsByIDs(ids)
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(msgs)
            return
        }

        // 增量拉取新消息（用于轮询）
        if sinceStr := query.Get("since_id"); sinceStr != "" {
            sinceID, err := strconv.Atoi(sinceStr)
            if err != nil {
                http.Error(w, "Invalid since_id", http.StatusBadRequest)
                return
            }
            msgs := db.GetNewWebMsgs(sinceID)
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(msgs)
            return
        }

        // 分页参数
        limitStr := query.Get("limit")
        offsetStr := query.Get("offset")
        limit := 50 // 默认每页 50 条
        offset := 0

        if limitStr != "" {
            if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
                limit = v
            }
        }
        if offsetStr != "" {
            if v, err := strconv.Atoi(offsetStr); err == nil {
                offset = v
            }
        }

        msgs := db.GetWebMsgsPaged(limit, offset)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(msgs)
    }))

	http.HandleFunc("/api/retry", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		msgIdStr := r.URL.Query().Get("msg_id")
		msgId, err := strconv.Atoi(msgIdStr)
		if err != nil {
			http.Error(w, "Invalid msg_id", http.StatusBadRequest)
			return
		}
		success := db.ResetReply(msgId)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": success})
	}))

	http.HandleFunc("/api/feed-stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "Unauthorized", 401)
			return
		}
		period := r.URL.Query().Get("period")
		customDateStr := r.URL.Query().Get("date")
		if period == "" {
			period = "day"
		}
		stats := db.GetFeedStats(period, customDateStr)
		overview := db.GetFeedOverviewCounts()

		type combined struct {
			db.FeedStatsResult
			db.FeedOverviewCounts
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(combined{
			FeedStatsResult:   stats,
			FeedOverviewCounts: overview,
		})
	})

	http.HandleFunc("/api/feed-reply-records", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		recs := db.GetFeedReplyRecords()
		if recs == nil {
			recs = []db.FeedReplyWebRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(recs)
	}))

	http.HandleFunc("/api/feed-reply-test", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"message":"手动触发刷帖测试，查看后台日志"}`))
		go xhh.TriggerFeedReplyTest()
	}))

	http.HandleFunc("/api/shadow-test", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		linkID, _ := strconv.Atoi(r.URL.Query().Get("link_id"))
		rootID, _ := strconv.Atoi(r.URL.Query().Get("root_id"))
		search := r.URL.Query().Get("search")
		if linkID == 0 || rootID == 0 || search == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"error":"need link_id, root_id, search params"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"message":"触发子评论翻页测试，查看后台日志"}`))
		go func() {
			loger.Loger.Info("[Shadow]手动测试开始",
				zap.Int("link_id", linkID),
				zap.Int("root_id", rootID),
				zap.String("search", search))
			xhh.TestFetchMoreSubComments(linkID, rootID, search)
		}()
	}))

	http.HandleFunc("/api/restart", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
		loger.Loger.Warn("[Web] ⚠️ 管理员在网页端触发了系统重启指令！程序即将退出。")
		go func() {
			time.Sleep(1 * time.Second)
			os.Exit(0)
		}()
	}))

	http.HandleFunc("/api/config", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			data, err := os.ReadFile("config.json")
			if err != nil {
				http.Error(w, "Cannot read config.json", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
		} else if r.Method == http.MethodPost {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Cannot read body", http.StatusBadRequest)
				return
			}
			err = os.WriteFile("config.json", data, 0664)
			if err != nil {
				http.Error(w, "Cannot write config.json", http.StatusInternalServerError)
				return
			}
			config.InitConfig()
			db.ReInit()
			xhh.ReloadBlacklist()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":true}`))
		}
	}))

	http.HandleFunc("/api/qrcode", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		resp := xhh.SendReq("GET", "/account/get_qrcode_url/", nil, "")
		if resp == nil {
			http.Error(w, "Request failed", http.StatusInternalServerError)
			return
		}
		data, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))

	http.HandleFunc("/api/qrcheck", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		qrUrl := r.URL.Query().Get("qr_url")
		if qrUrl == "" {
			http.Error(w, "Empty qr_url", http.StatusBadRequest)
			return
		}

		parts := strings.Split(qrUrl, "https://api.xiaoheihe.cn/account/qr_login/?")
		if len(parts) < 2 {
			http.Error(w, "Invalid qr_url", http.StatusBadRequest)
			return
		}

		resp := xhh.SendReq("GET", "/account/qr_state/", nil, "?"+parts[1])
		if resp == nil {
			http.Error(w, "Request failed", http.StatusInternalServerError)
			return
		}

		data, _ := io.ReadAll(resp.Body)
		var resps struct {
			Result struct {
				Err      string `json:"error"`
				ErrMsg   string `json:"error_msg"`
				NickName string `json:"nickname"`
			} `json:"result"`
		}
		json.Unmarshal(data, &resps)

		if resps.Result.Err == "ok" {
			cookies := resp.Cookies()
			var cookieStr string
			if len(cookies) >= 2 {
				cookieStr = cookies[0].Name + "=" + cookies[0].Value + ";" + cookies[1].Name + "=" + cookies[1].Value
			}
			cookieStr += xhh.GetFuckingToken()

			xhh.Info.Cookie = cookieStr
			for _, v := range cookies {
				if v.Name == "user_heybox_id" {
					xhh.Info.HeyBoxId = v.Value
				}
			}
			xhh.Info.Time = int(time.Now().Unix())

			jData, _ := json.MarshalIndent(xhh.Info, "", "  ")
			os.WriteFile("./cookie.json", jData, 0775)

			loger.Loger.Info("[Web] 扫码登录成功！Cookie 已热更新。")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))

	// 备用账号扫码登录
	http.HandleFunc("/api/qrcode2", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		resp := xhh.SendReq("GET", "/account/get_qrcode_url/", nil, "")
		if resp == nil {
			http.Error(w, "Request failed", http.StatusInternalServerError)
			return
		}
		data, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))

	http.HandleFunc("/api/qrcheck2", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		qrUrl := r.URL.Query().Get("qr_url")
		if qrUrl == "" {
			http.Error(w, "Empty qr_url", http.StatusBadRequest)
			return
		}

		parts := strings.Split(qrUrl, "https://api.xiaoheihe.cn/account/qr_login/?")
		if len(parts) < 2 {
			http.Error(w, "Invalid qr_url", http.StatusBadRequest)
			return
		}

		resp := xhh.SendReq("GET", "/account/qr_state/", nil, "?"+parts[1])
		if resp == nil {
			http.Error(w, "Request failed", http.StatusInternalServerError)
			return
		}

		data, _ := io.ReadAll(resp.Body)
		var resps struct {
			Result struct {
				Err      string `json:"error"`
				ErrMsg   string `json:"error_msg"`
				NickName string `json:"nickname"`
			} `json:"result"`
		}
		json.Unmarshal(data, &resps)

		if resps.Result.Err == "ok" {
			cookies := resp.Cookies()
			var cookieStr string
			if len(cookies) >= 2 {
				cookieStr = cookies[0].Name + "=" + cookies[0].Value + ";" + cookies[1].Name + "=" + cookies[1].Value
			}
			cookieStr += xhh.GetFuckingToken()

			xhh.FallbackInfo.Cookie = cookieStr
			for _, v := range cookies {
				if v.Name == "user_heybox_id" {
					xhh.FallbackInfo.HeyBoxId = v.Value
				}
			}
			xhh.FallbackInfo.Time = int(time.Now().Unix())

			jData, _ := json.MarshalIndent(xhh.FallbackInfo, "", "  ")
			os.WriteFile("./cookie2.json", jData, 0775)

			loger.Loger.Info("[Web] 备用账号扫码登录成功！Cookie2 已保存。")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))

	http.HandleFunc("/api/test-ai", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, `{"error":"Failed to parse form"}`, http.StatusBadRequest)
			return
		}

		title := r.FormValue("title")
		contentText := r.FormValue("content")
		mode := r.FormValue("mode")
		userSay := r.FormValue("user_say")

		var contents []ai.Content
		if strings.TrimSpace(contentText) != "" {
			contents = append(contents, ai.Content{Type: "text", Text: contentText})
		}
		if strings.TrimSpace(title) != "" && strings.TrimSpace(contentText) == "" {
			contents = append(contents, ai.Content{Type: "text", Text: title})
		}

		files := r.MultipartForm.File["images"]
		for _, fh := range files {
			file, err := fh.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				continue
			}
			mimeType := http.DetectContentType(data)
			if !strings.HasPrefix(mimeType, "image/") {
				continue
			}
			b64 := base64.StdEncoding.EncodeToString(data)
			dataUri := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
			contents = append(contents, ai.Content{
				Type: "image_url",
				ImgUrl: struct {
					Url string `json:"url"`
				}{Url: dataUri},
			})
		}

		if len(contents) == 0 {
			http.Error(w, `{"error":"No content provided"}`, http.StatusBadRequest)
			return
		}

		var parentText string
		var uid int
		if mode == "feed" {
			parentText = "【自动刷帖】"
		} else {
			parentText = "【单人模式】"
			uid = 1
		}

		loger.Loger.Info("[Web] ============ 测试 ============")
		result := ai.GetAiReply(contents, userSay, nil, nil, uid, 0, parentText, "")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"reply":         result.Text,
			"main_tokens":   result.MainTokens,
			"vision_tokens": result.VisionTokens,
			"total_tokens":  result.MainTokens + result.VisionTokens,
			"vision_desc":   result.VisionDesc,
			"reasoning":     result.Reasoning,
		})
	}))

	http.HandleFunc("/api/logs", requireAdminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		data := loger.WebConsole.String()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(data))
	}))

	loger.Loger.Info("[Web] 🌐 前端控制台已启动! 请在浏览器访问 http://localhost:8080/")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		loger.Loger.Fatal("[Web] 启动失败", zap.Error(err))
	}
}