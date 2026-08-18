package middleware

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/utils"
)

const CheckMembershipCallbackData = "check_membership"

// SendMembershipRequiredPrompt shows join buttons only for the channels the
// user still has to join, so already-joined ones are not offered again.
func SendMembershipRequiredPrompt(bot *tgbotapi.BotAPI, chatID int64, userID int64, text string) error {
	channels, err := MissingChannels(bot, userID)
	if err != nil {
		return fmt.Errorf("load required channels: %w", err)
	}
	log.Printf("membership prompt chat_id=%d user_id=%d missing=%v", chatID, userID, channels)

	msg := tgbotapi.NewMessage(chatID, text)
	joinButtons := utils.GetJoinChannelsButtons(channels)

	rows := [][]tgbotapi.InlineKeyboardButton{}
	if len(joinButtons) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(joinButtons...))
	}
	checkBtn := tgbotapi.NewInlineKeyboardButtonData("Check membership", CheckMembershipCallbackData)
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(checkBtn))

	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, err = bot.Send(msg)
	if err != nil {
		return fmt.Errorf("send membership prompt: %w", err)
	}

	return nil
}
