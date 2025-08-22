package middleware

import (
	"log"
	"strings"

	"telegram-bot/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func IsUserMember(bot *tgbotapi.BotAPI, userID int64) bool {
	log.Println("userId:", userID)
	channels := config.GetEnv("REQUIRED_CHANNELS", "")
	if channels == "" {
		log.Println("⚠ REQUIRED_CHANNELS is empty in .env")
		return false
	}

	channelList := strings.Split(channels, ",")
	for _, ch := range channelList {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}

		member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				SuperGroupUsername: "@" + ch,
				UserID:             userID,
			},
		})
		if err != nil {
			log.Printf("❌ Error checking membership for @%s: %v", ch, err)
			return false
		}

		log.Printf("ℹ User %d status in @%s: %s", userID, ch, member.Status)

		if member.Status != "member" && member.Status != "administrator" && member.Status != "creator" {
			return false
		}
	}

	return true
}
