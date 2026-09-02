package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ensureLinksTable creates the single link table. It holds one row for each
// album (media group) posted in the source chat, so a deep link can rebuild
// the album later.
//
// A single-message link needs no row at all: the message_id is encoded inside
// the deep link itself.
func ensureLinksTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS links (
			media_group_id TEXT PRIMARY KEY,
			message_ids TEXT NOT NULL DEFAULT '',
			caption TEXT NOT NULL DEFAULT '',
			link_sent INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create links table: %w", err)
	}
	return ensureLinksCaptionColumn()
}

// ensureLinksCaptionColumn adds the caption column to a table created before
// captions were stored.
func ensureLinksCaptionColumn() error {
	rows, err := DB.Query(`PRAGMA table_info(links)`)
	if err != nil {
		return fmt.Errorf("inspect links columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notNull    int
			dfltValue  any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &primaryKey); err != nil {
			return fmt.Errorf("scan links column: %w", err)
		}
		if name == "caption" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := DB.Exec(`ALTER TABLE links ADD COLUMN caption TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add links caption column: %w", err)
	}
	return nil
}

// SetLinkCaption stores the album caption. Telegram puts the caption on one
// message of the album only, so the first non-empty value wins and later empty
// messages of the same album do not erase it.
func SetLinkCaption(mediaGroupID, caption string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}
	if caption == "" {
		return nil
	}

	_, err := DB.Exec(`
		INSERT INTO links (media_group_id, caption) VALUES (?, ?)
		ON CONFLICT(media_group_id) DO UPDATE SET
			caption = CASE WHEN links.caption = '' THEN excluded.caption ELSE links.caption END
	`, mediaGroupID, caption)
	if err != nil {
		return fmt.Errorf("set link caption: %w", err)
	}
	return nil
}

// GetLinkCaption returns the stored album caption, or "" when there is none.
func GetLinkCaption(mediaGroupID string) (string, error) {
	if DB == nil {
		return "", fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return "", fmt.Errorf("media_group_id is empty")
	}

	var caption string
	err := DB.QueryRow(`SELECT caption FROM links WHERE media_group_id = ?`, mediaGroupID).Scan(&caption)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get link caption: %w", err)
	}
	return caption, nil
}

// AddLinkMessage appends one album message ID to the row for mediaGroupID.
// It creates the row on the first message of the album.
//
// Telegram splits an album into separate updates that arrive at the same
// instant, and each update runs in its own goroutine. So this must be one
// atomic statement. A read-then-write pair would let two goroutines read the
// same list and both write it back, which drops a message from the album.
func AddLinkMessage(mediaGroupID string, messageID int) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}
	if messageID <= 0 {
		return fmt.Errorf("message_id must be positive")
	}

	id := strconv.Itoa(messageID)

	// The CASE keeps the append idempotent: a redelivered update finds its ID
	// already in the list and changes nothing.
	_, err := DB.Exec(`
		INSERT INTO links (media_group_id, message_ids) VALUES (?, ?)
		ON CONFLICT(media_group_id) DO UPDATE SET
			message_ids = CASE
				WHEN ',' || message_ids || ',' LIKE '%,' || ? || ',%' THEN message_ids
				WHEN message_ids = '' THEN ?
				ELSE message_ids || ',' || ?
			END
	`, mediaGroupID, id, id, id, id)
	if err != nil {
		return fmt.Errorf("append link message id: %w", err)
	}
	return nil
}

// GetLinkMessageIDs returns the album message IDs in ascending order.
func GetLinkMessageIDs(mediaGroupID string) ([]int, error) {
	if DB == nil {
		return nil, fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return nil, fmt.Errorf("media_group_id is empty")
	}

	var raw string
	err := DB.QueryRow(`SELECT message_ids FROM links WHERE media_group_id = ?`, mediaGroupID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query link message ids: %w", err)
	}
	ids := decodeMessageIDs(raw)
	sort.Ints(ids)
	return ids, nil
}

// IsLinkSent reports whether the bot already published the album deep link.
func IsLinkSent(mediaGroupID string) (bool, error) {
	if DB == nil {
		return false, fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return false, fmt.Errorf("media_group_id is empty")
	}

	var linkSent int
	err := DB.QueryRow(`SELECT link_sent FROM links WHERE media_group_id = ?`, mediaGroupID).Scan(&linkSent)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query link_sent: %w", err)
	}
	return linkSent != 0, nil
}

// MarkLinkSent records that the album deep link went out, so a late album
// message does not trigger a second link.
func MarkLinkSent(mediaGroupID string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}

	if _, err := DB.Exec(`UPDATE links SET link_sent = 1 WHERE media_group_id = ?`, mediaGroupID); err != nil {
		return fmt.Errorf("mark link sent: %w", err)
	}
	return nil
}

// CountLinks returns the number of stored album links.
func CountLinks() (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db is not initialized")
	}

	var total int64
	if err := DB.QueryRow(`SELECT COUNT(*) FROM links`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return total, nil
}

func encodeMessageIDs(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func decodeMessageIDs(raw string) []int {
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, id)
	}
	return out
}
