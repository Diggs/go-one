package gone

import (
	zmq "github.com/pebbe/zmq4"
)

type Gone struct {
	ip          string
	lockFactory LockFactory
	zmqContext  *zmq.Context
	server      *goneServer
	dispatcher  *goneDispatcher
}

func (g *Gone) buildLock(id string) (GoneLock, error) {
	return g.lockFactory(id, g.ip)
}

func (g *Gone) Close() {
	g.server.Close()
	g.dispatcher.Close()
}

func (g *Gone) SendData(id string, data []byte) error {
	return g.dispatcher.SendData(id, data)
}

func (g *Gone) RunOne(id string, runHandler RunHandler) (*GoneRunner, error) {
	runner := g.dispatcher.GetRunner(id)
	if runner != nil {
		return runner, nil
	}

	lock, err := g.buildLock(id)
	if err != nil {
		return nil, err
	}

	runner = newGoneRunner(id, runHandler, lock, g)
	g.dispatcher.AddRunner(id, runner)
	return runner, nil
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
	g.dispatcher = newDispatcher(server, context)
	g.zmqContext = context
	return &g, nil
}
