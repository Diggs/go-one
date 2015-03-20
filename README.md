## What is it?

Gone (go-one) ensures there is one (and only one) instance of a Go routine running on a cluster of nodes.

## How does it Work?

It uses a distributed lock to ensure the specified Go routine is only running within one process on the cluster. The lock has a relatively short expiry time and if the Go routine is healthy it is required to continuously extend the lock. If the lock expires due to a badly behaved or unhealthy Go routine (or the process itself has died) then another node will spawn the Go routine and take over. Currently the distributed lock is implemented in Redis, but an interface is provided so that the lock could be implemented in other services such as Etcd or Postgres.

Additionally, ZeroMQ is used so that any node can send data to the Go routine, regardless of which node it is actually running on. The distributed locking interface doubles as a primitive service-discovery mechanism so each node can find and talk to other nodes when needed.

## Use cases

Gone was written specifically to solve the problem where a stateful connection is needed to talk to an external service or API (e.g. a persistent socket connection) in a horizontally scaled system. Take this simple use case:

```
load_balancer -> (N) x web_server -> irc_chat_room
```

An incoming web request is converted to a message that needs to be sent to an IRC chat room. IRC is stateful, that is you are persistently "connected". In a naive implementation one might open and close a connection to the IRC chat room for each message, but that would result in every one else in the room seeing your service constantly connect and disconnect. Additionally, the IRC server might begin to refuse connections or rate limit your service. 

This might be solved by introducing a message queue and a background worker for handling message delivery:

```
load_balancer -> (N) x web_server -> message_queue -> background_worker -> irc_chat_room
```

But now not only is your architecture more complicated, your horizontally scaled service isn't looking so horizontal any more.  If the background_worker process dies, or even worse the machine it runs on, there are no nodes standing by to reestablish the connection.

Gone solves this by enabling the first architecture displayed above to operate as though it looked like the second, more complicated architecture.

## Usage

Instantiate a new ```Gone``` and specify the ```IP``` address of the current machine (that should be routable by other instances), the ```port``` the process should listen on for messages from other instances, a ```unique key``` that identifies the Go routine that is being managed, a handler for the Go routine, and finally the locking mechanism to use (currently only Redis is supported).

The handler is the most interesting piece, it can read the ```gone.Data``` channel to receive messages from any instance for processing. It is also responsible for letting Gone know it is healthy by calling ```gone.Extend()``` within the lock expiry period (30 seconds by default).

See below for a full example: 

```go
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
      glog.Debugf("Received data: %v", string(data))
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
  g, err := gone.RunOne("tcp://127.0.0.1:6381", "irc:#connectrix:irc.freenode.net", Run, redisLock)
  if err != nil {
    panic(err.Error())
  }

  for {
    select {
    case <- g.Exit:
      glog.Info("Runner quit.")
      return
    }
  }
}

```

To send data to the handler function, regardless of which instance it is running on, use the ```gone.SendData``` function. Gone will route the data to the handler function on the node it is currently executing on.