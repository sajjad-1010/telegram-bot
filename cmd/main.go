package main

import (
	"log"

	"telegram-bot/bot"
	"telegram-bot/config"
	"telegram-bot/db"
)

func main() {
	config.LoadEnv()
	dbPath := config.GetEnv("DB_PATH", "data/bot.db")
	if err := db.Init(dbPath); err != nil {
		log.Fatal(err)
	}
	if err := bot.Start(); err != nil {
		log.Fatal(err)
	}
}
