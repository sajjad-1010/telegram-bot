package handlers

import (
	"log"
	"strconv"
	"strings"

	"telegram-bot/bot/middleware"
	"telegram-bot/config"
	"telegram-bot/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	requiredChannels  []string
	forwardFromChatID int64
	forwardMessageID  int
)

func init() {
	config.LoadEnv()

	channels := config.GetEnv("REQUIRED_CHANNELS", "")
	if channels != "" {
		requiredChannels = strings.Split(channels, ",")
	} else {
		requiredChannels = []string{}
	}

	chatIDStr := config.GetEnv("FORWARD_FROM_CHAT_ID", "")
	if chatIDStr != "" {
		id, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			log.Println("Error parsing FORWARD_FROM_CHAT_ID:", err)
		} else {
			forwardFromChatID = id
		}
	} else {
		log.Println("FORWARD_FROM_CHAT_ID is not set!")
	}

	msgIDStr := config.GetEnv("FORWARD_MESSAGE_ID", "")
	if msgIDStr != "" {
		id, err := strconv.Atoi(msgIDStr)
		if err != nil {
			log.Println("Error parsing FORWARD_MESSAGE_ID:", err)
		} else {
			forwardMessageID = id
		}
	} else {
		log.Println("FORWARD_MESSAGE_ID is not set!")
	}

	// log.Println("Loaded ENV:", requiredChannels, forwardFromChatID, forwardMessageID)
}

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	//     log.Println("Text:", update.Message.Text)
	if update.Message != nil {
		log.Println("=== A NEW MESSAGE FROM CHAT===")
		log.Println("Chat ID:", update.Message.Chat.ID)
		log.Println("Message ID:", update.Message.MessageID)
		// log.Println("#####Message :", update.Message, "#####")

		if update.Message.IsCommand() && update.Message.Command() == "start" {
			args := update.Message.CommandArguments()
			if args != "" {
				handleStartWithArgs(bot, update.Message.Chat.ID, args)
			} else {
				handleStart(bot, update.Message)
			}
		}
	}
}

func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {

	text := "برای دریافت فایل باید لینک مخصوص فایل رو کلیک کنی "
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	bot.Send(reply)
}

func handleStartWithArgs(bot *tgbotapi.BotAPI, chatID int64, args string) {
	log.Println("📌 handleStartWithArgs called with args:", args)
	idStr := strings.TrimPrefix(args, "msg")
	msgID, err := strconv.Atoi(idStr)
	if err == nil {
		if middleware.IsUserMember(bot, chatID) {
			forward := tgbotapi.NewForward(chatID, forwardFromChatID, msgID)
			if _, err := bot.Send(forward); err != nil {
				log.Println("❌ Error forwarding message:", err)
			}
		} else {
			notMemberMsg := tgbotapi.NewMessage(chatID, "برای دریافت فایل باید ابتدا عضو کانال‌ها شوید.")

			joinButtons := utils.GetJoinChannelsButtons(requiredChannels)
			forwardBtn := utils.MakeLinkForForwardingMessage(msgID)

			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(joinButtons...),
				tgbotapi.NewInlineKeyboardRow(forwardBtn),
			)

			notMemberMsg.ReplyMarkup = keyboard
			bot.Send(notMemberMsg)
		}
		return
	}
	msgText := "ورودی نامعتبره! برای دریافت فایل‌ها روی دکمه زیر بزنید:\n\n📥 دریافت فایل"
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 دریافت فایل"),
		),
	)
	reply := tgbotapi.NewMessage(chatID, msgText)
	reply.ReplyMarkup = keyboard
	bot.Send(reply)
}

func forwardContent(bot *tgbotapi.BotAPI, chatID int64) {
	forward := tgbotapi.NewForward(chatID, forwardFromChatID, forwardMessageID)
	_, err := bot.Send(forward)
	if err != nil {
		log.Printf("❌ Error forwarding message from %d to %d: %v", forwardFromChatID, chatID, err)
	} else {
		log.Printf("✅ Message forwarded from %d to %d", forwardFromChatID, chatID)
	}
}