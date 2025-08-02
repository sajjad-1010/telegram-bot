package main

import (
    "log"
    "telegram-bot/bot"
    "telegram-bot/config"
)

func main() {
    config.LoadEnv()
    if err := bot.Start(); err != nil {
        log.Fatal(err)
    }
}