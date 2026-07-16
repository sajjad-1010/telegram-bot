package db

import (
	"database/sql"
	"fmt"
)

// Media cache modes.
const (
	MediaCacheModeMP4 = "mp4"
	MediaCacheModeMP3 = "mp3"
)

func ensureMediaCacheTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS media_cache (
			link TEXT NOT NULL,
			mode TEXT NOT NULL,
			file_id TEXT NOT NULL,
			file_type TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (link, mode)
		)
	`)
	if err != nil {
		return fmt.Errorf("create media_cache table: %w", err)
	}
	return nil
}

// GetCachedMedia returns the cached Telegram file_id and file_type for a
// (link, mode) pair. ok is false when there is no cache entry.
func GetCachedMedia(link, mode string) (fileID, fileType string, ok bool, err error) {
	if DB == nil {
		return "", "", false, fmt.Errorf("db is not initialized")
	}

	row := DB.QueryRow(`SELECT file_id, file_type FROM media_cache WHERE link = ? AND mode = ?`, link, mode)
	if scanErr := row.Scan(&fileID, &fileType); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("get cached media: %w", scanErr)
	}
	return fileID, fileType, true, nil
}

// UpsertMediaCache stores or refreshes the cached file_id for a (link, mode) pair.
func UpsertMediaCache(link, mode, fileID, fileType string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		INSERT INTO media_cache (link, mode, file_id, file_type)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(link, mode) DO UPDATE SET
			file_id = excluded.file_id,
			file_type = excluded.file_type,
			created_at = CURRENT_TIMESTAMP
	`, link, mode, fileID, fileType)
	if err != nil {
		return fmt.Errorf("upsert media cache: %w", err)
	}
	return nil
}

// DeleteMediaCache removes a stale cache entry (e.g. when Telegram rejects the file_id).
func DeleteMediaCache(link, mode string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	if _, err := DB.Exec(`DELETE FROM media_cache WHERE link = ? AND mode = ?`, link, mode); err != nil {
		return fmt.Errorf("delete media cache: %w", err)
	}
	return nil
}
