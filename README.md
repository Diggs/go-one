## What is it?

Gone (go-one) ensures there is one (and only one) instance of a Go routine running on a cluster of nodes.

## How does it Work?

It uses a distributed lock to ensure the specified Go routine is only running within one process on the cluster. The lock has a relatively short expiry time and if the Go routine is healthy it is required to continuously extend the lock. If the lock expires due to a badly behaved or unhealthy Go routine (or the process itself has died) then another node will spawn the Go routine and take over. Currently the distributed lock is implemented in Redis, but an interface is provided so that the lock could be implemented in other services such as Etcd or Postgres.

Additionally, ZeroMQ is used so that any node can send data to the Go routine, regardless of which node it is actually running on. The distributed locking interface doubles as a primitive service-discovery mechanism so each node can find and talk to other nodes when needed.

## Use cases

Gone was written specifically to solve the problem where a stateful connection is needed to talk to an external service or API (e.g. a persistent socket connection) in a horizontally scaled system. Take these simple use cases:

```
1) load_balancer -> N x web_server -> irc_chat_room
2) message_queue -> N x background_worker -> irc_chat_room
```

A message needs to be sent to an IRC chat room. IRC is stateful, that is you are persistently "connected". In a naive implementation one might open and close a connection to the IRC chat room for each message, but that would result in every one else in the room seeing your service constantly connect and disconnect. Additionally, the IRC server might begin to refuse connections or rate limit your service.

go-one solves this by enabling automatic negotiation, routing and failover for a Go routine across a cluster of nodes.

## Usage

See the ```examples``` directory for a full working example.

Instantiate a new ```Gone``` and specify the ```IP``` address of the current machine (that should be routable by other instances), the ```port``` the process should listen on for messages from other instances, and finally the locking mechanism to use (currently only Redis is supported).

Next, call the ```RunOne``` function, passing in a unique identifier for the routine that will be run, as well as a ```RunHandler``` that go-one will ensure is always running on one node in the cluster.

The ```RunHandler``` is the most interesting item, it can read the ```runner.Data``` channel to receive messages from any instance for processing. It is also responsible for letting Gone know it is healthy by calling ```runner.Extend()``` within the lock expiry period (30 seconds by default). If the lock expires because the run handler has not extended it, the handler will quit and a new one will spawn on an arbitrary node.

See below for a full example: 

```go
func redisLock(id string, value string) (gone.GoneLock, error) {
  return gone.NewRedisLock(&net.TCPAddr{Port: 6379}, id, value)
}

func runner(r *gone.GoneRunner) {
  // Do some application-specific setup e.g. connect to stateful service
  ticker := time.NewTicker(25 * time.Second).C
  for {
    select {
    case <- r.Exit:
      glog.Debugf("Exit signalled; exiting runner.")
      return
    case data := <- r.Data:
      glog.Debugf("Received data: %v", string(data))
      // Do something application-specific with the data
    case <-ticker:
      err := r.Extend()
      if err != nil {
        glog.Debugf("Failed to extend lock; exiting runner: %v", err)
        return
      }
    }
  }
}

g, err := gone.NewGone("tcp://127.0.0.1:6381", redisLock)
if err != nil {
  panic(err.Error())
}

r, err := g.RunOne("unique_identifier", runner)
if err != nil {
  panic(err.Error())
}

r.SendData([]byte("hello world"))
```

To send data to the handler function, regardless of which instance it is running on, use the ```gone.SendData``` function. Gone will route the data to the handler function on the node it is currently executing on.