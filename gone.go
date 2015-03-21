package gone

import (
	"fmt"
	"github.com/diggs/glog"
	zmq "github.com/pebbe/zmq4"
	"sync"
)

type Gone struct {
	ip          string
	lockFactory LockFactory
	zmqContext  *zmq.Context
	// sockets keyed by ip
	zmqSockets      map[string]*zmq.Socket
	zmqSocketsMutex sync.Mutex
	// runners keyed by id
	runners      map[string]*GoneRunner
	runnersMutex sync.Mutex
	server       *goneServer
}

func (g *Gone) Close() {
	// TODO Close all runners and server
}

func (g *Gone) getReqSocket(ip string) (*zmq.Socket, error) {
	g.zmqSocketsMutex.Lock()
	defer g.zmqSocketsMutex.Unlock()

	if socket, exists := g.zmqSockets[ip]; exists {
		return socket, nil
	} else {
		socket, err := g.zmqContext.NewSocket(zmq.REQ)
		if err != nil {
			return nil, err
		}
		err = socket.Connect(ip)
		if err != nil {
			return nil, err
		}
		g.zmqSockets[ip] = socket
		return socket, nil
	}
}

func (g *Gone) buildLock(id string) (GoneLock, error) {
	return g.lockFactory(id, g.ip)
}

func (g *Gone) dispatchData(id string, data []byte) {
	runner := g.runners[id]
	if runner == nil {
		glog.Errorf("Unknown runner id: %v", id)
		return
	}
	runner.Data <- data
}

func (g *Gone) enterServerDispatchLoop() {
	for {
		select {
		case <-g.server.Exit:
			glog.Debug("Exit signalled; exiting dispatch loop.")
			return
		case data := <-g.server.Data:
			glog.Debugf("Received data: %v bytes", len(data))
			payload, err := decodePayload(data)
			if err != nil {
				glog.Errorf("Unable to decode payload: %v", err)
				continue
			}
			go g.dispatchData(payload.Id, payload.Data)
		}
	}
}

func (g *Gone) SendData(id string, data []byte) error {
	g.runnersMutex.Lock()
	defer g.runnersMutex.Unlock()

	runner := g.runners[id]
	if runner == nil {
		return fmt.Errorf("Unknown runner id: %v", id)
	}

	glog.Infof("Looking up lock holder for %v...", id)

	ip, err := runner.Lock.GetValue()
	if err != nil {
		return err
	}

	glog.Infof("Sending data for %v to %v", id, ip)

	socket, err := g.getReqSocket(ip)
	if err != nil {
		return err
	}

	payload := gonePayload{Id: id, Data: data}
	bytes, err := encodePayload(&payload)
	if err != nil {
		return err
	}

	socket.SendBytes(bytes, 0)
	socket.Recv(0)

	return nil
}

func (g *Gone) RunOne(id string, runHandler RunHandler) (*GoneRunner, error) {
	g.runnersMutex.Lock()
	defer g.runnersMutex.Unlock()
	if runner, exists := g.runners[id]; exists {
		return runner, nil
	}

	lock, err := g.buildLock(id)
	if err != nil {
		return nil, err
	}

	g.runners[id] = newGoneRunner(id, runHandler, lock, g)
	return g.runners[id], nil
}

func NewGone(ip string, lockFactory LockFactory) (*Gone, error) {
	context, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}
	server, err := NewGoneServer(ip, context)
	if err != nil {
		return nil, err
	}
	g := Gone{}
	g.ip = ip
	g.lockFactory = lockFactory
	g.server = server
	g.zmqContext = context
	g.zmqSockets = make(map[string]*zmq.Socket)
	g.runners = make(map[string]*GoneRunner)
	go g.enterServerDispatchLoop()
	return &g, nil
}
