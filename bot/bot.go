package bot

import (
    "os"
    "log"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
    "telegram-bot/bot/handlers"
)

var Bot *tgbotapi.BotAPI

func Start() error {
    token := os.Getenv("BOT_TOKEN")
    var err error
    Bot, err = tgbotapi.NewBotAPI(token)
    if err != nil {
        return err
    }
    log.Println("Bot started")

    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60

    updates, err := Bot.GetUpdatesChan(u)
    if err != nil {
        return err
    }

    for update := range updates {
        log.Printf("RAW UPDATE: %+v\n", update)
        handlers.HandleUpdate(Bot, update)
    }
    return nil
}