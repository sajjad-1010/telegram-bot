package config

import (
	"github.com/joho/godotenv"
	"log"
	"os"
	"strings"
)

func LoadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}
}

func GetBotToken() string {
	return os.Getenv("BOT_TOKEN")
}

func GetChannelIDs() []string {
	return strings.Split(os.Getenv("CHANNEL_IDS"), ",")
}

func GetFilePath() string {
	return os.Getenv("FILE_BASE_PATH")
}

func GetPostgresDSN() string {
	return os.Getenv("POSTGRES_DSN")
}