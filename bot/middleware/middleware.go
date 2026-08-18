package middleware

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/db"
)

// MissingChannels returns the required channels the user has not joined yet.
// A channel whose check fails is reported as missing, so the gate stays closed
// when Telegram cannot answer.
func MissingChannels(bot *tgbotapi.BotAPI, userID int64) ([]string, error) {
	channelList, err := db.ListAllRequiredChannels()
	if err != nil {
		return nil, fmt.Errorf("load required channels: %w", err)
	}

	var missing []string
	for _, ch := range channelList {
		member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				SuperGroupUsername: "@" + ch,
				UserID:             userID,
			},
		})
		if err != nil {
			log.Printf("Error checking membership for @%s: %v", ch, err)
			missing = append(missing, ch)
			continue
		}

		log.Printf("User %d status in @%s: %s", userID, ch, member.Status)

		if member.Status != "member" && member.Status != "administrator" && member.Status != "creator" {
			missing = append(missing, ch)
		}
	}

	return missing, nil
}

func IsUserMember(bot *tgbotapi.BotAPI, userID int64) bool {
	log.Printf("checking membership user_id=%d", userID)

	missing, err := MissingChannels(bot, userID)
	if err != nil {
		log.Println("Failed to load required channels:", err)
		return false
	}
	if len(missing) > 0 {
		log.Printf("user_id=%d not joined: %v", userID, missing)
		return false
	}

	return true
}
