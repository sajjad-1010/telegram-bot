package middleware

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/db"
)

func IsUserMember(bot *tgbotapi.BotAPI, userID int64) bool {
	log.Printf("checking membership user_id=%d", userID)
	channelList, err := db.ListAllRequiredChannels()
	if err != nil {
		log.Println("Failed to load required channels:", err)
		return false
	}
	if len(channelList) == 0 {
		log.Println("No required channels configured")
		return true
	}

	for _, ch := range channelList {
		member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				SuperGroupUsername: "@" + ch,
				UserID:             userID,
			},
		})
		if err != nil {
			log.Printf("Error checking membership for @%s: %v", ch, err)
			return false
		}

		log.Printf("User %d status in @%s: %s", userID, ch, member.Status)

		if member.Status != "member" && member.Status != "administrator" && member.Status != "creator" {
			return false
		}
	}

	return true
}
