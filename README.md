# xhhRobot

小黑盒（Xiaoheihe）社区 AI 自动回复机器人。监控 @-mention 并调用 OpenAI 兼容 API 生成评论，同时支持首页帖子自动评论。

## 功能

### 核心功能
- **@-回复** — 轮询小黑盒消息列表，检测 @-mentions（type 16），调用 AI 生成回复并自动评论
- **帖子回复** — 定时拉取首页 Feed 流，AI 判断帖子内容后自动评论（可选 dry-run 试运行）
- **Web 控制台** — 单文件 Vue 3 SPA，支持消息查看、统计面板、在线配置、扫码登录、AI 测试、日志查看
- **双账号容灾** — 主号被限制评论时自动切换备用账号，支持独立的备用 AI 人格（如呆猫）
- **影子评论检测** — 回复后异步检测评论是否被吞（影子封禁），自动用备用账号补发

### AI 能力
- **多模态视觉** — 自动提取帖子/评论中的图片，调用视觉模型生成描述注入上下文
- **联网搜索** — 支持 DeepSeek 风格联网搜索，自动查梗、查黑话
- **深度思考** — 支持 reasoning/thinking 模式，可配置推理强度
- **双模型流水线** — 视觉模型和主脑模型可分别配置不同的 API 端点和 Token
- **智能 @ 系统** — AI 输出 `@召唤者`/`@层主`/`@帖主` 占位符，后处理为小黑盒 HTML `<a>` 蓝字标签

### 防风控
- **违禁词替换** — 可配置敏感词映射表，自动替换为安全表述
- **回复间隔控制** — 互斥锁 + 可配置间隔，防止并发爆回复
- **时间窗口** — 可配置回复时段（如 7:00~次日 3:00），窗口外暂停
- **模拟打字延迟** — 根据回复字数计算随机延迟（3~16 秒）
- **验证码冷却** — 触发验证码后全局冷却 10 分钟，避免频繁请求
- **账号限制检测** — 检测"账号异常""禁言"等关键词，自动进入冷却期并切换备用号

## 架构

```
main.go
├── loger.InitLog()          # zap 日志 + Web 控制台 stdout 捕获
├── config.InitConfig()      # 读取 config.json，应用默认值
├── db.Init()                # SQLite / PostgreSQL 初始化 + 建表
│
├── xhh.Init()               # 加载 cookie.json，读取配置
├── xhh.Start()              # 启动 3 个并发循环
│   ├── CheckAt()            # 轮询 @-mentions → 写入 at 表
│   ├── AutoReply()          # 取待回复消息 → AI 生成 → 发评论
│   └── AutoFeedReply()      # 拉取首页帖子 → AI 生成 → 发评论
│
└── web.StartServer()        # HTTP :8080
    ├── GET  /               # index.html (Vue 3 SPA)
    ├── POST /api/login      # 登录获取 Bearer token
    ├── GET  /api/msgs       # 消息列表（分页/增量轮询）
    ├── GET  /api/stats      # @-回复统计数据
    ├── GET  /api/feed-stats # 帖子回复统计数据
    ├── GET  /api/config     # 读取 config.json（admin）
    ├── POST /api/config     # 写入 config.json + 热重载（admin）
    ├── GET  /api/qrcode     # 获取登录二维码（admin）
    ├── GET  /api/qrcheck    # 轮询扫码状态（admin）
    ├── GET  /api/logs       # 控制台日志（admin）
    ├── GET  /api/retry      # 重置消息回复状态（admin）
    └── GET  /api/restart    # 退出进程（admin，配合 systemd 重启）
```

### 数据流：一次 @-回复的完整链路

```
小黑盒消息 API → CheckAt() 轮询
  → db.Insert() 写入 at 表
  → AutoReply() db.GetComm(owner) 取待回复（主人优先 Top 3）
  → GetLinkInfo() 拉帖子正文 + 图片
  → GetRootComment() 拉层主上下文
  → ai.GetAiReply() 调用 AI API
    ├── 收集图片 → 视觉模型（LRU 缓存 100 条）
    ├── 拼接 prompt + 视觉报告 + 帖子正文 + 层主评论
    └── 返回 replyText + token 用量
  → 后处理：@-占位符 → HTML 标签 + 违禁词替换
  → 模拟打字延迟（字数/10 + 随机 0~4 秒）
  → Reply() 发评论（互斥锁保护）
    ├── 主号被限制？→ 备用号接替
    └── 发成功？→ 异步影子检测 → 被吞？→ 备用号补发
  → db.Replyed() 更新状态 + token 用量
```

## 快速开始

### 前置要求

- Go 1.21+
- OpenAI 兼容 API 的 Token（如 DeepSeek、Moonshot、智谱等）
- 小黑盒账号

### 使用流程

全程在浏览器中操作，不需要手动编辑文件。

**1. 启动**

双击 `启动.bat`，程序自动启动 Web 控制台，后台循环进入静默等待。

> 首次 `config.json` 不存在时会自动生成空模板，同时默认值（API 地址、版本号）自动兜底，确保扫码等功能可用。

**2. 打开控制台**

浏览器访问 `http://localhost:8080`，用管理员账号登录：

| 用户名 | 密码 |
|--------|------|
| `admin` | `admin123` |

**3. 配置**

点击左侧 **"配置"面板**，填写必填项：

| 字段 | 说明 |
|------|------|
| `ai.token` | AI API 密钥 |
| `ai.model` | 模型名称（如 `deepseek-chat`） |
| `ai.prompt` | AI 角色提示词 |
| `xhh.owner` | 你的小黑盒用户 ID |

点 **保存**，数据库自动初始化，配置热加载。

**4. 扫码登录**

点击左侧 **"登录"面板** → 获取二维码 → 小黑盒 App 扫码。`cookie.json` 自动生成并热加载。

> 备用账号同理，使用 "备用登录" 面板。

**5. 自动运行**

三个条件（配置 ✓ + 数据库 ✓ + Cookie ✓）凑齐后 10 秒内自动开始工作，无需重启。

控制台面板可实时查看消息、统计、日志，配置修改保存即生效。

### 编译（可选）

| 目标 | 脚本 | 产物 |
|------|------|------|
| Windows | `一键打包.bat` | `xhhRobot.exe` |
| Linux | `打包Linux版.bat` | `xhhRobot` |

或手动：

```bash
# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o xhhRobot.exe main.go

# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o xhhRobot main.go
```

## Bat 脚本说明

项目提供了 4 个 Windows 批处理脚本，双击即可运行，省去手动敲命令：

| 脚本 | 对应命令 | 用途 |
|------|----------|------|
| `启动.bat` | `go run main.go -mode start` | 生产模式启动。同时启动三个后台循环 + Web 服务。如果未检测到 `cookie.json` 会提示去 Web 控制台扫码。 |
| `登录.bat` | `go run main.go -mode login` | **备选方案**。终端扫码登录，二维码直接打印在命令行窗口。无需浏览器，适合 SSH/无桌面环境。 |
| `一键打包.bat` | `go build -o xhhRobot.exe` | 编译 Windows 可执行文件。自动执行 `go mod tidy` 同步依赖，然后交叉编译纯静态二进制（CGO_ENABLED=0）。 |
| `打包Linux版.bat` | `GOOS=linux go build -o xhhRobot` | 交叉编译 Linux 可执行文件。用于在 Windows 上打包，然后上传到 Linux 服务器运行。 |

> 扫码登录推荐直接在 Web 控制台操作（有 UI 面板），`登录.bat` 仅在无法打开浏览器时使用。

## 部署

### 部署需要哪些文件

编译后只需要 **3 个文件** 即可运行：

```
xhhRobot.exe          # 编译产物（或 Linux: xhhRobot）
config.json           # 你的配置文件（从 config.json.example 填写后生成）
cookie.json           # 登录后生成的 Cookie（通过 登录.bat 获取）
```

> 如果启用备用账号容灾，还需 `cookie2.json`。

### 完整部署步骤

**1. 本地编译**

双击 `一键打包.bat`（Windows）或 `打包Linux版.bat`（Linux），得到可执行文件。

**2. 准备配置文件**

```bash
copy config.json.example config.json
```

编辑 `config.json`，填入 API Token、主人 UID、角色提示词等。

**3. 获取 Cookie**

双击 `登录.bat` 扫码，生成 `cookie.json`。把 `config.json` + `cookie.json` + 可执行文件一起丢到服务器上即可。

**4. 上传到服务器**

```
服务器目录/
├── xhhRobot        # 可执行文件
├── config.json     # 配置文件
└── cookie.json     # Cookie
```

**5. 启动**

```bash
./xhhRobot -mode start
```

访问 `http://<服务器IP>:8080` 进入 Web 控制台。

> ⚠️ 如果配置了 PostgreSQL 数据库，服务器需要能连通数据库。默认使用 SQLite，无需额外安装。

### 使用 systemd 守护（Linux 推荐）

```ini
# /etc/systemd/system/xhhrobot.service
[Unit]
Description=xhhRobot AI Reply Bot
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/xhhrobot
ExecStart=/opt/xhhrobot/xhhRobot -mode start
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable xhhrobot
sudo systemctl start xhhrobot
```

程序退出 code=2（触发验证码冷却）时 systemd 会自动重启进程。

## 配置参考

### xhh（小黑盒 API）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `checkTime` | int | 30 | @-消息轮询间隔（秒） |
| `replyTime` | int | 10 | 回复检查间隔（秒） |
| `owner` | string | — | 主人小黑盒 ID，逗号分隔多个。主人的消息置顶优先回复 |
| `reply_only_owner` | bool | false | 仅回复主人的 @ |
| `replyStartHour` | int | 7 | 回复窗口起始（时），0=不限制 |
| `replyEndHour` | int | 3 | 回复窗口结束（时），小于起始表示跨天 |
| `replyIntervalSeconds` | int | 15 | 两次回复最小间隔（互斥锁保护） |
| `banned_words` | object | — | 违禁词→替换词映射表 |
| `deviceID` | string | — | 设备指纹（留空则自动生成） |
| `baseUrl` | string | api.xiaoheihe.cn | 小黑盒 API 地址 |

### ai（AI API）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `model` | string | — | 主脑模型名称 |
| `prompt` | string | — | 系统提示词（角色设定） |
| `baseUrl` | string | — | API 地址（兼容 OpenAI 格式） |
| `token` | string | — | API 密钥 |
| `vision_model` | string | — | 视觉模型名称（留空=不使用） |
| `vision_prompt` | string | — | 视觉模型的系统提示词 |
| `vision_base_url` | string | — | 视觉 API 地址（留空=复用主脑地址） |
| `vision_token` | string | — | 视觉 API 密钥（留空=复用主脑密钥） |
| `enable_vision` | bool | false | 启用图片识别 |
| `vision_mode` | string | "dual" | 视觉模式 |
| `enable_search` | bool | false | 启用联网搜索 |
| `enable_thinking` | bool | false | 启用深度思考 |
| `reasoning_effort` | string | "medium" | 推理强度（low/medium/high/xhigh） |
| `enable_search_extension` | bool | false | 搜索扩展 |
| `max_post_images` | int | 6 | 帖子图片上限 |
| `max_comment_images` | int | 3 | 评论图片上限 |

### feedReply（首页帖子回复）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | false | 是否启用 |
| `startHour` | int | 4 | 运行窗口起始 |
| `endHour` | int | 6 | 运行窗口结束 |
| `interval` | int | 900 | 拉取间隔（秒） |
| `maxPerRun` | int | 1 | 每轮最多回复数 |
| `maxPerDay` | int | 10 | 每天最多回复数 |
| `dryRun` | bool | false | 试运行模式（生成但不发送） |

### fallback（备用账号容灾）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | true | 启用备用账号 |
| `cookieFile` | string | "cookie2.json" | 备用 Cookie 文件路径 |
| `deviceID` | string | — | 备用设备指纹 |
| `prompt` | string | — | 备用 AI 人格（如"呆猫"），留空则复用主 prompt |
| `mainCooldownMinutes` | int | 360 | 主号被限制后的冷却时间（分钟） |

### database

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | "sqlite" | 数据库类型（sqlite / pg） |
| `db` | string | — | SQLite 文件路径或 PG 数据库名 |
| `host` | string | — | PostgreSQL 主机 |
| `port` | string | — | PostgreSQL 端口 |
| `user` | string | — | PostgreSQL 用户名 |
| `passwd` | string | — | PostgreSQL 密码 |

## Web 控制台

浏览器访问 `http://localhost:8080`。

### 功能面板

| 面板 | 权限 | 功能 |
|------|------|------|
| 统计 | 登录用户 | 今日/本周/本月回复量、Token 消耗、成功率 |
| 艾特回复 | 登录用户 | 消息列表（左侧线程分组 + 右侧气泡），8 秒自动刷新 |
| 帖子回复 | 登录用户 | 首页帖子 + AI 回复记录，回复状态标记 |
| 配置 | 管理员 | 在线编辑 config.json，保存即热重载 |
| 测试 | 管理员 | 粘贴文字/图片，手动测试 AI 回复效果 |
| 登录 | 管理员 | 扫码登录小黑盒，支持主号+备用号 |
| 日志 | 管理员 | 实时查看控制台日志 |

### 登录凭据

| 角色 | 用户名 | 密码 |
|------|--------|------|
| 管理员 | `admin` | `admin123` |
| 游客 | `guest` | `guest` |

> ⚠️ **安全提醒**：部署到公网前务必修改默认凭据。编辑 `web/server.go` 第 22-28 行的常量后重新编译。

## 双账号容灾机制

```
主号发评论 → 成功 → 异步检测影子评论
                  ├── 未被吞 → 完成
                  └── 被吞 → 备用号补发（可选独立人格）

主号发评论 → 失败 → 检测 msg 关键词
                  ├── "账号异常/禁言/限制" → 主号进入冷却(默认6h)
                  │   ├── 备用号可用 → 接替发评论
                  │   └── 备用号不可用 → 丢弃
                  └── 其他错误 → 丢弃
```

配置 `fallback.enabled=true` 并在 `fallback.prompt` 中填写备用 AI 人设即可启用（如项目自带的"呆猫"——《怪物猎人》艾露猫人格）。

## 项目结构

```
xhhRobot/
├── main.go              # 入口，模式分发（default/login/login2/start/test）
├── go.mod / go.sum      # Go 模块依赖
├── index.html           # Web 控制台（Vue 3 + Bootstrap 5）
├── config.json.example  # 配置模板
├── README.md
│
├── 启动.bat             # 生产模式启动
├── 登录.bat             # 扫码登录获取 Cookie
├── 一键打包.bat          # 编译 Windows 版
├── 打包Linux版.bat       # 交叉编译 Linux 版
│
├── xhh/                 # 核心业务逻辑
│   ├── main.go          # 配置加载、@-消息轮询、AI 回复调度
│   ├── start.go         # 启动 3 个并发 goroutine
│   ├── reply.go         # 评论发送、互斥锁、@-标签生成
│   ├── feed_reply.go    # 首页帖子自动回复
│   ├── login.go         # 扫码登录 + RSA 加密
│   ├── getkey.go        # API 请求签名算法
│   ├── sendreq.go       # HTTP 请求封装（主号/备用号路由）
│   ├── GetLinkInfo.go   # 帖子正文 + 图片提取
│   ├── shadow_check.go  # 影子评论检测
│   ├── fallback.go      # 备用账号管理
│   ├── account_cooldown.go  # 主号冷却 + 账号限制检测
│   ├── captcha.go       # 验证码冷却全局锁
│   ├── blacklist.go     # 用户黑名单
│   └── owner.go         # 主人 UID 解析
│
├── ai/                  # AI API 客户端
│   ├── GetReply.go      # prompt 构建 + 视觉 pipeline + 多模型调度
│   └── sendReq.go       # HTTP 请求 + 视觉/主脑智能路由
│
├── db/                  # 数据库层
│   ├── main.go          # SQLite 连接 + at 表 CRUD
│   ├── feed_reply.go    # 帖子回复记录表
│   └── web_api.go       # Web API 查询（统计、分页、增量轮询）
│
├── web/                 # HTTP 服务
│   └── server.go        # 路由注册 + Bearer 鉴权中间件 + 19 个 API 端点
│
├── config/              # 配置管理
│   └── main.go          # ConfigStruct 定义 + config.json 读写
│
├── loger/               # 日志
│   └── loger.go         # zap 初始化 + stdout 捕获（供 Web 日志面板）
│
├── sqlite/              # SQLite 初始化
│   └── init.go          # at 表建表（WAL 模式）
│
└── pg/                  # PostgreSQL 初始化
    └── init.go          # 连接池 + 建表
```

## License

MIT
