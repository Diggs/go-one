package gone

import (
	"github.com/diggs/glog"
	zmq "github.com/pebbe/zmq4"
	"sync"
)

type goneServer struct {
	socket    *zmq.Socket
	waitGroup *sync.WaitGroup
	Data      chan []byte
	Exit      chan bool
}

func NewGoneServer(ip string, context *zmq.Context) (*goneServer, error) {
	s := goneServer{}
	s.waitGroup = &sync.WaitGroup{}
	s.Data = make(chan []byte)
	s.Exit = make(chan bool)
	err := s.startServer(ip, context)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *goneServer) Close() {
	close(s.Exit)
	go func() {
		s.waitGroup.Wait()
		close(s.Data)
	}()
}

func (s *goneServer) startServer(ip string, context *zmq.Context) error {
	socket, err := context.NewSocket(zmq.REP)
	if err != nil {
		return err
	}
	err = socket.Bind(ip)
	if err != nil {
		return err
	}
	s.socket = socket
	go s.enterReadLoop()
	return nil
}

func (s *goneServer) enterReadLoop() {
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()

	glog.Debug("Entering read loop...")
	for {
		select {
		case <-s.Exit:
			glog.Debug("Exit signalled; exiting read loop.")
			return
		default:
			data, err := s.socket.RecvBytes(0)
			if err != nil {
				glog.Errorf("Error reading data from socket: %v", err)
				continue
			}
			s.socket.Send("", 0)
			s.Data <- data
		}
	}
}
