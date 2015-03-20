package main

import (
	"github.com/diggs/glog"
	"github.com/diggs/gone"
	"net"
	"time"
	"os"
)

func redisLock(id string, value string) (gone.GoneLock, error) {
	return gone.NewRedisLock(&net.TCPAddr{Port: 6379}, id, value)
}

func Run(g *gone.Gone) {
	for {
		select {
		case <- g.Exit:
			glog.Debugf("Exit signalled; exiting runner.")
			return
		case data := <- g.Data:
			glog.Debugf("Received some data: %v", data)
		case <-time.After(25 * time.Second):
			err := g.Extend()
			if err != nil {
				glog.Debugf("Failed to extend lock; exiting runner: %v", err)
				return
			}
		}
	}
}

func main() {

	glog.SetSeverity("debug")
	defer glog.Flush()

	g, err := gone.RunOne("tcp://127.0.0.1:6380", "irc:#connectrix:irc.freenode.net", Run, redisLock)
	if err != nil {
		panic(err.Error())
	}

	if os.Getenv("T") == "1" {
		go func() {
			time.Sleep(3 * time.Second)
			glog.Info("Sending data")
			err := g.SendData([]byte("hello"))
			glog.Info("Sent data")
			if err != nil {
				glog.Debugf("Failed to send data: %v", err)
			}
		}()
	}

	for {
		select {
		case <- g.Exit:
			glog.Info("Runner quit.")
			return
		}
	}
}
