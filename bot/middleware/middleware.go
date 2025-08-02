package middleware

import (
    "os"
    "strconv"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Bot را از بیرون می‌گیریم
func IsUserMember(bot *tgbotapi.BotAPI, userID int) bool {
    requiredChannels := strings.Split(os.Getenv("REQUIRED_CHANNELS"), ",")
    for _, ch := range requiredChannels {
        chatID := chID(ch)
        member, err := bot.GetChatMember(tgbotapi.ChatConfigWithUser{
            ChatID: chatID,
            UserID: userID,
        })
        if err != nil || member.Status == "left" {
            return false
        }
    }
    return true
}

func chID(username string) int64 {
    if strings.HasPrefix(username, "@") {
        username = username[1:]
    }
    // اگر مقدار عددی باشه به int64 تبدیلش کن
    if id, err := strconv.ParseInt(username, 10, 64); err == nil {
        return id
    }
    // در غیر این صورت API خودش با username کار می‌کنه (ولی معمولاً باید عدد باشه)
    return 0
}
