package gone

import (
	"github.com/diggs/glog"
	zmq "github.com/pebbe/zmq4"
	"time"
)

type Runner func(*Gone)

type Gone struct {
	id          string
	ip          string
	runner      Runner
	lockFactory LockFactory
	lock        GoneLock
	zmqContext  *zmq.Context
	Data        chan []byte
	Exit        chan bool
}

func (g *Gone) Close() {
	close(g.Exit)
}

func (g *Gone) buildLock() error {
	lock, err := g.lockFactory(g.id, g.ip)
	if err != nil {
		return err
	}
	g.lock = lock
	return nil
}

func (g *Gone) enterRunLoop() {
	for {
		select {
		case <-g.Exit:
			glog.Debugf("Exit signalled; exiting run loop.")
			return
		default:
			g.run()
			time.Sleep(5 * time.Second)
		}
	}
}

func (g *Gone) run() {
	err := g.lock.Lock()
	if err != nil {
		return
	}
	defer g.lock.Unlock()
	g.runner(g)
}

func (g *Gone) startDataServer() error {
	socket, err := g.zmqContext.NewSocket(zmq.REP)
	if err != nil {
		return err
	}
	socket.Bind(g.ip)

	go func() {
		for {
			data, _ := socket.RecvBytes(0)
			glog.Infof("Received data: %v", string(data))
			socket.Send("ack", 0)
			// TODO Route data to appropriate Runner
		}
	}()

	return nil
}

func (g *Gone) Extend() error {
	return g.lock.Extend()
}

func (g *Gone) SendData(data []byte) error {

	ip, err := g.lock.GetValue()
	if err != nil {
		return err
	}

	glog.Infof("Sending data for %v to %v", g.id, ip)

	socket, err := g.zmqContext.NewSocket(zmq.REQ)
	if err != nil {
		return err
	}
	socket.Connect(ip)

	// TODO encapsulate runner info in to message
	socket.SendBytes(data, 0)

	glog.Info("Sent data, waiting for reply")

	socket.Recv(0)

	return nil
}

func RunOne(ip string, id string, runner Runner, lockFactory LockFactory) (*Gone, error) {
	g := Gone{}
	g.ip = ip
	g.id = id
	g.runner = runner
	g.lockFactory = lockFactory
	g.Data = make(chan []byte)
	g.Exit = make(chan bool)
	context, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}
	g.zmqContext = context
	err = g.buildLock()
	if err != nil {
		return nil, err
	}
	err = g.startDataServer()
	if err != nil {
		return nil, err
	}
	go g.enterRunLoop()
	return &g, nil
}
