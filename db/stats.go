package db

import "fmt"

// CountUniqueUsers returns the number of distinct users seen across all chats.
func CountUniqueUsers() (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db is not initialized")
	}

	var total int64
	if err := DB.QueryRow(`SELECT COUNT(DISTINCT user_id) FROM audience_contacts`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count unique users: %w", err)
	}
	return total, nil
}

// ChatTypeCount pairs a chat_type label with the number of distinct chats of that type.
type ChatTypeCount struct {
	ChatType string
	Count    int64
}

// CountChatsByType returns distinct-chat counts grouped by chat_type.
func CountChatsByType() ([]ChatTypeCount, error) {
	if DB == nil {
		return nil, fmt.Errorf("db is not initialized")
	}

	rows, err := DB.Query(`
		SELECT chat_type, COUNT(DISTINCT chat_id) AS c
		FROM audience_contacts
		GROUP BY chat_type
		ORDER BY c DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("count chats by type: %w", err)
	}
	defer rows.Close()

	var out []ChatTypeCount
	for rows.Next() {
		var ctc ChatTypeCount
		if err := rows.Scan(&ctc.ChatType, &ctc.Count); err != nil {
			return nil, fmt.Errorf("scan chat type count: %w", err)
		}
		out = append(out, ctc)
	}
	return out, rows.Err()
}
