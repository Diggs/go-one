package gone

import (
	"fmt"
	"github.com/diggs/glog"
	zmq "github.com/pebbe/zmq4"
	"sync"
	"runtime/debug"
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

func (d *goneDispatcher) sendDataWithRecovery(bytes []byte, socket *zmq.Socket) (<-chan error, <-chan error) {
	errChan := make(chan error)
	panicChan := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("Recovered in sendDataWithRecovery: %v \n %s", r, debug.Stack())
				panicChan <- fmt.Errorf("Panicked in sendDataWithRecovery. Remote lock holder possibly offline.")
			}
		}()
		_, err := socket.SendBytes(bytes, 0)
		if err != nil {
			errChan <- err
			return
		}
		_, err = socket.Recv(0)
		if err != nil {
			errChan <- err
			return
		}
		errChan <- nil
	}()
	return panicChan, errChan
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

	panicChan, errChan := d.sendDataWithRecovery(bytes, socket)
	select {
	case err = <-errChan:
		// Handle normal send/recv errors, no need to recycle socket.
		if err != nil {
			return err
		}
	case err = <-panicChan:
		// By observation pebbe/zmq4 sometimes panics when the remote socket closes during the recv() loop.
		// Recycle the socket so that the next time the remote process comes online and takes the lock
		// we will establish a new connection.
		go d.destroyReqSocket(ip)
		if err != nil {
			return err
		}
	case <-time.After(10 * time.Second):
		// Zeromq can deadlock on REQ/REP socket pairs where the REP goes missing.
		// (due to remote server going offline between REQ and REP steps, for example).
		// There's no easy way to detect this so we use a simple timeout to infer it (which will get it wrong sometimes)
		// Recycle the socket so we establish a new connection on the next send.
		go d.destroyReqSocket(ip)
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

	socket := d.zmqSockets[ip]
	if socket != nil {
		glog.Debugf("Using existing socket: %v", socket.String())
		return socket, nil
	}

	socket, err := d.zmqContext.NewSocket(zmq.REQ)
	if err != nil {
		return nil, err
	}
	err = socket.Connect(ip)
	if err != nil {
		return nil, err
	}
	d.zmqSockets[ip] = socket
	glog.Debugf("Created new socket: %v", socket.String())
	return socket, nil
}

func (d *goneDispatcher) destroyReqSocket(ip string) {
	defer func() {
		if r := recover(); r != nil {
			glog.Warningf("Recovered in destroyReqSocket: %v", r)
		}
		glog.Debugf("Destroyed socket for %v: %v", ip, d.zmqSockets[ip].String())
		d.zmqSockets[ip] = nil
	}()
	d.zmqSocketsMutex.Lock()
	defer d.zmqSocketsMutex.Unlock()
	// TODO Attempting to close a socket while it's waiting on a recv() causes a panic
	// in cgo that doesn't seem to be recoverable
	// socket := d.zmqSockets[ip]
	// if socket != nil {
	// 	socket.Close()
	// }
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
