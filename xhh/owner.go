package xhh

import (
	"strconv"
	"strings"
	"xhhrobot/config"
	"xhhrobot/loger"
)

var Owners []int

func Check(UID int) bool {
	if len(Owners) <= 0 {
		cfg := config.ConfigStruct.Xhh
		if cfg.Owner == "" {
			loger.Loger.Fatal("您未在配置中设置所有者（Xhh.owner）程序已退出！")
			return false
		}
		OwnArr := strings.Split(cfg.Owner, ",")
		for _, v := range OwnArr {
			if v != "" {
				i, err := strconv.Atoi(v)
				if err != nil {
					loger.Loger.Error("[XHH]您的所有者配置->" + v + "<-似乎并非数字")
					continue
				}
				Owners = append(Owners, i)
			}
		}
	}
	for _, v := range Owners {
		if v == UID {
			return true
		}
	}
	return false
}
