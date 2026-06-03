package xhh

import (
	"fmt"
)

func Start() {
	fmt.Println("[XHH] Starting")
	go func() {
		CheckAt()
	}()
	go func() {
		AutoReply()
	}()
	go func() {
		AutoFeedReply()
	}()
}