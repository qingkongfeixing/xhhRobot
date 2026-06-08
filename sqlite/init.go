package sqlite

import (
	"database/sql"
	"xhhrobot/loger"

	_ "modernc.org/sqlite"
	"go.uber.org/zap"
)

var Db *sql.DB

func Init() {
	var err error
	Db, err = sql.Open("sqlite", "./sql.db")
	if err != nil {
		loger.Loger.Fatal("[SQLite]无法读取文件", zap.Error(err))
	}

	// 【新增防死锁机制】：开启 WAL 模式和 5 秒繁忙等待时间
	Db.Exec("PRAGMA journal_mode=WAL;")
	Db.Exec("PRAGMA busy_timeout=5000;")

	_, err = Db.Exec(`
	CREATE TABLE IF NOT EXISTS at (
	msg_id BIGINT PRIMARY KEY,
	comment_a_id BIGINT,
	comment_root_id BIGINT,
	link_id BIGINT,
	user_a_id BIGINT,
	comment_text TEXT,
	reply boolean
	)
	`)
	if err != nil {
		loger.Loger.Fatal("[Sqlite]无法创建新的数据库", zap.Error(err))
	}
	err = Db.Ping()
	if err != nil {
		loger.Loger.Fatal("[Sqlite]无法连接至新的数据库", zap.Error(err))
	}
	// 之前你加的：自动为旧数据表添加 user_name 列
	Db.Exec("ALTER TABLE at ADD COLUMN user_name TEXT DEFAULT ''")
	// 【新增】：自动为数据表添加 reply_content 列，用来保存塔菲的回复内容
	Db.Exec("ALTER TABLE at ADD COLUMN reply_content TEXT DEFAULT ''")
	// 【核心新增】：自动为数据表添加 created_at 列，用来保存记录的时间戳
	Db.Exec("ALTER TABLE at ADD COLUMN created_at BIGINT DEFAULT 0")
	// 【本次新增】：自动为数据表添加 link_title 列，用来保存帖子标题
	Db.Exec("ALTER TABLE at ADD COLUMN link_title TEXT DEFAULT ''")
	// 【真实算力】：自动为数据表添加精确的 token 记录列
	Db.Exec("ALTER TABLE at ADD COLUMN main_tokens BIGINT DEFAULT 0")
	Db.Exec("ALTER TABLE at ADD COLUMN vision_tokens BIGINT DEFAULT 0")
	Db.Exec("ALTER TABLE at ADD COLUMN replied_at BIGINT DEFAULT 0")
	Db.Exec("ALTER TABLE at ADD COLUMN retry_count INTEGER DEFAULT 0")

	loger.Loger.Info("[SQLite]READY!")
	loger.Loger.Info("[SQLite]READY!")
}