package gone

import (
	"fmt"
	"github.com/diggs/glog"
	zmq "github.com/pebbe/zmq4"
	"sync"
	"time"
)

type goneDispatcher struct {
	zmqContext *zmq.Context
	server     *goneServer
	// runners keyed by id
	runners      map[string]*GoneRunner
	runnersMutex sync.Mutex
	// sockets keyed by ip
	zmqSockets      map[string]*zmq.Socket
	zmqSocketsMutex sync.Mutex
}

func (d *goneDispatcher) Close() {
	d.runnersMutex.Lock()
	defer d.runnersMutex.Unlock()
	for _, runner := range d.runners {
		runner.Close()
	}

	d.zmqSocketsMutex.Lock()
	defer d.zmqSocketsMutex.Unlock()
	for _, socket := range d.zmqSockets {
		socket.Close()
	}
}

func (d *goneDispatcher) GetRunner(id string) *GoneRunner {
	d.runnersMutex.Lock()
	defer d.runnersMutex.Unlock()
	return d.runners[id]
}

func (d *goneDispatcher) AddRunner(id string, runner *GoneRunner) {
	d.runnersMutex.Lock()
	defer d.runnersMutex.Unlock()
	d.runners[id] = runner
}

func (d *goneDispatcher) sendDataWithTimeout(bytes []byte, socket *zmq.Socket) <-chan error {
	done := make(chan error)
	go func() {
		// defer func() {
		//    if err := recover(); err != nil {
		//        done <- err
		//    }
		//   }()

		_, err := socket.SendBytes(bytes, 0)
		if err != nil {
			done <- err
			return
		}
		_, err = socket.Recv(0)
		if err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	return done
}

func (d *goneDispatcher) SendData(id string, data []byte) error {
	runner := d.GetRunner(id)
	if runner == nil {
		return fmt.Errorf("Unknown runner id: %v", id)
	}

	glog.Infof("Looking up lock holder for %v...", id)
	ip, err := runner.Lock.GetValue()
	if err != nil {
		return err
	}

	glog.Infof("Sending data for %v to %v", id, ip)
	socket, err := d.getReqSocket(ip)
	if err != nil {
		return err
	}

	payload := gonePayload{Id: id, Data: data}
	bytes, err := encodePayload(&payload)
	if err != nil {
		return err
	}

	c := d.sendDataWithTimeout(bytes, socket)
	select {
	case err = <-c:
		if err != nil {
			return err
		}
	case <-time.After(10 * time.Second):
		d.destroyReqSocket(ip)
		return fmt.Errorf("Timed out sending data")
	}

	return nil
}

func (d *goneDispatcher) dispatchData(data []byte) {
	payload, err := decodePayload(data)
	if err != nil {
		glog.Errorf("Unable to decode payload: %v", err)
		return
	}

	runner := d.GetRunner(payload.Id)
	if runner == nil {
		glog.Errorf("Unknown runner id: %v", payload.Id)
		return
	}
	runner.Data <- payload.Data
}

func (d *goneDispatcher) enterDispatchLoop() {
	for {
		select {
		case <-d.server.Exit:
			glog.Debug("Exit signalled; exiting dispatch loop.")
			return
		case data := <-d.server.Data:
			glog.Debugf("Received data: %v bytes", len(data))
			go d.dispatchData(data)
		}
	}
}

func (d *goneDispatcher) getReqSocket(ip string) (*zmq.Socket, error) {
	d.zmqSocketsMutex.Lock()
	defer d.zmqSocketsMutex.Unlock()

	if socket, exists := d.zmqSockets[ip]; exists {
		return socket, nil
	} else {
		socket, err := d.zmqContext.NewSocket(zmq.REQ)
		if err != nil {
			return nil, err
		}
		err = socket.Connect(ip)
		if err != nil {
			return nil, err
		}
		d.zmqSockets[ip] = socket
		return socket, nil
	}
}

func (d *goneDispatcher) destroyReqSocket(ip string) {
	d.zmqSocketsMutex.Lock()
	defer d.zmqSocketsMutex.Unlock()
	// socket := d.zmqSockets[ip]
	// if socket != nil {
	// 	socket.Close()
	// }
	d.zmqSockets[ip] = nil
}

func newDispatcher(server *goneServer, zmqContext *zmq.Context) *goneDispatcher {
	d := goneDispatcher{}
	d.server = server
	d.zmqContext = zmqContext
	d.zmqSockets = make(map[string]*zmq.Socket)
	d.runners = make(map[string]*GoneRunner)
	go d.enterDispatchLoop()
	return &d
}
