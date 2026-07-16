package bot

import (
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/bot/handlers"
	"telegram-bot/config"
	"telegram-bot/internal/redditfeed"
)

var Bot *tgbotapi.BotAPI

func Start() error {
	token := config.GetEnv("BOT_TOKEN", "")
	if token == "" {
		return logError("BOT_TOKEN is not set")
	}

	var err error
	log.Println("Creating bot client and calling getMe...")
	localAPIURL := config.GetEnv("TELEGRAM_LOCAL_API_URL", "")
	if localAPIURL != "" {
		endpoint := strings.TrimRight(localAPIURL, "/") + "/bot%s/%s"
		Bot, err = tgbotapi.NewBotAPIWithAPIEndpoint(token, endpoint)
		log.Printf("Using local Bot API server: %s", localAPIURL)
	} else {
		Bot, err = tgbotapi.NewBotAPI(token)
	}
	if err != nil {
		return err
	}
	log.Printf("Bot authorized as @%s (id=%d)", Bot.Self.UserName, Bot.Self.ID)
	log.Printf("config: rate_limit_per_hour=%s media_cache=%s default_quality=%s default_lang=%s",
		config.GetEnv("RATE_LIMIT_PER_HOUR", "30"),
		config.GetEnv("MEDIA_CACHE_ENABLED", "true"),
		config.GetEnv("DEFAULT_VIDEO_QUALITY", "best"),
		config.GetEnv("DEFAULT_LANG", "en"),
	)

	if err := redditfeed.Start(Bot); err != nil {
		return err
	}
	log.Println("Bot started, listening for updates...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := Bot.GetUpdatesChan(u)

	for update := range updates {
		go handlers.HandleUpdate(Bot, update)
	}

	return nil
}

func logError(msg string) error {
	log.Println("ERROR:", msg)
	return &customError{msg}
}

type customError struct {
	msg string
}

func (e *customError) Error() string {
	return e.msg
}
