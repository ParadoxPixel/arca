package auth

import (
	"testing"
	"time"
)

func TestThrottleBlocksAfterTheLimitAndForgetsAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	throttle := NewThrottle(3, time.Minute)
	throttle.now = func() time.Time { return now }

	for attempt := 0; attempt < 3; attempt++ {
		if ok, _ := throttle.Allow("key"); !ok {
			t.Fatalf("attempt %d was refused before the limit", attempt)
		}
		throttle.Fail("key")
	}

	ok, wait := throttle.Allow("key")
	if ok {
		t.Fatal("the fourth attempt must be refused")
	}
	if wait != time.Minute {
		t.Fatalf("wait = %s, want the rest of the window", wait)
	}
	if ok, _ := throttle.Allow("other"); !ok {
		t.Fatal("another key is unaffected")
	}

	now = now.Add(time.Minute)
	if ok, _ := throttle.Allow("key"); !ok {
		t.Fatal("the window has passed, the key must be allowed again")
	}
}

func TestGuardCombinesAddressAndAccount(t *testing.T) {
	guard := NewGuard()

	for attempt := 0; attempt < accountFailures; attempt++ {
		guard.Fail("10.0.0.1", "victim@example.com")
	}
	if ok, _ := guard.Allow("10.0.0.2", "victim@example.com"); ok {
		t.Fatal("the account is throttled from every address")
	}
	if ok, _ := guard.Allow("10.0.0.1", "someone-else@example.com"); !ok {
		t.Fatal("the address has budget left for other accounts")
	}
}

func TestCredentialCacheIsBoundToTheStoredHash(t *testing.T) {
	cache := newCredentialCache()

	key := credentialKey("me@example.com", "password", "hash-1")
	cache.add(key)
	if !cache.has(key) {
		t.Fatal("a verified pair is remembered")
	}
	if cache.has(credentialKey("me@example.com", "password", "hash-2")) {
		t.Fatal("a changed password hash invalidates the entry")
	}

	now := time.Now().Add(verifiedTTL + time.Second)
	cache.now = func() time.Time { return now }
	if cache.has(key) {
		t.Fatal("an entry expires")
	}
}

func TestDecoyHashIsAValidHashNoPasswordMatches(t *testing.T) {
	if VerifyPassword("", decoyHash()) || VerifyPassword("password", decoyHash()) {
		t.Fatal("the decoy must never verify")
	}
	if decoyHash() != decoyHash() {
		t.Fatal("the decoy is derived once")
	}
}
