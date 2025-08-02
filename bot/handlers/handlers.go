package handlers

import (
    "log"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
    "telegram-bot/bot/middleware"
)

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
    if update.Message != nil {
        log.Println("=== MESSAGE ===")
        log.Println("Chat ID:", update.Message.Chat.ID)
        log.Println("Message ID:", update.Message.MessageID)
        log.Println("Text:", update.Message.Text)

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
        return
    }

    if update.ChannelPost != nil {
        log.Println("=== CHANNEL POST ===")
        log.Println("Channel ID:", update.ChannelPost.Chat.ID)
        log.Println("Post ID:", update.ChannelPost.MessageID)
        log.Println("Text:", update.ChannelPost.Text)
        return
    }

    log.Println("Update type not handled")
}

// --- بقیه توابع مثل قبل ---
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
    fromChatID := int64(-1001234567890)
    messageID := 42

    forward := tgbotapi.NewForward(chatID, fromChatID, messageID)
    bot.Send(forward)
}
