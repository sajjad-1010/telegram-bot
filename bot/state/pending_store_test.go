package state

import (
	"testing"
	"time"
)

func newRAMStore(ttl time.Duration) *PendingStore {
	return &PendingStore{
		ttl: ttl,
		ram: make(map[pendingKey]ramPendingEntry),
		rl:  make(map[string]ramRateEntry),
	}
}

func TestPendingIsScopedPerUser(t *testing.T) {
	s := newRAMStore(time.Minute)
	s.Set(-100, 1, PendingRequest{Payload: "a"})
	s.Set(-100, 2, PendingRequest{Payload: "b"})

	if req, ok := s.Get(-100, 1); !ok || req.Payload != "a" {
		t.Fatalf("user 1 got %+v ok=%t, want payload a", req, ok)
	}
	if req, ok := s.Get(-100, 2); !ok || req.Payload != "b" {
		t.Fatalf("user 2 got %+v ok=%t, want payload b", req, ok)
	}
	if _, ok := s.Get(-100, 3); ok {
		t.Fatal("user 3 must not see another user's request")
	}
}

func TestTakeSucceedsOnce(t *testing.T) {
	s := newRAMStore(time.Minute)
	s.Set(5, 5, PendingRequest{Payload: "x"})

	if _, ok := s.Take(5, 5); !ok {
		t.Fatal("first Take should succeed")
	}
	if _, ok := s.Take(5, 5); ok {
		t.Fatal("second Take must fail so a double tap runs once")
	}
}

func TestRAMEntriesExpire(t *testing.T) {
	s := newRAMStore(time.Millisecond)
	s.Set(1, 1, PendingRequest{Payload: "x"})
	time.Sleep(5 * time.Millisecond)

	if _, ok := s.Get(1, 1); ok {
		t.Fatal("expired entry should not be returned")
	}
	s.Set(2, 2, PendingRequest{})
	if _, exists := s.ram[pendingKey{1, 1}]; exists {
		t.Fatal("expired entry should be pruned on the next Set")
	}
}
