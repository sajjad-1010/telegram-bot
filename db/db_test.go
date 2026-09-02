package db

import (
	"path/filepath"
	"testing"
)

// TestInitEnablesWAL guards the album fix. With the default rollback journal
// and no busy timeout, concurrent album writes fail with SQLITE_BUSY and the
// bot links only one message of the album.
func TestInitEnablesWAL(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer DB.Close()

	var mode string
	if err := DB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode must be wal, got %q", mode)
	}

	var timeout int
	if err := DB.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if timeout < 1000 {
		t.Fatalf("busy_timeout must be at least 1000ms, got %d", timeout)
	}
}
