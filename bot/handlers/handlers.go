package handlers

import (
    "log"
    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
    "telegram-bot/bot/middleware"
)

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
    if update.Message != nil {
        log.Println("Chat ID:", update.Message.Chat.ID)
    }

    if update.Message.IsCommand() && update.Message.Command() == "start" {
        handleStart(bot, update.Message)
    } else if update.Message.Text == "📥 دریافت فایل" {
        if middleware.IsUserMember(bot, update.Message.From.ID) {
            forwardContent(bot, update.Message.Chat.ID)
        } else {
            msg := tgbotapi.NewMessage(update.Message.Chat.ID, "برای دریافت فایل باید ابتدا عضو کانال‌ها شوید.")
            bot.Send(msg)
        }
    }
}

// این توابع باید تعریف شوند
func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
    text := "سلام! برای دریافت فایل‌ها روی دکمه زیر بزنید:\n\n📥 دریافت فایل"
    keyboard := tgbotapi.NewReplyKeyboard(
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("📥 دریافت فایل"),
        ),
    )
    reply := tgbotapi.NewMessage(msg.Chat.ID, text)
    reply.ReplyMarkup = keyboard
    bot.Send(reply)
}

func forwardContent(bot *tgbotapi.BotAPI, chatID int64) {
    fromChatID := int64(-1001234567890) // این رو با آیدی گروه خودت عوض کن
    messageID := 42                     // ID پیامی که میخوای فوروارد شه

    forward := tgbotapi.NewForward(chatID, fromChatID, messageID)
    bot.Send(forward)
}
