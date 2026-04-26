package db

import (
	"database/sql"
	"fmt"
)

func ensureSourceMediaGroupsTables() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS source_media_groups (
			media_group_id TEXT PRIMARY KEY,
			link_sent INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create source_media_groups table: %w", err)
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS source_media_group_items (
			media_group_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			PRIMARY KEY (media_group_id, message_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create source_media_group_items table: %w", err)
	}

	_, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_source_media_group_items_message_id ON source_media_group_items(message_id)`)
	if err != nil {
		return fmt.Errorf("create source_media_group_items message_id index: %w", err)
	}

	return nil
}

func AddSourceMediaGroupItem(mediaGroupID string, messageID int) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}
	if messageID <= 0 {
		return fmt.Errorf("message_id must be positive")
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("begin source media group tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		INSERT INTO source_media_groups(media_group_id, link_sent)
		VALUES(?, 0)
		ON CONFLICT(media_group_id) DO UPDATE SET
			updated_at = CURRENT_TIMESTAMP
	`, mediaGroupID); err != nil {
		return fmt.Errorf("upsert source_media_groups: %w", err)
	}

	if _, err = tx.Exec(`
		INSERT OR IGNORE INTO source_media_group_items(media_group_id, message_id)
		VALUES(?, ?)
	`, mediaGroupID, messageID); err != nil {
		return fmt.Errorf("insert source_media_group_items: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit source media group tx: %w", err)
	}

	return nil
}

func IsSourceMediaGroupLinkSent(mediaGroupID string) (bool, error) {
	if DB == nil {
		return false, fmt.Errorf("db is not initialized")
	}
	if mediaGroupID == "" {
		return false, fmt.Errorf("media_group_id is empty")
	}

	var linkSent int
	err := DB.QueryRow(`SELECT link_sent FROM source_media_groups WHERE media_group_id = ?`, mediaGroupID).Scan(&linkSent)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query source media group link_sent: %w", err)
	}
	return linkSent != 0, nil
}

func MarkSourceMediaGroupLinkSent(mediaGroupID string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}

	_, err := DB.Exec(`
		UPDATE source_media_groups
		SET link_sent = 1, updated_at = CURRENT_TIMESTAMP
		WHERE media_group_id = ?
	`, mediaGroupID)
	if err != nil {
		return fmt.Errorf("mark source media group link sent: %w", err)
	}
	return nil
}

func GetSourceMediaGroupMessageIDs(mediaGroupID string) ([]int, error) {
	if DB == nil {
		return nil, fmt.Errorf("db is not initialized")
	}
	if mediaGroupID == "" {
		return nil, fmt.Errorf("media_group_id is empty")
	}

	rows, err := DB.Query(`
		SELECT message_id
		FROM source_media_group_items
		WHERE media_group_id = ?
		ORDER BY message_id
	`, mediaGroupID)
	if err != nil {
		return nil, fmt.Errorf("query source media group items: %w", err)
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var messageID int
		if err := rows.Scan(&messageID); err != nil {
			return nil, fmt.Errorf("scan source media group item: %w", err)
		}
		out = append(out, messageID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source media group items: %w", err)
	}

	return out, nil
}
