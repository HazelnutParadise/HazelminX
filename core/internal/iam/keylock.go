package iam

import "sync"

// keyedLocks hands out one mutex per key. Callers on the same key run one at
// a time; callers on different keys never wait for each other. An entry
// lives only while someone holds or waits for it, so the map never holds
// more entries than there are calls in flight, however many distinct keys
// (including made-up usernames) have been seen.
type keyedLocks struct {
	mu sync.Mutex
	m  map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int // holders plus waiters, guarded by keyedLocks.mu
}

func newKeyedLocks() *keyedLocks { return &keyedLocks{m: map[string]*keyedLock{}} }

// lock blocks until key is free and returns the function that frees it.
func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	l := k.m[key]
	if l == nil {
		l = &keyedLock{}
		k.m[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

// size is the number of live entries (for tests).
func (k *keyedLocks) size() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.m)
}
