package server

import "sync"

// keyedMutex serialises work on one key at a time, for the writes whose
// correctness rests on a check that ran a moment earlier: an npm version may be
// published once, and two publishes of it racing each other would both pass the
// existence check before either had stored anything.
type keyedMutex struct {
	mutex sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mutex   sync.Mutex
	waiters int
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{locks: map[string]*keyedLock{}} }

// lock acquires the key and returns what releases it. Entries are dropped once
// nobody holds or waits for them, so the map stays as small as the contention.
func (k *keyedMutex) lock(key string) func() {
	k.mutex.Lock()
	entry, ok := k.locks[key]
	if !ok {
		entry = &keyedLock{}
		k.locks[key] = entry
	}
	entry.waiters++
	k.mutex.Unlock()

	entry.mutex.Lock()

	return func() {
		entry.mutex.Unlock()

		k.mutex.Lock()
		entry.waiters--
		if entry.waiters == 0 {
			delete(k.locks, key)
		}
		k.mutex.Unlock()
	}
}
