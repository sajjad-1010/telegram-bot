package db

import (
	"fmt"
	"strings"
)

type AudienceContact struct {
	UserID       int64
	ChatID       int64
	Username     string
	FirstName    string
	LastName     string
	ChatType     string
	ChatTitle    string
	ChatUsername string
	IsBot        bool
}

func ensureAudienceContactsTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS audience_contacts (
			user_id INTEGER NOT NULL,
			chat_id INTEGER NOT NULL,
			username TEXT,
			first_name TEXT,
			last_name TEXT,
			chat_type TEXT,
			chat_title TEXT,
			chat_username TEXT,
			is_bot INTEGER NOT NULL DEFAULT 0,
			first_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, chat_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create audience_contacts table: %w", err)
	}

	_, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_audience_contacts_chat_id ON audience_contacts(chat_id)`)
	if err != nil {
		return fmt.Errorf("create audience_contacts chat_id index: %w", err)
	}

	return nil
}

func UpsertAudienceContact(contact AudienceContact) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	if contact.UserID == 0 {
		return fmt.Errorf("user_id is required")
	}
	if contact.ChatID == 0 {
		return fmt.Errorf("chat_id is required")
	}

	_, err := DB.Exec(`
		INSERT INTO audience_contacts (
			user_id,
			chat_id,
			username,
			first_name,
			last_name,
			chat_type,
			chat_title,
			chat_username,
			is_bot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, chat_id) DO UPDATE SET
			username = excluded.username,
			first_name = excluded.first_name,
			last_name = excluded.last_name,
			chat_type = excluded.chat_type,
			chat_title = excluded.chat_title,
			chat_username = excluded.chat_username,
			is_bot = excluded.is_bot,
			last_seen_at = CURRENT_TIMESTAMP
	`,
		contact.UserID,
		contact.ChatID,
		normalizeUsername(contact.Username),
		strings.TrimSpace(contact.FirstName),
		strings.TrimSpace(contact.LastName),
		strings.TrimSpace(contact.ChatType),
		strings.TrimSpace(contact.ChatTitle),
		normalizeUsername(contact.ChatUsername),
		boolToInt(contact.IsBot),
	)
	if err != nil {
		return fmt.Errorf("upsert audience contact: %w", err)
	}

	return nil
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
