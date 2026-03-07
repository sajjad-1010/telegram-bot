package handlers

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/bot/middleware"
	"telegram-bot/config"
	"telegram-bot/providers/instagram"
	"telegram-bot/utils"
)

var (
	requiredChannels  []string
	forwardFromChatID int64
	pendingMediaMu    sync.RWMutex
	pendingMediaLinks = map[int64]string{}
)

const checkMembershipCallback = "check_membership"

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
	if update.CallbackQuery != nil {
		handleCallbackQuery(bot, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	log.Printf("update chat_id=%d message_id=%d", update.Message.Chat.ID, update.Message.MessageID)

	if update.Message.Chat.ID == forwardFromChatID {
		if err := makeLinkForNewMessage(bot, update.Message.MessageID); err != nil {
			log.Println("Error creating deep link:", err)
		}
	}

	if update.Message.Command() == "start" {
		args := update.Message.CommandArguments()
		if args != "" {
			handleStartWithArgs(bot, update.Message.Chat.ID, args)
			return
		}

		handleStart(bot, update.Message)
		return
	}

	if update.Message.Text == "" {
		return
	}

	link, platform, ok := extractSupportedMediaLink(update.Message.Text)
	if !ok {
		return
	}

	handleMediaDownload(bot, update.Message.Chat.ID, link, platform)
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

func extractSupportedMediaLink(text string) (string, string, bool) {
	for _, part := range strings.Fields(text) {
		candidate := strings.TrimSpace(part)
		candidate = strings.Trim(candidate, "<>\"'.,)")

		u, err := url.Parse(candidate)
		if err != nil || u == nil {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}

		host := strings.ToLower(u.Hostname())
		path := strings.ToLower(strings.Trim(u.Path, "/"))

		if host == "instagram.com" || host == "www.instagram.com" {
			if strings.HasPrefix(path, "reel/") || strings.HasPrefix(path, "p/") || strings.HasPrefix(path, "tv/") {
				return u.String(), "Instagram", true
			}
			continue
		}

		if host == "tiktok.com" || host == "www.tiktok.com" || host == "m.tiktok.com" || host == "vm.tiktok.com" {
			if path != "" {
				return u.String(), "TikTok", true
			}
			continue
		}

		if host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" {
			if strings.HasPrefix(path, "watch") || strings.HasPrefix(path, "shorts/") || strings.HasPrefix(path, "live/") || strings.HasPrefix(path, "embed/") {
				return u.String(), "YouTube", true
			}
			continue
		}

		if host == "youtu.be" && path != "" {
			return u.String(), "YouTube", true
		}

		if host == "reddit.com" || host == "www.reddit.com" || host == "old.reddit.com" || host == "v.redd.it" || host == "redd.it" {
			if path != "" {
				return u.String(), "Reddit", true
			}
			continue
		}
	}

	return "", "", false
}

func handleMediaDownload(bot *tgbotapi.BotAPI, chatID int64, link, platform string) {
	if !middleware.IsUserMember(bot, chatID) {
		storePendingMediaLink(chatID, link)

		notMemberMsg := tgbotapi.NewMessage(chatID, "Join required channels first, then tap Check membership.")
		joinButtons := utils.GetJoinChannelsButtons(requiredChannels)

		rows := [][]tgbotapi.InlineKeyboardButton{}
		if len(joinButtons) > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(joinButtons...))
		}
		checkBtn := tgbotapi.NewInlineKeyboardButtonData("Check membership", checkMembershipCallback)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(checkBtn))
		notMemberMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

		if _, err := bot.Send(notMemberMsg); err != nil {
			log.Println("Error sending membership prompt:", err)
		}
		return
	}

	waitMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Downloading from %s, please wait...", platform))
	sentWaitMsg, err := bot.Send(waitMsg)
	if err != nil {
		log.Println("Error sending progress message:", err)
	} else {
		defer deleteMessage(bot, chatID, sentWaitMsg.MessageID)
	}

	filePath, cleanup, err := instagram.Download(link)
	if err != nil {
		errMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Download failed. The %s content may be private, unavailable, or yt-dlp is missing on server.", platform))
		if _, sendErr := bot.Send(errMsg); sendErr != nil {
			log.Println("Error sending download-failed message:", sendErr)
		}
		log.Printf("%s download error: %v", platform, err)
		return
	}
	defer cleanup()

	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(filePath))
	video.Caption = "@" + bot.Self.UserName
	if _, err := bot.Send(video); err != nil {
		log.Println("Error sending downloaded video:", err)
		fallback := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
		fallback.Caption = "@" + bot.Self.UserName
		if _, fallbackErr := bot.Send(fallback); fallbackErr != nil {
			log.Println("Error sending downloaded file as document:", fallbackErr)
		}
	}
}

func deleteMessage(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	deleteCfg := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := bot.Request(deleteCfg); err != nil {
		log.Println("Error deleting message:", err)
	}
}

func handleCallbackQuery(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	if cb == nil {
		return
	}

	if cb.Data != checkMembershipCallback {
		return
	}

	if _, err := bot.Request(tgbotapi.NewCallback(cb.ID, "Checking membership...")); err != nil {
		log.Println("Error answering callback query:", err)
	}

	chatID := cb.From.ID
	link, ok := getPendingMediaLink(chatID)
	if !ok {
		msg := tgbotapi.NewMessage(chatID, "No pending link found. Please send the link again.")
		if _, err := bot.Send(msg); err != nil {
			log.Println("Error sending no-pending-link message:", err)
		}
		return
	}

	if !middleware.IsUserMember(bot, chatID) {
		msg := tgbotapi.NewMessage(chatID, "You are still not a member of all required channels.")
		if _, err := bot.Send(msg); err != nil {
			log.Println("Error sending not-member-yet message:", err)
		}
		return
	}

	deletePendingMediaLink(chatID)
	platform := "media"
	if _, detectedPlatform, detected := extractSupportedMediaLink(link); detected {
		platform = detectedPlatform
	}
	handleMediaDownload(bot, chatID, link, platform)
}

func storePendingMediaLink(chatID int64, link string) {
	pendingMediaMu.Lock()
	defer pendingMediaMu.Unlock()
	pendingMediaLinks[chatID] = link
}

func getPendingMediaLink(chatID int64) (string, bool) {
	pendingMediaMu.RLock()
	defer pendingMediaMu.RUnlock()
	link, ok := pendingMediaLinks[chatID]
	return link, ok
}

func deletePendingMediaLink(chatID int64) {
	pendingMediaMu.Lock()
	defer pendingMediaMu.Unlock()
	delete(pendingMediaLinks, chatID)
}
