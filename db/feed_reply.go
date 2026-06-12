package db

import (
	"context"
	"xhhrobot/loger"
	"xhhrobot/pg"
	"xhhrobot/sqlite"

	"go.uber.org/zap"
)

type FeedReplyRecord struct {
	LinkID        int64
	Title         string
	PostContent   string
	Status        string
	ReplyContent  string
	RepliedAt     int64
	MainTokens    int
	VisionTokens  int
}

func MigrateFeedReplyTable() {
	ctx := context.Background()
	if cfg.Type == "pg" {
		_, err := pg.Conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS feed_reply_records (
			link_id BIGINT PRIMARY KEY, title TEXT, post_content TEXT, status TEXT, reply_content TEXT, replied_at BIGINT, main_tokens INT DEFAULT 0, vision_tokens INT DEFAULT 0
		)`)
		if err != nil {
			loger.Loger.Warn("[DB]无法创建自动刷帖记录表", zap.Error(err))
		}
	} else if cfg.Type == "sqlite" {
		_, err := sqlite.Db.Exec(`CREATE TABLE IF NOT EXISTS feed_reply_records (
			link_id BIGINT PRIMARY KEY, title TEXT, post_content TEXT, status TEXT, reply_content TEXT, replied_at BIGINT, main_tokens INT DEFAULT 0, vision_tokens INT DEFAULT 0
		)`)
		if err != nil {
			loger.Loger.Warn("[DB]无法创建自动刷帖记录表", zap.Error(err))
		}
		// 兼容旧表：补充新增列
		sqlite.Db.Exec(`ALTER TABLE feed_reply_records ADD COLUMN reply_content TEXT`)
		sqlite.Db.Exec(`ALTER TABLE feed_reply_records ADD COLUMN post_content TEXT`)
		sqlite.Db.Exec(`ALTER TABLE feed_reply_records ADD COLUMN main_tokens INT DEFAULT 0`)
		sqlite.Db.Exec(`ALTER TABLE feed_reply_records ADD COLUMN vision_tokens INT DEFAULT 0`)
	}
}

func FeedReplyRecordExists(linkID int64) bool {
	ctx := context.Background()
	var exists bool
	if cfg.Type == "pg" {
		pg.Conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM feed_reply_records WHERE link_id=$1)", linkID).Scan(&exists)
	} else if cfg.Type == "sqlite" {
		sqlite.Db.QueryRow("SELECT EXISTS(SELECT 1 FROM feed_reply_records WHERE link_id=?)", linkID).Scan(&exists)
	}
	return exists
}

func FeedReplyAttemptsSince(since int64) int {
	ctx := context.Background()
	var count int
	if cfg.Type == "pg" {
		pg.Conn.QueryRow(ctx, "SELECT COUNT(*) FROM feed_reply_records WHERE replied_at >= $1 AND status IN ('sent','dry_run')", since).Scan(&count)
	} else if cfg.Type == "sqlite" {
		sqlite.Db.QueryRow("SELECT COUNT(*) FROM feed_reply_records WHERE replied_at >= ? AND status IN ('sent','dry_run')", since).Scan(&count)
	}
	return count
}

type FeedReplyWebRecord struct {
	LinkID       int64  `json:"link_id"`
	Title        string `json:"title"`
	PostContent  string `json:"post_content"`
	Status       string `json:"status"`
	ReplyContent string `json:"reply_content"`
	RepliedAt    int64  `json:"replied_at"`
	MainTokens   int    `json:"main_tokens"`
	VisionTokens int    `json:"vision_tokens"`
}

func GetFeedReplyRecords() []FeedReplyWebRecord {
	ctx := context.Background()
	if cfg.Type == "pg" {
		rows, err := pg.Conn.Query(ctx, "SELECT link_id,title,COALESCE(post_content,''),status,COALESCE(reply_content,''),replied_at,COALESCE(main_tokens,0),COALESCE(vision_tokens,0) FROM feed_reply_records WHERE status != 'skipped' ORDER BY replied_at DESC LIMIT 100")
		if err != nil {
			loger.Loger.Warn("[DB]查询刷帖记录失败", zap.Error(err))
			return nil
		}
		defer rows.Close()
		var recs []FeedReplyWebRecord
		for rows.Next() {
			var r FeedReplyWebRecord
			rows.Scan(&r.LinkID, &r.Title, &r.PostContent, &r.Status, &r.ReplyContent, &r.RepliedAt, &r.MainTokens, &r.VisionTokens)
			recs = append(recs, r)
		}
		return recs
	}
	rows, err := sqlite.Db.Query("SELECT link_id,title,COALESCE(post_content,''),status,COALESCE(reply_content,''),replied_at,COALESCE(main_tokens,0),COALESCE(vision_tokens,0) FROM feed_reply_records WHERE status != 'skipped' ORDER BY replied_at DESC LIMIT 100")
	if err != nil {
		loger.Loger.Warn("[DB]查询刷帖记录失败", zap.Error(err))
		return nil
	}
	defer rows.Close()
	var recs []FeedReplyWebRecord
	for rows.Next() {
		var r FeedReplyWebRecord
		rows.Scan(&r.LinkID, &r.Title, &r.PostContent, &r.Status, &r.ReplyContent, &r.RepliedAt, &r.MainTokens, &r.VisionTokens)
		recs = append(recs, r)
	}
	return recs
}

func SaveFeedReplyRecord(record FeedReplyRecord) {
	ctx := context.Background()
	if cfg.Type == "pg" {
		pg.Conn.Exec(ctx, `INSERT INTO feed_reply_records (link_id,title,post_content,status,reply_content,replied_at,main_tokens,vision_tokens) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (link_id) DO NOTHING`,
			record.LinkID, record.Title, record.PostContent, record.Status, record.ReplyContent, record.RepliedAt, record.MainTokens, record.VisionTokens)
	} else if cfg.Type == "sqlite" {
		sqlite.Db.Exec(`INSERT INTO feed_reply_records (link_id,title,post_content,status,reply_content,replied_at,main_tokens,vision_tokens) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT (link_id) DO NOTHING`,
			record.LinkID, record.Title, record.PostContent, record.Status, record.ReplyContent, record.RepliedAt, record.MainTokens, record.VisionTokens)
	}
}
