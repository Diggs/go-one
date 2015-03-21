package main

import (
	"github.com/diggs/glog"
	"github.com/diggs/gone"
	"net"
	"time"
)

func redisLock(id string, value string) (gone.GoneLock, error) {
	return gone.NewRedisLock(&net.TCPAddr{Port: 6379}, id, value)
}

func runner(r *gone.GoneRunner) {
	ticker := time.NewTicker(25 * time.Second).C
	for {
		select {
		case <- r.Exit:
			glog.Debugf("Exit signalled; exiting runner.")
			return
		case data := <- r.Data:
			glog.Debugf("Received data: %v", string(data))
		case <-ticker:
			err := r.Extend()
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

	g, err := gone.NewGone("tcp://127.0.0.1:6381", redisLock)
	if err != nil {
		panic(err.Error())
	}

	r, err := g.RunOne("irc:#connectrix:irc.freenode.net", runner)
	if err != nil {
		panic(err.Error())
	}

	go func() {
		for {
			err = r.SendData([]byte("hello world"))
			if err != nil {
				glog.Debugf("Send err: %v", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	for {
		select {
		case <- r.Exit:
			glog.Info("Runner quit.")
			return
		}
	}
}
