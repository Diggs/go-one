package gone

import (
	"github.com/diggs/glog"
	"github.com/diggs/go-backoff"
	"sync"
	"time"
)

type RunHandler func(*GoneRunner)

type GoneRunner struct {
	id         string
	ip         string
	runHandler RunHandler
	Lock       GoneLock
	runBackoff *backoff.Backoff
	waitGroup  *sync.WaitGroup
	gone       *Gone
	Data       chan []byte
	Exit       chan bool
}

func (r *GoneRunner) Close() {
	close(r.Exit)
	go func() {
		r.waitGroup.Wait()
		close(r.Data)
	}()
}

func (r *GoneRunner) Extend() error {
	return r.Lock.Extend()
}

func (r *GoneRunner) SendData(data []byte) error {
	return r.gone.SendData(r.id, data)
}

func (r *GoneRunner) enterRunLoop() {
	r.waitGroup.Add(1)
	defer r.waitGroup.Done()

	for {
		select {
		case <-r.Exit:
			glog.Debug("Exit signalled; exiting run loop.")
			return
		default:
			err := r.run()
			r.runBackoff.Backoff()
			if err == nil {
				r.runBackoff.Reset()
			}
		}
	}
}

func (r *GoneRunner) run() error {
	err := r.Lock.Lock()
	if err != nil {
		return err
	}
	defer r.Lock.Unlock()
	r.runHandler(r)
	return nil
}

func newGoneRunner(id string, runHandler RunHandler, lock GoneLock, gone *Gone) *GoneRunner {
	r := GoneRunner{}
	r.id = id
	r.runHandler = runHandler
	r.Lock = lock
	r.gone = gone
	r.Data = make(chan []byte)
	r.Exit = make(chan bool)
	r.waitGroup = &sync.WaitGroup{}
	r.runBackoff = backoff.NewExponentialFullJitter(5*time.Second, 20*time.Second)
	go r.enterRunLoop()
	return &r
}
