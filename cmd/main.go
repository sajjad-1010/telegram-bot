package main

import (
	"telegram-bot-go/config"
	"telegram-bot-go/internal/bot"
	"telegram-bot-go/internal/db"
	"fmt"
)

func main() {
	config.LoadEnv()
	db.Init(config.GetPostgresDSN())
	bot.RunBot()
}