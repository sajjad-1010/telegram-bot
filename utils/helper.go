package utils

import "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func GetJoinChannelsKeyboard(channels []string) tgbotapi.InlineKeyboardMarkup {
    var rows [][]tgbotapi.InlineKeyboardButton
    for _, ch := range channels {
        btn := tgbotapi.NewInlineKeyboardButtonURL("عضویت در "+ch, "https://t.me/"+ch)
        rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
    }
    return tgbotapi.NewInlineKeyboardMarkup(rows...)
}