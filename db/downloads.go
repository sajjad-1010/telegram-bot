package db

import "fmt"

// Download event status values.
const (
	DownloadStatusOK     = "ok"
	DownloadStatusFailed = "failed"
)

// PlatformCount pairs a platform label with its download count.
type PlatformCount struct {
	Platform string
	Count    int64
}

func ensureDownloadsTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS downloads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			platform TEXT NOT NULL,
			link TEXT NOT NULL,
			mode TEXT,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create downloads table: %w", err)
	}

	_, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_downloads_link ON downloads(link)`)
	if err != nil {
		return fmt.Errorf("create downloads link index: %w", err)
	}

	return nil
}

// LogDownload records one download attempt (success or failure).
func LogDownload(userID int64, platform, link, mode, status string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(
		`INSERT INTO downloads (user_id, platform, link, mode, status) VALUES (?, ?, ?, ?, ?)`,
		userID, platform, link, mode, status,
	)
	if err != nil {
		return fmt.Errorf("insert download event: %w", err)
	}
	return nil
}

// CountDownloads returns the total number of download events.
func CountDownloads() (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db is not initialized")
	}

	var total int64
	if err := DB.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count downloads: %w", err)
	}
	return total, nil
}

// CountDownloadsByStatus returns the number of download events with the given status.
func CountDownloadsByStatus(status string) (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db is not initialized")
	}

	var total int64
	if err := DB.QueryRow(`SELECT COUNT(*) FROM downloads WHERE status = ?`, status).Scan(&total); err != nil {
		return 0, fmt.Errorf("count downloads by status: %w", err)
	}
	return total, nil
}

// DownloadsByPlatform returns per-platform download counts, most frequent first.
func DownloadsByPlatform() ([]PlatformCount, error) {
	if DB == nil {
		return nil, fmt.Errorf("db is not initialized")
	}

	rows, err := DB.Query(`
		SELECT platform, COUNT(*) AS c
		FROM downloads
		GROUP BY platform
		ORDER BY c DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query downloads by platform: %w", err)
	}
	defer rows.Close()

	var out []PlatformCount
	for rows.Next() {
		var pc PlatformCount
		if err := rows.Scan(&pc.Platform, &pc.Count); err != nil {
			return nil, fmt.Errorf("scan platform count: %w", err)
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}
