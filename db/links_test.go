package db

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestAddLinkMessageConcurrent reproduces the album bug: Telegram delivers each
// album message as its own update and the bot handles each in its own
// goroutine. Every message must survive.
func TestAddLinkMessageConcurrent(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer DB.Close()

	const groupID = "test-album"
	const items = 10

	var wg sync.WaitGroup
	for i := 1; i <= items; i++ {
		wg.Add(1)
		go func(messageID int) {
			defer wg.Done()
			if err := AddLinkMessage(groupID, messageID); err != nil {
				t.Errorf("add message %d: %v", messageID, err)
			}
		}(i)
	}
	wg.Wait()

	got, err := GetLinkMessageIDs(groupID)
	if err != nil {
		t.Fatalf("get message ids: %v", err)
	}
	if len(got) != items {
		t.Fatalf("lost album messages: want %d, got %d (%v)", items, len(got), got)
	}
	for i, id := range got {
		if id != i+1 {
			t.Fatalf("ids are not sorted: got %v", got)
		}
	}
}

// TestAddLinkMessageIdempotent covers a redelivered update.
func TestAddLinkMessageIdempotent(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer DB.Close()

	for i := 0; i < 3; i++ {
		if err := AddLinkMessage("g", 42); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	// 4 must not match the "42" prefix.
	if err := AddLinkMessage("g", 4); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := GetLinkMessageIDs("g")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 || got[0] != 4 || got[1] != 42 {
		t.Fatalf("want [4 42], got %v", got)
	}
}

// TestLinkCaptionFirstNonEmptyWins covers how Telegram delivers an album: only
// one message carries the caption and the others arrive empty, in any order.
func TestLinkCaptionFirstNonEmptyWins(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer DB.Close()

	const groupID = "album"
	const want = "استیکر توت فرنگی"

	// The captioned message is not the first one to arrive.
	if err := AddLinkMessage(groupID, 1); err != nil {
		t.Fatal(err)
	}
	if err := SetLinkCaption(groupID, ""); err != nil {
		t.Fatal(err)
	}
	if err := AddLinkMessage(groupID, 2); err != nil {
		t.Fatal(err)
	}
	if err := SetLinkCaption(groupID, want); err != nil {
		t.Fatal(err)
	}
	if err := AddLinkMessage(groupID, 3); err != nil {
		t.Fatal(err)
	}
	if err := SetLinkCaption(groupID, ""); err != nil {
		t.Fatal(err)
	}

	got, err := GetLinkCaption(groupID)
	if err != nil {
		t.Fatalf("get caption: %v", err)
	}
	if got != want {
		t.Fatalf("caption: want %q, got %q", want, got)
	}

	ids, err := GetLinkMessageIDs(groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 message ids, got %v", ids)
	}
}

// TestLinkCaptionOnlyMessage covers an album stored with no caption at all.
func TestLinkCaptionOnlyMessage(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer DB.Close()

	if err := AddLinkMessage("g", 5); err != nil {
		t.Fatal(err)
	}
	got, err := GetLinkCaption("g")
	if err != nil {
		t.Fatalf("get caption: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty caption, got %q", got)
	}
}
