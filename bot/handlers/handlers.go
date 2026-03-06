package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/bot/middleware"
	"telegram-bot/config"
	"telegram-bot/utils"
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
		log.Println("FORWARD_FROM_CHAT_ID is not set")
	}
}

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	log.Printf("update chat_id=%d message_id=%d", update.Message.Chat.ID, update.Message.MessageID)

	if update.Message.Chat.ID == forwardFromChatID {
		if err := makeLinkForNewMessage(bot, update.Message.MessageID); err != nil {
			log.Println("Error creating deep link:", err)
		}
	}

	if update.Message.Command() != "start" {
		return
	}

	args := update.Message.CommandArguments()
	if args != "" {
		handleStartWithArgs(bot, update.Message.Chat.ID, args)
		return
	}

	handleStart(bot, update.Message)
}

func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	text := "Open the private link of a file to receive it."
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	if _, err := bot.Send(reply); err != nil {
		log.Println("Error sending start message:", err)
	}
}

func handleStartWithArgs(bot *tgbotapi.BotAPI, chatID int64, args string) {
	log.Println("handleStartWithArgs called")

	decodedMsgID, err := utils.Decode(args)
	if err != nil {
		text := "Invalid link. Please open a valid file link."
		reply := tgbotapi.NewMessage(chatID, text)
		if _, sendErr := bot.Send(reply); sendErr != nil {
			log.Println("Error sending invalid-link message:", sendErr)
		}
		return
	}

	if middleware.IsUserMember(bot, chatID) {
		forward := tgbotapi.NewCopyMessage(chatID, forwardFromChatID, decodedMsgID)
		if _, err := bot.CopyMessage(forward); err != nil {
			log.Println("Error forwarding message:", err)
		}
		return
	}

	notMemberMsg := tgbotapi.NewMessage(chatID, "Join required channels first, then try again.")
	joinButtons := utils.GetJoinChannelsButtons(requiredChannels)
	forwardBtn := utils.MakeForwardButton(bot.Self.UserName, args)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(joinButtons...),
		tgbotapi.NewInlineKeyboardRow(forwardBtn),
	)

	notMemberMsg.ReplyMarkup = keyboard
	if _, err := bot.Send(notMemberMsg); err != nil {
		log.Println("Error sending membership prompt:", err)
	}
}

func makeLinkForNewMessage(bot *tgbotapi.BotAPI, messageID int) error {
	if messageID < 0 {
		return fmt.Errorf("messageID must be non-negative")
	}

	encodedMsg := utils.Encode(messageID)
	link := utils.MakeDeepLink(bot.Self.UserName, encodedMsg)

	msg := tgbotapi.NewMessage(forwardFromChatID, link)
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send generated link: %w", err)
	}

	log.Println("Deep link created and sent to source chat")
	return nil
}
