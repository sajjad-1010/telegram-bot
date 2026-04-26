package db

import (
	"fmt"
	"strings"
	"telegram-bot/config"
)

func ensureRequiredChannelsTable() error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}

	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS required_channels (
			channel TEXT PRIMARY KEY
		)
	`)
	if err != nil {
		return fmt.Errorf("create required_channels table: %w", err)
	}
	return nil
}

func NormalizeChannel(channel string) string {
	c := strings.TrimSpace(strings.ToLower(channel))
	c = strings.TrimPrefix(c, "@")
	return c
}

func ListDynamicRequiredChannels() ([]string, error) {
	if DB == nil {
		return nil, fmt.Errorf("db is not initialized")
	}

	rows, err := DB.Query(`SELECT channel FROM required_channels ORDER BY channel`)
	if err != nil {
		return nil, fmt.Errorf("list required channels: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var channel string
		if err := rows.Scan(&channel); err != nil {
			return nil, fmt.Errorf("scan required channel: %w", err)
		}
		channel = NormalizeChannel(channel)
		if channel == "" {
			continue
		}
		result = append(result, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate required channels: %w", err)
	}

	return result, nil
}

func AddDynamicRequiredChannel(channel string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	channel = NormalizeChannel(channel)
	if channel == "" {
		return fmt.Errorf("channel is empty")
	}

	_, err := DB.Exec(`INSERT OR IGNORE INTO required_channels(channel) VALUES(?)`, channel)
	if err != nil {
		return fmt.Errorf("insert required channel: %w", err)
	}
	return nil
}

func RemoveDynamicRequiredChannel(channel string) error {
	if DB == nil {
		return fmt.Errorf("db is not initialized")
	}
	channel = NormalizeChannel(channel)
	if channel == "" {
		return fmt.Errorf("channel is empty")
	}

	_, err := DB.Exec(`DELETE FROM required_channels WHERE channel = ?`, channel)
	if err != nil {
		return fmt.Errorf("delete required channel: %w", err)
	}
	return nil
}

func ListAllRequiredChannels() ([]string, error) {
	staticChannels := listStaticRequiredChannels()
	dynamicChannels, err := ListDynamicRequiredChannels()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var all []string
	for _, ch := range append(staticChannels, dynamicChannels...) {
		ch = NormalizeChannel(ch)
		if ch == "" {
			continue
		}
		if _, ok := seen[ch]; ok {
			continue
		}
		seen[ch] = struct{}{}
		all = append(all, ch)
	}

	return all, nil
}

func listStaticRequiredChannels() []string {
	raw := config.GetEnv("BASE_REQUIRED_CHANNEL", "")
	if strings.TrimSpace(raw) == "" {
		// Backward compatibility with old key.
		raw = config.GetEnv("REQUIRED_CHANNELS", "")
	}
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = NormalizeChannel(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
