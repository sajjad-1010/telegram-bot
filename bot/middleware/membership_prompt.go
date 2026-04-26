package middleware

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/db"
	"telegram-bot/utils"
)

const CheckMembershipCallbackData = "check_membership"

func SendMembershipRequiredPrompt(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	channels, err := db.ListAllRequiredChannels()
	if err != nil {
		return fmt.Errorf("load required channels: %w", err)
	}

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
