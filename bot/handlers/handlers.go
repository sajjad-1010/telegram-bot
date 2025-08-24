package handlers

import (
	"log"
	"strconv"
	"strings"
	"fmt"

	"telegram-bot/bot/middleware"
	"telegram-bot/config"
	"telegram-bot/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	requiredChannels  []string
	forwardFromChatID int64
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

	// log.Println("Loaded ENV:", requiredChannels, forwardFromChatID, forwardMessageID)
}

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {

	if update.Message != nil {
		log.Println("Chat ID:", update.Message.Chat.ID)
		log.Println("Message ID:", update.Message.MessageID)
		log.Println("Text:", update.Message.Text)
		
		
		if update.Message.Chat.ID == forwardFromChatID {
			log.Println("============ A NEW MESSAGE FROM GROUP CHAT ============")
			log.Println("Chat ID:", update.Message.Chat.ID)
			log.Println("Message ID:", update.Message.MessageID)

			messageID := update.Message.MessageID
			makeLinkForNewMessage(bot , messageID)

		}

		if update.Message.Command() == "start" {
			log.Println("============ A NEW MESSAGE FROM BOT ============")
			log.Println("Chat ID:", update.Message.Chat.ID)
			log.Println("Message ID:", update.Message.MessageID)
			log.Println("Text:", update.Message.Text)

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

	DecodemsgID , err := utils.Decode(args)
	log.Println(err,"###################")
	if err != nil {
		if middleware.IsUserMember(bot, chatID) {
			forward := tgbotapi.NewForward(chatID, forwardFromChatID, DecodemsgID)
			if _, err := bot.Send(forward); err != nil {
				log.Println("❌ Error forwarding message:", err)
			}
		} else {
			notMemberMsg := tgbotapi.NewMessage(chatID, "برای دریافت فایل باید ابتدا عضو کانال‌ها شوید.")

			joinButtons := utils.GetJoinChannelsButtons(requiredChannels)
			forwardBtn := utils.MakeLinkForForwardingMessage(DecodemsgID)

			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(joinButtons...),
				tgbotapi.NewInlineKeyboardRow(forwardBtn),
			)

			notMemberMsg.ReplyMarkup = keyboard
			bot.Send(notMemberMsg)
		}
	}

	text := "برای دریافت فایل باید لینک درست و مخصوص فایل رو کلیک کنی "
	reply := tgbotapi.NewMessage(chatID, text)
	bot.Send(reply)
}

func makeLinkForNewMessage(bot *tgbotapi.BotAPI, messageID int) {

	if messageID < 0 {
		fmt.Errorf("your post in groupChat have negative messageID it's not supported")
	}
	Encodedmsg := utils.Encode(messageID)
	
	link := fmt.Sprintf("https://t.me/realblyat_bot?start=%s", Encodedmsg)
	
	msg := tgbotapi.NewMessage(forwardFromChatID, link)
	_, err := bot.Send(msg)
	if err != nil {
		fmt.Errorf("failed to send message: %s", link)
	}

	log.Println("From message succesfully create an Encoded link"+link+" and sended into the groupChat")
}
