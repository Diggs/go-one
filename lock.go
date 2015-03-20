package gone

type GoneLock interface {
	Lock() error
	Extend() error
	Unlock()
	GetValue() (string, error)
}

type LockFactory func(string, string) (GoneLock, error)
