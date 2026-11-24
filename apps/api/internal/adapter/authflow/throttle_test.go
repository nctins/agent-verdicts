package authflow

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

const addr = "203.0.113.7:51000"

func TestLoginThrottle_PairLocksAfterAttemptsAndExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := newLoginThrottle()
	th.now = func() time.Time { return now }

	for i := 0; i < pairMaxFailures; i++ {
		if !th.Begin("u", addr) {
			t.Fatalf("attempt %d refused before reaching the limit", i+1)
		}
	}
	if th.Begin("u", addr) {
		t.Fatal("attempt allowed after reaching the limit")
	}
	if !th.Begin("other", addr) {
		t.Fatal("lock leaked to another username")
	}
	now = now.Add(loginLockout + time.Second)
	if !th.Begin("u", addr) {
		t.Fatal("still locked after the lockout window")
	}

	th.Begin("v", addr)
	th.Succeed("v", addr)
	for i := 0; i < pairMaxFailures; i++ {
		if !th.Begin("v", addr) {
			t.Fatal("success did not reset the attempt count")
		}
	}
}

// A stranger guessing a known username from his own address must not lock
// the real user out of hers.
func TestLoginThrottle_PairLockDoesNotLockUsernameFromOtherIP(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < pairMaxFailures; i++ {
		th.Begin("alice", "198.51.100.9:1")
	}
	if th.Begin("alice", "198.51.100.9:2") {
		t.Fatal("the attacker's pair should be locked (any port)")
	}
	if !th.Begin("alice", "203.0.113.7:3") {
		t.Fatal("alice from another IP is locked by someone else's failures")
	}
}

func TestLoginThrottle_IPLocksAcrossUsernames(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < ipMaxFailures; i++ {
		if !th.Begin(fmt.Sprintf("user-%d", i), addr) {
			t.Fatalf("attempt %d refused before the IP limit", i+1)
		}
	}
	if th.Begin("one-more", addr) {
		t.Fatal("IP not locked after its limit")
	}
	if !th.Begin("one-more", "198.51.100.1:5") {
		t.Fatal("IP lock leaked to another IP")
	}
}

func TestLoginThrottle_UsernameLocksAcrossIPs(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < userMaxFailures; i++ {
		if !th.Begin("alice", fmt.Sprintf("10.0.%d.%d", i/200, i%200)) {
			t.Fatalf("attempt %d refused before the username limit", i+1)
		}
	}
	if th.Begin("alice", "172.16.0.1") {
		t.Fatal("username not locked after its limit across IPs")
	}
	if !th.Begin("bob", "172.16.0.1") {
		t.Fatal("username lock leaked to another username")
	}
}

// Below the 100 mark, failures of one IP do not lock the username elsewhere.
func TestLoginThrottle_OneIPDoesNotLockUsernameBelowLimit(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < pairMaxFailures; i++ {
		th.Begin("alice", addr)
	}
	if !th.Begin("alice", "198.51.100.1") {
		t.Fatal("alice locked from a second IP after only one IP's failures")
	}
}

func TestLoginThrottle_CountsExpireWithWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := newLoginThrottle()
	th.now = func() time.Time { return now }
	for i := 0; i < pairMaxFailures-1; i++ {
		th.Begin("u", addr)
	}
	now = now.Add(loginWindow + time.Second)
	for i := 0; i < pairMaxFailures; i++ {
		if !th.Begin("u", addr) {
			t.Fatal("old failures still counted after the window")
		}
	}
}

// Parallel attempts must not all slip past the check while the slow hash
// of the first ones is still running.
func TestLoginThrottle_ConcurrentAttemptsCapped(t *testing.T) {
	th := newLoginThrottle()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if th.Begin("alice", addr) {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != pairMaxFailures {
		t.Fatalf("allowed %d concurrent attempts, want %d", allowed, pairMaxFailures)
	}
}

// Spraying random usernames must not erase an active lockout.
func TestLoginThrottle_SprayDoesNotClearLockout(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < pairMaxFailures; i++ {
		th.Begin("alice", addr)
	}
	for i := 0; i < maxThrottleEntries+50; i++ {
		th.Begin(fmt.Sprintf("spray-%d", i), fmt.Sprintf("10.1.%d.%d", i/250, i%250))
	}
	if th.Begin("alice", addr) {
		t.Fatal("lockout erased by username spray")
	}
}

func TestLoginThrottle_ReleaseUndoesAttempt(t *testing.T) {
	th := newLoginThrottle()
	for i := 0; i < ipMaxFailures*3; i++ {
		th.Begin("x", addr)
		th.Release("x", addr)
	}
	if !th.Begin("x", addr) {
		t.Fatal("released attempts still counted")
	}
	if len(th.fails) > 3 {
		t.Fatalf("%d entries after release, want the 3 live counters", len(th.fails))
	}
}

func TestLoginThrottle_CustomLockout(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := newLoginThrottle()
	th.now = func() time.Time { return now }
	th.lockout = 30 * time.Second

	for i := 0; i < pairMaxFailures; i++ {
		th.Begin("u", addr)
	}
	now = now.Add(29 * time.Second)
	if th.Begin("u", addr) {
		t.Fatal("allowed before the custom lockout ended")
	}
	now = now.Add(2 * time.Second)
	if !th.Begin("u", addr) {
		t.Fatal("still locked after the custom lockout")
	}
}
