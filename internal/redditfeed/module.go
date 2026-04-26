package redditfeed

import (
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/config"
	"telegram-bot/db"
	"telegram-bot/internal/redditfeed/application"
	"telegram-bot/internal/redditfeed/domain"
	"telegram-bot/internal/redditfeed/infrastructure"
)

func Start(bot *tgbotapi.BotAPI) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		log.Println("reddit feed module disabled")
		return nil
	}
	if strings.TrimSpace(cfg.Subreddit) == "" || cfg.ChatID == 0 {
		log.Printf("reddit feed module disabled due to incomplete config subreddit=%q chat_id=%d", cfg.Subreddit, cfg.ChatID)
		return nil
	}

	service := application.NewService(
		cfg,
		infrastructure.NewRedditClient(config.GetEnv("REDDIT_USER_AGENT", "")),
		infrastructure.NewSQLiteStore(db.DB),
		infrastructure.NewTelegramPublisher(bot, cfg.ChatID),
	)

	return service.StartBackground()
}

func loadConfig() (domain.Config, error) {
	cfg := domain.Config{
		Enabled:       parseBoolEnv("REDDIT_FEED_ENABLED", false),
		Subreddit:     normalizeSubreddit(config.GetEnv("REDDIT_FEED_SUBREDDIT", "")),
		IntervalHours: parseIntEnv("REDDIT_FEED_INTERVAL_HOURS", 24),
		FetchLimit:    parseIntEnv("REDDIT_FEED_FETCH_LIMIT", 25),
		PostsPerTopic: parseIntEnv("REDDIT_FEED_POSTS_PER_TOPIC", 3),
		TopWindow:     strings.TrimSpace(strings.ToLower(config.GetEnv("REDDIT_FEED_TOP_WINDOW", "day"))),
		PollMinutes:   parseIntEnv("REDDIT_FEED_POLL_MINUTES", 15),
	}

	if raw := strings.TrimSpace(config.GetEnv("REDDIT_FEED_CHAT_ID", "")); raw != "" {
		chatID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return domain.Config{}, err
		}
		cfg.ChatID = chatID
	}

	return cfg, nil
}

func parseIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(config.GetEnv(key, ""))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(config.GetEnv(key, "")))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizeSubreddit(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimPrefix(raw, "/r/")
	raw = strings.TrimPrefix(raw, "r/")
	raw = strings.Trim(raw, "/")
	return raw
}
