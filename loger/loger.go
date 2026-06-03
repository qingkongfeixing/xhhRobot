package loger

import (
	"os"
	"regexp"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Loger *zap.Logger
var ModelName = "[Loger]"

// 【新增】：网页专用的终端缓冲池
var WebConsole = &ConsoleBuffer{buf: make([]byte, 0, 50000)}

type ConsoleBuffer struct {
	buf []byte
	mu  sync.Mutex
}

func (c *ConsoleBuffer) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	// 保持最新 50KB 数据，防止积压导致内存溢出
	if len(c.buf) > 50000 {
		c.buf = c.buf[len(c.buf)-50000:]
	}
	return len(p), nil
}

func (c *ConsoleBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 剔除终端专用的颜色代码（如 \033[1;35m），防止网页显示乱码
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(string(c.buf), "")
}

func InitLog() {
	// ==========================================
	// 【核心黑科技】：全局劫持控制台标准输出 (os.Stdout)
	// ==========================================
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				oldStdout.Write(buf[:n])   // 1. 真实终端正常显示
				WebConsole.Write(buf[:n]) // 2. 网页缓冲池同步记录
			}
			if err != nil {
				break
			}
		}
	}()

	ConsoleLogConfig := zap.NewProductionEncoderConfig()
	ConsoleLogConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	ConsoleLogConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	var ConsoleLogerEcoder zapcore.Encoder
	ConsoleLogerEcoder = zapcore.NewConsoleEncoder(ConsoleLogConfig)

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(wd+"/log", 0775)
	if err != nil {
		panic(err)
	}

	File, err := os.Create(wd + "/log/" + time.Now().Format("2006-01-02_15_04_05") + ".log")
	if err != nil {
		panic(err)
	}

	FileConfig := zap.NewProductionEncoderConfig()
	FileConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	FileEcoder := zapcore.NewConsoleEncoder(FileConfig)

	// 【核心】：终端依然使用 Info，文件依然使用 Debug（互不干扰）
	core := zapcore.NewTee(
		zapcore.NewCore(ConsoleLogerEcoder, zapcore.AddSync(os.Stdout), zap.InfoLevel),
		zapcore.NewCore(FileEcoder, zapcore.AddSync(File), zap.DebugLevel),
	)

	Loger = zap.New(core)
	Loger.Info(ModelName + "OK")
}