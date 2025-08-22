package utils
import "fmt"

import "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func GetJoinChannelsButtons(channels []string) []tgbotapi.InlineKeyboardButton {
    var buttons []tgbotapi.InlineKeyboardButton
    for _, ch := range channels {
        btn := tgbotapi.NewInlineKeyboardButtonURL("عضویت در "+ch, "https://t.me/"+ch)
        buttons = append(buttons, btn)
    }
    return buttons
}


func MakeLinkForForwardingMessage(msgID int) tgbotapi.InlineKeyboardButton {
    url := fmt.Sprintf("https://t.me/realblyat_bot?start=msg%d", msgID)
    return tgbotapi.NewInlineKeyboardButtonURL("مشاهده پیام", url)
}

