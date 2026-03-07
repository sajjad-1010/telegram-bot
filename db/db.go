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

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("opening sqlite db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping sqlite db: %w", err)
	}

	DB = db
	log.Println("SQLite connected at", dbPath)
	return nil
}
