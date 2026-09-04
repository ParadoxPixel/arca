package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Password checks are deliberately slow, which is a property an attacker can use
// twice over: to guess passwords at leisure, and to pin the CPU with nothing more
// than a stream of wrong ones. The throttle counts failures and refuses to run
// another derivation once a client address or an account has failed too often in
// a short window. Successes are never counted, so a build that sends the right
// credentials on every request is never slowed down.

const (
	// throttleWindow is how long a run of failures is held against a key. It is
	// short on purpose: the point is to make guessing slow, not to lock a person
	// out of their account for the afternoon because a script had a stale password.
	throttleWindow = 5 * time.Minute
	// addressFailures is what one client address may fail within the window.
	addressFailures = 100
	// accountFailures is what one account may fail within the window, from
	// anywhere. It caps a distributed guess at a few thousand tries a day.
	accountFailures = 20
)

type failureWindow struct {
	count   int
	resetAt time.Time
}

// Throttle is a fixed-window failure counter per key.
type Throttle struct {
	mutex     sync.Mutex
	limit     int
	window    time.Duration
	failures  map[string]failureWindow
	lastPrune time.Time
	now       func() time.Time
}

func NewThrottle(limit int, window time.Duration) *Throttle {
	return &Throttle{
		limit:    limit,
		window:   window,
		failures: map[string]failureWindow{},
		now:      time.Now,
	}
}

// Allow reports whether the key may attempt another authentication, and how long
// it has to wait when it may not.
func (t *Throttle) Allow(key string) (bool, time.Duration) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := t.now()
	entry, ok := t.failures[key]
	if !ok || !now.Before(entry.resetAt) {
		return true, 0
	}
	if entry.count < t.limit {
		return true, 0
	}
	return false, entry.resetAt.Sub(now)
}

// Fail records one failed attempt against the key.
func (t *Throttle) Fail(key string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := t.now()
	t.pruneLocked(now)

	entry, ok := t.failures[key]
	if !ok || !now.Before(entry.resetAt) {
		entry = failureWindow{resetAt: now.Add(t.window)}
	}
	entry.count++
	t.failures[key] = entry
}

// pruneLocked drops expired windows, at most once per window, so the map stays
// bounded by the number of distinct keys that failed recently.
func (t *Throttle) pruneLocked(now time.Time) {
	if now.Sub(t.lastPrune) < t.window {
		return
	}
	t.lastPrune = now
	for key, entry := range t.failures {
		if !now.Before(entry.resetAt) {
			delete(t.failures, key)
		}
	}
}

// Guard is the pair of throttles every password check consults: one per client
// address, one per account.
type Guard struct {
	addresses *Throttle
	accounts  *Throttle
}

func NewGuard() *Guard {
	return &Guard{
		addresses: NewThrottle(addressFailures, throttleWindow),
		accounts:  NewThrottle(accountFailures, throttleWindow),
	}
}

// Allow reports whether a password check may run for the address and account,
// and the wait to advertise when it may not.
func (g *Guard) Allow(address, account string) (bool, time.Duration) {
	if ok, wait := g.addresses.Allow(address); !ok {
		return false, wait
	}
	if ok, wait := g.accounts.Allow(account); !ok {
		return false, wait
	}
	return true, 0
}

func (g *Guard) Fail(address, account string) {
	g.addresses.Fail(address)
	g.accounts.Fail(account)
}

// ClientAddress is the peer the connection came from. It is deliberately not
// read from X-Forwarded-For, which any client can set: behind a proxy every
// client shares the proxy's address and the per-account throttle carries the
// load, while a forged header would let a guesser reset its own budget at will.
func ClientAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
