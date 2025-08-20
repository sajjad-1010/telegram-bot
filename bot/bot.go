package bot

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/bot/handlers"
	"telegram-bot/config"
)

var Bot *tgbotapi.BotAPI

func Start() error {
	// بارگذاری متغیرهای محیطی از فایل .env
	config.LoadEnv()

	// گرفتن توکن ربات با امکان fallback
	token := config.GetEnv("BOT_TOKEN", "")
	if token == "" {
		return logError("BOT_TOKEN is not set")
	}

	var err error
	Bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		return err
	}
	log.Println("Bot started")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := Bot.GetUpdatesChan(u)

	for update := range updates {
		handlers.HandleUpdate(Bot, update)
	}

	return nil
}

func logError(msg string) error {
	log.Println("⚠", msg)
	return &customError{msg}
}

type customError struct {
	msg string
}

func (e *customError) Error() string {
	return e.msg
}
