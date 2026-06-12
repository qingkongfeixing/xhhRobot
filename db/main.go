package db

import (
	"context"
	"fmt"
	"strings"
	"time"
	"xhhrobot/config"
	"xhhrobot/loger"
	"xhhrobot/pg"
	"xhhrobot/sqlite"

	"go.uber.org/zap"
)

var cfg = &config.ConfigStruct.DataBase

func Init() {
	switch cfg.Type {
	case "pg":
		pg.InitPostgreSQL()
		MigrateFeedReplyTable()
		MigrateAtRepliedAt()
			MigrateAtFallbackReply()
			MigrateAtFallbackReplyContent()
			MigrateAtRetryCount()
		return
	case "sqlite":
		sqlite.Init()
		MigrateFeedReplyTable()
			MigrateAtFallbackReply()
			MigrateAtFallbackReplyContent()
			MigrateAtRetryCount()
		return
	default:
		loger.Loger.Fatal("[DB]无效的数据库类型")
	}
}

func MigrateAtFallbackReply() {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "ALTER TABLE at ADD COLUMN IF NOT EXISTS fallback_reply BOOLEAN DEFAULT FALSE")
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec("ALTER TABLE at ADD COLUMN fallback_reply INTEGER DEFAULT 0")
	}
}

func MigrateAtFallbackReplyContent() {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "ALTER TABLE at ADD COLUMN IF NOT EXISTS fallback_reply_content TEXT DEFAULT ''")
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec("ALTER TABLE at ADD COLUMN fallback_reply_content TEXT DEFAULT ''")
	}
}

func MigrateAtRetryCount() {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "ALTER TABLE at ADD COLUMN IF NOT EXISTS retry_count INTEGER DEFAULT 0")
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec("ALTER TABLE at ADD COLUMN retry_count INTEGER DEFAULT 0")
	}
}

func MigrateAtRepliedAt() {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "ALTER TABLE at ADD COLUMN IF NOT EXISTS replied_at BIGINT DEFAULT 0")
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec("ALTER TABLE at ADD COLUMN replied_at BIGINT DEFAULT 0")
	}
}

func Insert(msg_id, comment_a_id, comment_root_id, link_id, user_a_id int, user_name string, comment_text string, reply bool) bool {
	ctx := context.Background()
	now := time.Now().Unix()

	if cfg.Type == "pg" {
		_, err := pg.Conn.Exec(ctx, "INSERT INTO at (msg_id,comment_a_id,comment_root_id,link_id,user_a_id,user_name,comment_text,reply,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (msg_id) DO NOTHING", msg_id, comment_a_id, comment_root_id, link_id, user_a_id, user_name, comment_text, reply, now)
		if err != nil {
			loger.Loger.Info("[DB]PsqlError", zap.Error(err))
			return false
		}
		return true
	}
	if cfg.Type == "sqlite" {
		_, err := sqlite.Db.Exec("INSERT INTO at (msg_id,comment_a_id,comment_root_id,link_id,user_a_id,user_name,comment_text,reply,created_at) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (msg_id) DO NOTHING", msg_id, comment_a_id, comment_root_id, link_id, user_a_id, user_name, comment_text, reply, now)
		if err != nil {
			loger.Loger.Info("[DB]SQLiteERROR", zap.Error(err))
			return false
		}
	}
	return false
}

func Replyed(msg_id int, reply_content string, main_tokens int, vision_tokens int) {
	ctx := context.Background()
	now := time.Now().Unix()
	if cfg.Type == "pg" {
		_, err := pg.Conn.Exec(ctx, "UPDATE at SET reply=$1, reply_content=$2, main_tokens=$3, vision_tokens=$4, replied_at=$5 WHERE msg_id=$6", true, reply_content, main_tokens, vision_tokens, now, msg_id)
		if err != nil {
			loger.Loger.Error("[DB]Replyed pg update failed", zap.Error(err), zap.Int("msg_id", msg_id))
		}
		return
	}
	if cfg.Type == "sqlite" {
		result, err := sqlite.Db.Exec("UPDATE at SET reply=?, reply_content=?, main_tokens=?, vision_tokens=?, replied_at=? WHERE msg_id=?", true, reply_content, main_tokens, vision_tokens, now, msg_id)
		if err != nil {
			loger.Loger.Error("[DB]Replyed sqlite update failed", zap.Error(err), zap.Int("msg_id", msg_id))
		} else {
			rows, _ := result.RowsAffected()
			loger.Loger.Debug("[DB]Replyed success", zap.Int("msg_id", msg_id), zap.Int64("rows", rows))
		}
	}
}

func MarkFallbackReply(msg_id int, content string) {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "UPDATE at SET fallback_reply=$1, fallback_reply_content=$2 WHERE msg_id=$3", true, content, msg_id)
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec("UPDATE at SET fallback_reply=?, fallback_reply_content=? WHERE msg_id=?", 1, content, msg_id)
	}
	loger.Loger.Info("[DB]备用账号接替回复已记录", zap.Int("msg_id", msg_id))
}

// MarkReplyFailed 回复失败时递增重试次数，超过上限则标记为已处理放弃重试
const MaxRetryCount = 3

func MarkReplyFailed(msg_id int) {
	ctx := context.Background()
	if cfg.Type == "pg" {
		var count int
		err := pg.Conn.QueryRow(ctx, "UPDATE at SET retry_count = retry_count + 1 WHERE msg_id=$1 RETURNING retry_count", msg_id).Scan(&count)
		if err != nil {
			loger.Loger.Error("[DB]MarkReplyFailed pg update failed", zap.Error(err), zap.Int("msg_id", msg_id))
			return
		}
		if count >= MaxRetryCount {
			now := time.Now().Unix()
			pg.Conn.Exec(ctx, "UPDATE at SET reply=$1, reply_content=$2, replied_at=$3 WHERE msg_id=$4", true, "[系统] 重试次数超限，已放弃", now, msg_id)
			loger.Loger.Warn("[DB]回复失败已达上限，标记为已处理", zap.Int("msg_id", msg_id), zap.Int("retry_count", count))
		} else {
			loger.Loger.Info("[DB]回复失败，等待重试", zap.Int("msg_id", msg_id), zap.Int("retry_count", count))
		}
		return
	}
	if cfg.Type == "sqlite" {
		_, err := sqlite.Db.Exec("UPDATE at SET retry_count = retry_count + 1 WHERE msg_id=?", msg_id)
		if err != nil {
			loger.Loger.Error("[DB]MarkReplyFailed sqlite update failed", zap.Error(err), zap.Int("msg_id", msg_id))
			return
		}
		var count int
		sqlite.Db.QueryRow("SELECT retry_count FROM at WHERE msg_id=?", msg_id).Scan(&count)
		if count >= MaxRetryCount {
			now := time.Now().Unix()
			sqlite.Db.Exec("UPDATE at SET reply=?, reply_content=?, replied_at=? WHERE msg_id=?", true, "[系统] 重试次数超限，已放弃", now, msg_id)
			loger.Loger.Warn("[DB]回复失败已达上限，标记为已处理", zap.Int("msg_id", msg_id), zap.Int("retry_count", count))
		} else {
			loger.Loger.Info("[DB]回复失败，等待重试", zap.Int("msg_id", msg_id), zap.Int("retry_count", count))
		}
	}
}

type CommStruct struct {
	LinkID    int
	CommentID int
	RootID    int
	Text      string
	Uid       int
	MsgID     int
}

func GetComm(owners string) (CommArr []CommStruct) {
	// 过滤掉非数字的 owner 值（防止占位符文本导致 SQL 报错）
	var validOwners []string
	for _, o := range strings.Split(owners, ",") {
		o = strings.TrimSpace(o)
		if _, err := fmt.Sscanf(o, "%d", new(int)); err == nil {
			validOwners = append(validOwners, o)
		}
	}
	if len(validOwners) == 0 {
		validOwners = []string{"0"}
	}
	owners = strings.Join(validOwners, ",")
	ctx := context.Background()
	query := fmt.Sprintf("SELECT link_id,comment_a_id,comment_root_id,comment_text,user_a_id,msg_id FROM at WHERE reply=false ORDER BY (user_a_id IN (%s)) DESC, msg_id ASC LIMIT 3", owners)

	if cfg.Type == "pg" {
		row, err := pg.Conn.Query(ctx, query)
		if err != nil {
			loger.Loger.Error("[DB]无法获取评论信息", zap.Error(err))
			return
		}
		defer row.Close()
		for row.Next() {
			var Comm CommStruct
			if err := row.Scan(&Comm.LinkID, &Comm.CommentID, &Comm.RootID, &Comm.Text, &Comm.Uid, &Comm.MsgID); err != nil {
				loger.Loger.Error("[DB]GetComm扫描失败(pg)", zap.Error(err))
				continue
			}
			CommArr = append(CommArr, Comm)
		}
		return
	}
	if cfg.Type == "sqlite" {
		row, err := sqlite.Db.Query(query)
		if err != nil {
			loger.Loger.Error("[DB]无法获取评论信息", zap.Error(err))
			return
		}
		defer row.Close()
		for row.Next() {
			var Comm CommStruct
			if err := row.Scan(&Comm.LinkID, &Comm.CommentID, &Comm.RootID, &Comm.Text, &Comm.Uid, &Comm.MsgID); err != nil {
				loger.Loger.Error("[DB]GetComm扫描失败(sqlite)", zap.Error(err))
				continue
			}
			CommArr = append(CommArr, Comm)
		}
	}
	return
}

func IsNew() bool {
	ctx := context.Background()
	var num int
	if cfg.Type == "pg" {
		row := pg.Conn.QueryRow(ctx, "SELECT COUNT(*) FROM at")
		if err := row.Scan(&num); err != nil {
			loger.Loger.Error("[DB]IsNew查询失败(pg)", zap.Error(err))
			return false
		}
	}
	if cfg.Type == "sqlite" {
		row := sqlite.Db.QueryRow("SELECT COUNT(*) FROM at")
		if err := row.Scan(&num); err != nil {
			loger.Loger.Error("[DB]IsNew查询失败(sqlite)", zap.Error(err))
			return false
		}
	}
	return num == 0
}

func UpdateLinkTitle(link_id int, title string) {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, "UPDATE at SET link_title=$1 WHERE link_id=$2", title, link_id)
	}
	if cfg.Type == "sqlite" {
		sqlite.Db.Exec("UPDATE at SET link_title=? WHERE link_id=?", title, link_id)
	}
}

type StatsResult struct {
	Replies      int `json:"replies"`
	MainTokens   int `json:"mainTokens"`
	VisionTokens int `json:"visionTokens"`
}

func GetStats(period string, customDateStr string) StatsResult {
	var res StatsResult
	var startTime, endTime int64
	now := time.Now()

	endTime = now.AddDate(1, 0, 0).Unix()

	switch period {
	case "day":
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		startTime = t.Unix()
	case "week":
		offset := int(now.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		startTime = t.AddDate(0, 0, -offset).Unix()
	case "month":
		t := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		startTime = t.Unix()
	case "custom":
		if customDateStr == "" {
			return res
		}
		t, err := time.ParseInLocation("2006-01-02", customDateStr, now.Location())
		if err != nil {
			return res
		}
		startTime = t.Unix()
		endTime = t.AddDate(0, 0, 1).Unix()
	default:
		return res
	}

	query := "SELECT COUNT(*), COALESCE(SUM(main_tokens), 0), COALESCE(SUM(vision_tokens), 0) FROM at WHERE reply=true AND (reply_content NOT LIKE '【系统提示：%' OR reply_content IS NULL OR reply_content = '') AND created_at >= ? AND created_at < ?"
	if cfg.Type == "pg" {
		query = "SELECT COUNT(*), COALESCE(SUM(main_tokens), 0), COALESCE(SUM(vision_tokens), 0) FROM at WHERE reply=true AND (reply_content NOT LIKE '【系统提示：%' OR reply_content IS NULL OR reply_content = '') AND created_at >= $1 AND created_at < $2"
		if err := pg.Conn.QueryRow(context.Background(), query, startTime, endTime).Scan(&res.Replies, &res.MainTokens, &res.VisionTokens); err != nil {
			loger.Loger.Error("[DB]GetStats查询失败(pg)", zap.Error(err))
		}
	} else if cfg.Type == "sqlite" {
		if err := sqlite.Db.QueryRow(query, startTime, endTime).Scan(&res.Replies, &res.MainTokens, &res.VisionTokens); err != nil {
			loger.Loger.Error("[DB]GetStats查询失败(sqlite)", zap.Error(err))
		}
	}
	return res
}

type WebMsg struct {
	MsgID        int    `json:"msg_id"`
	LinkID       int    `json:"link_id"`
	LinkTitle    string `json:"link_title"`
	UserName     string `json:"user_name"`
	UserID       int    `json:"user_id"`
	CommentText  string `json:"comment_text"`
	ReplyContent string `json:"reply_content"`
	ReplyStatus  bool   `json:"reply_status"`
	Time         int64  `json:"time"`
	ReplyTime    int64  `json:"reply_time"`
	FallbackReply        bool   `json:"fallback_reply"`
	FallbackReplyContent string `json:"fallback_reply_content"`
}

func GetAllWebMsgs() []WebMsg {
	ctx := context.Background()
	var rows interface{ Next() bool; Scan(...interface{}) error }
	var err error

	query := "SELECT msg_id, link_id, link_title, user_name, user_a_id, comment_text, reply_content, reply, created_at, replied_at, fallback_reply, fallback_reply_content FROM at ORDER BY created_at DESC"

	var result []WebMsg

	if cfg.Type == "pg" {
		rows, err = pg.Conn.Query(ctx, query)
	} else {
		rows, err = sqlite.Db.Query(query)
	}
	if err != nil {
		loger.Loger.Error("[DB] GetAllWebMsgs query failed", zap.Error(err))
		return result
	}
	defer func() {
		if rows != nil {
			rows.(interface{ Close() error }).Close()
		}
	}()

	for rows.Next() {
		var m WebMsg
		var timeUnix, replyTimeUnix int64
		if cfg.Type == "pg" {
			err = rows.(interface{ Scan(...interface{}) error }).Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent)
		} else {
			err = rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent)
		}
		if err != nil {
			loger.Loger.Error("[DB] Scan error", zap.Error(err))
			continue
		}
		m.Time = timeUnix
		m.ReplyTime = replyTimeUnix
		result = append(result, m)
	}
	return result
}

func ResetReply(msgId int) bool {
	ctx := context.Background()
	if cfg.Type == "pg" {
		_, err := pg.Conn.Exec(ctx, "UPDATE at SET reply=false, reply_content='' WHERE msg_id=$1", msgId)
		return err == nil
	}
	if cfg.Type == "sqlite" {
		_, err := sqlite.Db.Exec("UPDATE at SET reply=false, reply_content='' WHERE msg_id=?", msgId)
		return err == nil
	}
	return false
}

// GetWebMsgsPaged 按创建时间倒序分页获取消息
func GetWebMsgsPaged(limit, offset int) []WebMsg {
	ctx := context.Background()
	query := fmt.Sprintf("SELECT msg_id, link_id, link_title, user_name, user_a_id, comment_text, reply_content, reply, created_at, replied_at, fallback_reply, fallback_reply_content FROM at ORDER BY created_at DESC LIMIT %d OFFSET %d", limit, offset)

	var result []WebMsg
	if cfg.Type == "pg" {
		rows, err := pg.Conn.Query(ctx, query)
		if err != nil {
			loger.Loger.Error("[DB] GetWebMsgsPaged pg error", zap.Error(err))
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	} else if cfg.Type == "sqlite" {
		rows, err := sqlite.Db.Query(query)
		if err != nil {
			loger.Loger.Error("[DB] GetWebMsgsPaged sqlite error", zap.Error(err))
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	}
	return result
}

// GetWebMsgsByIDs 按 msg_id 列表精确获取消息（用于状态同步）
func GetWebMsgsByIDs(ids []int) []WebMsg {
	if len(ids) == 0 {
		return []WebMsg{}
	}
	ctx := context.Background()

	// 构建占位符和参数
	args := make([]interface{}, len(ids))
	var placeholders []string
	for i, id := range ids {
		args[i] = id
		if cfg.Type == "pg" {
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		} else {
			placeholders = append(placeholders, "?")
		}
	}
	query := fmt.Sprintf("SELECT msg_id, link_id, link_title, user_name, user_a_id, comment_text, reply_content, reply, created_at, replied_at, fallback_reply, fallback_reply_content FROM at WHERE msg_id IN (%s)", strings.Join(placeholders, ","))

	var result []WebMsg
	if cfg.Type == "pg" {
		rows, err := pg.Conn.Query(ctx, query, args...)
		if err != nil {
			loger.Loger.Error("[DB] GetWebMsgsByIDs pg error", zap.Error(err))
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	} else if cfg.Type == "sqlite" {
		rows, err := sqlite.Db.Query(query, args...)
		if err != nil {
			loger.Loger.Error("[DB] GetWebMsgsByIDs sqlite error", zap.Error(err))
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	}
	return result
}

// GetNewWebMsgs 获取所有 msg_id > sinceMsgID 的消息（按时间升序，保证插入顺序）
func GetNewWebMsgs(sinceMsgID int) []WebMsg {
	ctx := context.Background()
	query := fmt.Sprintf("SELECT msg_id, link_id, link_title, user_name, user_a_id, comment_text, reply_content, reply, created_at, replied_at, fallback_reply, fallback_reply_content FROM at WHERE msg_id > %d ORDER BY created_at ASC", sinceMsgID)

	var result []WebMsg
	if cfg.Type == "pg" {
		rows, err := pg.Conn.Query(ctx, query)
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	} else if cfg.Type == "sqlite" {
		rows, err := sqlite.Db.Query(query)
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var m WebMsg
			var timeUnix, replyTimeUnix int64
			if err := rows.Scan(&m.MsgID, &m.LinkID, &m.LinkTitle, &m.UserName, &m.UserID, &m.CommentText, &m.ReplyContent, &m.ReplyStatus, &timeUnix, &replyTimeUnix, &m.FallbackReply, &m.FallbackReplyContent); err != nil {
				continue
			}
			m.Time = timeUnix
			m.ReplyTime = replyTimeUnix
			result = append(result, m)
		}
	}
	return result
}

type OverviewCounts struct {
	TotalPosts    int `json:"totalPosts"`
	TotalMessages int `json:"totalMessages"`
	SuccessReplies int `json:"successReplies"`
}

func GetOverviewCounts() OverviewCounts {
	ctx := context.Background()
	var c OverviewCounts
	queryPosts := "SELECT COUNT(DISTINCT link_id) FROM at"
	queryTotal := "SELECT COUNT(*) FROM at WHERE (reply_content NOT LIKE '【系统提示：%' OR reply_content IS NULL OR reply_content = '')"
	querySuccess := "SELECT COUNT(*) FROM at WHERE reply=true AND (reply_content NOT LIKE '【系统提示：%' OR reply_content IS NULL OR reply_content = '')"

	if cfg.Type == "pg" {
		if err := pg.Conn.QueryRow(ctx, queryPosts).Scan(&c.TotalPosts); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(pg)", zap.Error(err))
		}
		if err := pg.Conn.QueryRow(ctx, queryTotal).Scan(&c.TotalMessages); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(pg)", zap.Error(err))
		}
		if err := pg.Conn.QueryRow(ctx, querySuccess).Scan(&c.SuccessReplies); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(pg)", zap.Error(err))
		}
	} else {
		if err := sqlite.Db.QueryRow(queryPosts).Scan(&c.TotalPosts); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(sqlite)", zap.Error(err))
		}
		if err := sqlite.Db.QueryRow(queryTotal).Scan(&c.TotalMessages); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(sqlite)", zap.Error(err))
		}
		if err := sqlite.Db.QueryRow(querySuccess).Scan(&c.SuccessReplies); err != nil {
			loger.Loger.Error("[DB]GetOverviewCounts查询失败(sqlite)", zap.Error(err))
		}
	}
	return c
}

type FeedStatsResult struct {
	Total        int `json:"total"`
	Sent         int `json:"sent"`
	DryRun       int `json:"dryRun"`
	Skipped      int `json:"skipped"`
	Failed       int `json:"failed"`
	MainTokens   int `json:"mainTokens"`
	VisionTokens int `json:"visionTokens"`
}

func GetFeedStats(period string, customDateStr string) FeedStatsResult {
	var res FeedStatsResult
	var startTime, endTime int64
	now := time.Now()

	switch period {
	case "day":
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		startTime = t.Unix()
	case "week":
		offset := int(now.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		startTime = t.AddDate(0, 0, -offset).Unix()
	case "month":
		t := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		startTime = t.Unix()
	case "custom":
		if customDateStr == "" {
			return res
		}
		t, err := time.ParseInLocation("2006-01-02", customDateStr, now.Location())
		if err != nil {
			return res
		}
		startTime = t.Unix()
		endTime = t.AddDate(0, 0, 1).Unix()
	default:
		return res
	}

	if period != "custom" {
		endTime = now.AddDate(1, 0, 0).Unix()
	}

	type rowScanner interface {
		Scan(dest ...interface{}) error
	}

	var row rowScanner
	query := "SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='sent' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='dry_run' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='skipped' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0), COALESCE(SUM(main_tokens),0), COALESCE(SUM(vision_tokens),0) FROM feed_reply_records WHERE replied_at >= ? AND replied_at < ?"
	if cfg.Type == "pg" {
		query = "SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='sent' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='dry_run' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='skipped' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0), COALESCE(SUM(main_tokens),0), COALESCE(SUM(vision_tokens),0) FROM feed_reply_records WHERE replied_at >= $1 AND replied_at < $2"
		row = pg.Conn.QueryRow(context.Background(), query, startTime, endTime)
	} else if cfg.Type == "sqlite" {
		row = sqlite.Db.QueryRow(query, startTime, endTime)
	}
	if row != nil {
		if err := row.Scan(&res.Total, &res.Sent, &res.DryRun, &res.Skipped, &res.Failed, &res.MainTokens, &res.VisionTokens); err != nil {
			loger.Loger.Error("[DB]GetFeedStats查询失败", zap.Error(err))
		}
	}
	return res
}

type FeedOverviewCounts struct {
	TotalRecords int `json:"totalRecords"`
	SentRecords  int `json:"sentRecords"`
}

func GetFeedOverviewCounts() FeedOverviewCounts {
	ctx := context.Background()
	var c FeedOverviewCounts
	queryTotal := "SELECT COUNT(*) FROM feed_reply_records"
	querySent := "SELECT COUNT(*) FROM feed_reply_records WHERE status='sent'"

	if cfg.Type == "pg" {
		if err := pg.Conn.QueryRow(ctx, queryTotal).Scan(&c.TotalRecords); err != nil {
			loger.Loger.Error("[DB]GetFeedOverviewCounts查询失败(pg)", zap.Error(err))
		}
		if err := pg.Conn.QueryRow(ctx, querySent).Scan(&c.SentRecords); err != nil {
			loger.Loger.Error("[DB]GetFeedOverviewCounts查询失败(pg)", zap.Error(err))
		}
	} else {
		if err := sqlite.Db.QueryRow(queryTotal).Scan(&c.TotalRecords); err != nil {
			loger.Loger.Error("[DB]GetFeedOverviewCounts查询失败(sqlite)", zap.Error(err))
		}
		if err := sqlite.Db.QueryRow(querySent).Scan(&c.SentRecords); err != nil {
			loger.Loger.Error("[DB]GetFeedOverviewCounts查询失败(sqlite)", zap.Error(err))
		}
	}
	return c
}
