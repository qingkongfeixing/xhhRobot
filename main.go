package main

import (
	"flag"
	"fmt"
	"time"
	"xhhrobot/config"
	"xhhrobot/db"
	"xhhrobot/loger"
	"xhhrobot/web"
	"xhhrobot/xhh"
)

func main() {
	loger.InitLog()
	config.InitConfig()
	time.Sleep(1 * time.Second)
	db.Init()
	mode := flag.String("mode", "default", "Switch a mode when start")
	flag.Parse()
	start(mode)
}

func CheckNew() {
	if !db.IsNew() {
		return
	}
	xhh.DontReply = true
}

func start(mode *string) {
	switch *mode {
	case "default":
		loger.Loger.Info("\nhttps://github.com/SomeOvO/xhhRobot\n你需要输入启动项\n-mode start | login | login2 | test")
	case "test":
		fmt.Println("你玩原神吗？")
	case "login":
		go web.StartServer()
		xhh.Login()
	case "login2":
		go web.StartServer()
		xhh.Login2()
	case "start":
		CheckNew()
		xhh.Init()
		xhh.Start()
		go web.StartServer()
		select {}
	}
}

