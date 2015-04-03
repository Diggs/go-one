package gone

import (
	"errors"
	"github.com/diggs/glog"
	"github.com/fzzy/radix/redis"
	"net"
	"sync"
	"time"
)

const (
	DefaultExpiry = 30 * time.Second
)

var (
	LockError = errors.New("Failed to acquire Redis lock.")
)

type RedisLock struct {
	Expiry  time.Duration
	client  *redis.Client
	mutex   sync.Mutex
	lockId  string
	lockVal string
}

func NewRedisLock(redisIp net.Addr, lockId string, lockVal string) (*RedisLock, error) {
	client, err := redis.DialTimeout(redisIp.Network(), redisIp.String(), time.Duration(10)*time.Second)
	if err != nil {
		return nil, err
	}
	return &RedisLock{client: client, lockVal: lockVal, lockId: lockId}, nil
}

func (r *RedisLock) expiry() int {
	expiry := r.Expiry
	if expiry == 0 {
		expiry = DefaultExpiry
	}
	return int(expiry / time.Millisecond)
}

func (r *RedisLock) logErr(action string, err string) {
	glog.Debugf("Failed to %v lock %v: %v", action, r.lockId, err)
}

func (r *RedisLock) Lock() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	reply := r.client.Cmd("set", r.lockId, r.lockVal, "nx", "px", r.expiry())
	if reply.Err != nil {
		r.logErr("acquire", reply.Err.Error())
		return reply.Err
	}

	if reply.String() != "OK" {
		r.logErr("acquire", reply.String())
		return LockError
	}

	glog.Debugf("Acquired lock %v:%v", r.lockId, r.lockVal)

	return nil
}

func (r *RedisLock) Extend() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	reply := r.client.Cmd("eval", `
			if redis.call("get", KEYS[1]) == ARGV[1] then
			    return redis.call("set", KEYS[1], ARGV[1], "PX", ARGV[2])
			else
			    return redis.error_reply("Lock not found")
			end
		`, 1, r.lockId, r.lockVal, r.expiry())

	if reply.Err != nil {
		r.logErr("extend", reply.Err.Error())
		return reply.Err
	}

	if reply.String() != "OK" {
		r.logErr("extend", reply.String())
		return LockError
	}

	glog.Debugf("Extended lock %v:%v", r.lockId, r.lockVal)

	return nil
}

func (r *RedisLock) Unlock() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.client.Cmd("eval", `
			if redis.call("get", KEYS[1]) == ARGV[1] then
			    return redis.call("del", KEYS[1])
			else
			    return 0
			end
		`, 1, r.lockId, r.lockVal)

	glog.Debugf("Unlocked %v:%v", r.lockId, r.lockVal)
}

func (r *RedisLock) GetValue() (string, error) {
	// redis_lock GetValue() will be a bulk reply when lock doesn't exist - should turn in to appropriate error
	reply := r.client.Cmd("get", r.lockId)
	if reply.Err != nil {
		return "", reply.Err
	}
	return reply.Str()
}
