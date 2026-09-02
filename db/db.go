package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

// Init opens (and creates if missing) the SQLite database at the given path and pings it.
func Init(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating db directory: %w", err)
	}

	// Every Telegram update runs in its own goroutine, so album messages hit the
	// database at the same instant. The default rollback journal rejects a second
	// writer with SQLITE_BUSY straight away and the write is lost. WAL plus a busy
	// timeout makes the second writer wait for its turn instead.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		dbPath,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("opening sqlite db: %w", err)
	}

	// SQLite takes one writer at a time. A single connection removes lock
	// contention between goroutines and costs nothing at this traffic level.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping sqlite db: %w", err)
	}

	DB = db

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return fmt.Errorf("read journal_mode: %w", err)
	}
	log.Printf("SQLite connected at %s journal_mode=%s", dbPath, journalMode)

	if err := ensureRequiredChannelsTable(); err != nil {
		return err
	}
	if err := ensureUsersTable(); err != nil {
		return err
	}
	if err := ensureLinksTable(); err != nil {
		return err
	}
	if err := ensureMediaCacheTable(); err != nil {
		return err
	}
	return nil
}
