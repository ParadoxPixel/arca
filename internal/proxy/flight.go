package proxy

import "sync"

// Flight collapses concurrent work on the same key onto one call. A parallel
// Gradle build asks for the same artifact from every worker thread at once, and
// without this they would all race into the same storage key.
type Flight struct {
	mutex sync.Mutex
	calls map[string]*call
}

type call struct {
	done chan struct{}
	err  error
}

func NewFlight() *Flight {
	return &Flight{calls: map[string]*call{}}
}

// Do returns the error from the work function and whether this caller was the
// one that ran it. Followers wait for the leader and then read the shared
// result themselves, since the payload is a file on disk rather than a value.
func (f *Flight) Do(key string, work func() error) (error, bool) {
	f.mutex.Lock()
	if existing, waiting := f.calls[key]; waiting {
		f.mutex.Unlock()
		<-existing.done
		return existing.err, false
	}

	pending := &call{done: make(chan struct{})}
	f.calls[key] = pending
	f.mutex.Unlock()

	pending.err = work()

	f.mutex.Lock()
	delete(f.calls, key)
	f.mutex.Unlock()
	close(pending.done)

	return pending.err, true
}
