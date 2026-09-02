package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ensureUsersTable creates the single user table. One row for each user who
// ever touched the bot, in any chat. The row is written once and is never
// rewritten on later activity. Only an explicit /lang choice updates it.
func ensureUsersTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			user_id INTEGER PRIMARY KEY,
			username TEXT,
			language TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create users table: %w", err)
	}
	return nil
}

// AddUser inserts a user row on first contact. A user who is already present
// stays untouched, so no write happens on repeat activity.
func AddUser(userID int64, username string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	if userID == 0 {
		return fmt.Errorf("user_id is required")
	}

	username = strings.TrimPrefix(strings.TrimSpace(username), "@")

	_, err := DB.Exec(
		`INSERT OR IGNORE INTO users (user_id, username) VALUES (?, ?)`,
		userID, username,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// GetUserLanguage returns the stored language code for a user, or "" if unset.
func GetUserLanguage(userID int64) (string, error) {
	if DB == nil {
		return "", fmt.Errorf("db is not initialized")
	}

	var lang string
	err := DB.QueryRow(`SELECT COALESCE(language, '') FROM users WHERE user_id = ?`, userID).Scan(&lang)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get user language: %w", err)
	}
	return lang, nil
}

// SetUserLanguage stores the language the user picked with /lang.
func SetUserLanguage(userID int64, lang string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		INSERT INTO users (user_id, language) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET language = excluded.language
	`, userID, lang)
	if err != nil {
		return fmt.Errorf("set user language: %w", err)
	}
	return nil
}

// CountUsers returns the number of users the bot has ever seen.
func CountUsers() (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db is not initialized")
	}

	var total int64
	if err := DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return total, nil
}
