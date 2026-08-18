package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/bot/middleware"
	"telegram-bot/bot/state"
	"telegram-bot/config"
	"telegram-bot/db"
	"telegram-bot/internal/i18n"
	"telegram-bot/providers/instagram"
	"telegram-bot/utils"
)

var (
	forwardFromChatID      int64
	pendingStore           *state.PendingStore
	sourceMediaGroupMu     sync.Mutex
	sourceMediaGroupTimers = map[string]*time.Timer{}
)

type contentSendSummary struct {
	AllPhotos bool
}

const (
	pendingKindForward     = "forward_copy"
	pendingKindMediaChoice = "media_choice"
	pendingKindMediaAuto   = "media_auto"

	callbackMediaOptionPrefix = "media_opt:"
	callbackLangSetPrefix     = "lang_set:"
	callbackResendPrefix      = "resend:"
	optionKeyMP4              = "mp4"
	optionKeyMP3              = "mp3"
	optionKeyBoth             = "both"

	optionModeVideoMP4 = "video_mp4"
	optionModeAudioMP3 = "audio_mp3"
	optionModeBoth     = "video_and_audio"

	sourceMediaGroupDebounce = 1500 * time.Millisecond
)

// Init wires up env-derived state. Must be called after config.LoadEnv(),
// so it is an explicit call rather than a package init().
func Init() {
	pendingStore = state.NewPendingStoreFromEnv()

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
		trackAudienceFromCallback(update.CallbackQuery)
		log.Printf("callback chat_id=%d user_id=%d data=%s", callbackChatID(update.CallbackQuery), update.CallbackQuery.From.ID, strings.TrimSpace(update.CallbackQuery.Data))
		handleCallbackQuery(bot, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	trackAudienceFromMessage(update.Message)
	log.Printf(
		"update chat_id=%d message_id=%d user_id=%d username=%s chat_type=%s",
		update.Message.Chat.ID,
		update.Message.MessageID,
		update.Message.From.ID,
		strings.TrimSpace(update.Message.From.UserName),
		update.Message.Chat.Type,
	)

	if update.Message.Chat.ID == forwardFromChatID {
		if update.Message.MediaGroupID != "" {
			if err := registerSourceMediaGroupMessage(bot, update.Message.MediaGroupID, update.Message.MessageID); err != nil {
				log.Println("Error registering source media group message:", err)
			}
		} else {
			if err := makeLinkForNewMessage(bot, update.Message.MessageID); err != nil {
				log.Println("Error creating deep link:", err)
			}
		}
	}

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			args := update.Message.CommandArguments()
			if args != "" {
				handleStartWithArgs(bot, update.Message.Chat.ID, update.Message.From.ID, args)
				return
			}
			handleStart(bot, update.Message)
			return
		case "req_list", "req_add", "req_remove":
			handleAdminRequiredChannelsCommand(bot, update.Message)
			return
		case "help":
			handleHelp(bot, update.Message)
			return
		case "stats":
			handleStats(bot, update.Message)
			return
		case "lang":
			handleLang(bot, update.Message)
			return
		default:
			return
		}
	}

	if update.Message.Text == "" {
		return
	}

	link, platform, ok := extractSupportedMediaLink(update.Message.Text)
	if !ok {
		return
	}
	log.Printf("media request detected platform=%s chat_id=%d link=%s", platform, update.Message.Chat.ID, link)

	if !allowMediaRequest(update.Message.From) {
		log.Printf("rate limit rejected user_id=%d chat_id=%d platform=%s", update.Message.From.ID, update.Message.Chat.ID, platform)
		sendText(bot, update.Message.Chat.ID, i18n.T(userLang(update.Message.From.ID), i18n.KeyRateLimited))
		return
	}

	handleMediaRequest(bot, update.Message.Chat.ID, update.Message.From.ID, link, platform)
}

func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	welcome := i18n.T(userLang(msg.From.ID), i18n.KeyWelcome)

	log.Printf("handleStart chat_id=%d user_id=%d message_id=%d", msg.Chat.ID, msg.From.ID, msg.MessageID)

	gifPath := config.GetEnv("WELCOME_GIF_PATH", "logo/stash-electric.gif")
	if _, statErr := os.Stat(gifPath); statErr == nil {
		anim := tgbotapi.NewAnimation(msg.Chat.ID, tgbotapi.FilePath(gifPath))
		anim.Caption = welcome
		anim.ParseMode = tgbotapi.ModeMarkdown
		if _, err := bot.Send(anim); err != nil {
			log.Println("Error sending start animation, falling back to text:", err)
		} else {
			log.Printf("welcome sent as animation chat_id=%d", msg.Chat.ID)
			return
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	reply.ParseMode = tgbotapi.ModeMarkdown
	if _, err := bot.Send(reply); err != nil {
		log.Println("Error sending start message:", err)
		return
	}
	log.Printf("welcome sent as text chat_id=%d", msg.Chat.ID)
}

func handleStartWithArgs(bot *tgbotapi.BotAPI, chatID int64, userID int64, args string) {
	log.Println("handleStartWithArgs called")
	lang := userLang(userID)

	if _, err := utils.ParseDeepLinkPayload(args); err != nil {
		reply := tgbotapi.NewMessage(chatID, i18n.T(lang, i18n.KeyInvalidLink))
		if _, sendErr := bot.Send(reply); sendErr != nil {
			log.Println("Error sending invalid-link message:", sendErr)
		}
		return
	}

	req := state.PendingRequest{
		Kind:    pendingKindForward,
		Payload: args,
	}
	if !ensureMembershipOrQueue(bot, chatID, userID, req) {
		return
	}

	copied, err := executeForwardCopy(bot, chatID, args)
	if err != nil {
		log.Println("Error forwarding message:", err)
		sendText(bot, chatID, i18n.T(lang, i18n.KeyFetchFailed))
		return
	}
	sendSaveNoticeAndScheduleDelete(bot, chatID, args, copied)
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

func makeLinkForMediaGroup(bot *tgbotapi.BotAPI, mediaGroupID string, itemCount int) error {
	mediaGroupID = strings.TrimSpace(mediaGroupID)
	if mediaGroupID == "" {
		return fmt.Errorf("media_group_id is empty")
	}

	payload := utils.EncodeMediaGroup(mediaGroupID)
	link := utils.MakeDeepLink(bot.Self.UserName, payload)

	msg := tgbotapi.NewMessage(forwardFromChatID, link)
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send media group deep link: %w", err)
	}

	log.Printf("Media group deep link created and sent media_group_id=%s items=%d", mediaGroupID, itemCount)
	return nil
}

func registerSourceMediaGroupMessage(bot *tgbotapi.BotAPI, mediaGroupID string, messageID int) error {
	if err := db.AddSourceMediaGroupItem(mediaGroupID, messageID); err != nil {
		return err
	}

	log.Printf("source media group item stored media_group_id=%s message_id=%d", mediaGroupID, messageID)

	sourceMediaGroupMu.Lock()
	if timer, ok := sourceMediaGroupTimers[mediaGroupID]; ok {
		timer.Stop()
	}
	sourceMediaGroupTimers[mediaGroupID] = time.AfterFunc(sourceMediaGroupDebounce, func() {
		if err := flushSourceMediaGroupLink(bot, mediaGroupID); err != nil {
			log.Printf("source media group link flush failed media_group_id=%s: %v", mediaGroupID, err)
		}
	})
	sourceMediaGroupMu.Unlock()

	return nil
}

func flushSourceMediaGroupLink(bot *tgbotapi.BotAPI, mediaGroupID string) error {
	sourceMediaGroupMu.Lock()
	delete(sourceMediaGroupTimers, mediaGroupID)
	sourceMediaGroupMu.Unlock()

	sent, err := db.IsSourceMediaGroupLinkSent(mediaGroupID)
	if err != nil {
		return err
	}
	if sent {
		log.Printf("source media group link already sent media_group_id=%s", mediaGroupID)
		return nil
	}

	messageIDs, err := db.GetSourceMediaGroupMessageIDs(mediaGroupID)
	if err != nil {
		return err
	}
	if len(messageIDs) == 0 {
		return fmt.Errorf("no source media group items found")
	}

	if err := makeLinkForMediaGroup(bot, mediaGroupID, len(messageIDs)); err != nil {
		return err
	}
	if err := db.MarkSourceMediaGroupLinkSent(mediaGroupID); err != nil {
		return err
	}
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
			if strings.HasPrefix(path, "reel/") || strings.HasPrefix(path, "p/") || strings.HasPrefix(path, "tv/") || strings.HasPrefix(path, "stories/") {
				return normalizeInstagramLink(u), "Instagram", true
			}
			continue
		}

		if host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com") {
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

		if host == "twitter.com" || host == "www.twitter.com" || host == "mobile.twitter.com" || host == "x.com" || host == "www.x.com" {
			if path != "" {
				return u.String(), "Twitter", true
			}
			continue
		}

		if host == "pinterest.com" || strings.HasSuffix(host, ".pinterest.com") || host == "pin.it" {
			if path != "" {
				return u.String(), "Pinterest", true
			}
			continue
		}
	}

	return "", "", false
}

func normalizeInstagramLink(u *url.URL) string {
	if u == nil {
		return ""
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return u.String()
	}

	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	shortcode := strings.TrimSpace(parts[1])
	if shortcode == "" {
		return u.String()
	}

	switch kind {
	case "p", "reel", "tv":
		return fmt.Sprintf("https://www.instagram.com/%s/%s/", kind, shortcode)
	case "stories":
		if len(parts) < 3 {
			return u.String()
		}
		username := strings.TrimSpace(parts[1])
		storyID := strings.TrimSpace(parts[2])
		if username == "" || storyID == "" {
			return u.String()
		}
		return fmt.Sprintf("https://www.instagram.com/stories/%s/%s/", username, storyID)
	default:
		return u.String()
	}
}

func handleMediaRequest(bot *tgbotapi.BotAPI, chatID int64, userID int64, link, platform string) {
	req := state.PendingRequest{
		Kind:     pendingKindMediaChoice,
		Payload:  link,
		Platform: platform,
	}

	if strings.EqualFold(platform, "Instagram") || strings.EqualFold(platform, "Reddit") {
		req.Kind = pendingKindMediaAuto
		if !ensureMembershipOrQueue(bot, chatID, userID, req) {
			return
		}
		executePendingRequest(bot, chatID, userID, req)
		return
	}

	probe, err := instagram.Probe(link)
	if err != nil {
		log.Printf("Media probe failed for %s: %v", platform, err)
		if strings.EqualFold(platform, "YouTube") && req.Kind == pendingKindMediaChoice {
			req.Options = buildDefaultMediaOptions()
		}
	} else {
		req.HasAudio = probe.HasAudio
	}

	if !strings.EqualFold(platform, "YouTube") {
		req.Kind = pendingKindMediaAuto
	} else if err != nil {
		// Keep YouTube on explicit format choice even when probing fails.
		if len(req.Options) == 0 {
			req.Options = buildDefaultMediaOptions()
		}
	} else if probe.ItemCount > 1 {
		log.Printf("%s multi-item post detected (%d items), using collection delivery flow", platform, probe.ItemCount)
		req.Kind = pendingKindMediaAuto
	} else if !probe.HasVideo {
		req.Kind = pendingKindMediaAuto
	} else {
		req.Options = buildMediaOptionsForPlatform(platform, link)
	}

	if req.Kind == pendingKindMediaChoice && len(req.Options) == 0 {
		req.Options = buildDefaultMediaOptions()
	}

	if !ensureMembershipOrQueue(bot, chatID, userID, req) {
		return
	}

	executePendingRequest(bot, chatID, userID, req)
}

func ensureMembershipOrQueue(bot *tgbotapi.BotAPI, chatID int64, userID int64, req state.PendingRequest) bool {
	if middleware.IsUserMember(bot, userID) {
		log.Printf("membership ok chat_id=%d user_id=%d kind=%s platform=%s", chatID, userID, req.Kind, req.Platform)
		return true
	}

	storePendingDownload(chatID, req)
	log.Printf("membership required chat_id=%d user_id=%d kind=%s platform=%s", chatID, userID, req.Kind, req.Platform)
	if err := middleware.SendMembershipRequiredPrompt(bot, chatID, userID, i18n.T(userLang(userID), i18n.KeyMembershipRequired)); err != nil {
		log.Println("Error sending membership prompt:", err)
	}
	return false
}

func executePendingRequest(bot *tgbotapi.BotAPI, chatID int64, userID int64, req state.PendingRequest) {
	log.Printf("execute pending request chat_id=%d user_id=%d kind=%s platform=%s", chatID, userID, req.Kind, req.Platform)
	switch req.Kind {
	case pendingKindForward:
		copied, err := executeForwardCopy(bot, chatID, req.Payload)
		if err != nil {
			log.Println("Error forwarding message:", err)
			sendText(bot, chatID, i18n.T(userLang(userID), i18n.KeyFetchFailed))
			return
		}
		sendSaveNoticeAndScheduleDelete(bot, chatID, req.Payload, copied)
	case pendingKindMediaChoice:
		sendMediaOptionsPrompt(bot, chatID, userID, req)
	case pendingKindMediaAuto:
		executeAutoMediaDownload(bot, chatID, userID, req.Payload, req.Platform, req.HasAudio)
	default:
		sendText(bot, chatID, i18n.T(userLang(userID), i18n.KeyUnknownPending))
	}
}

func sendMediaOptionsPrompt(bot *tgbotapi.BotAPI, chatID int64, userID int64, req state.PendingRequest) {
	if len(req.Options) == 0 {
		req.Options = buildDefaultMediaOptions()
	}
	storePendingDownload(chatID, req)
	log.Printf("media options prompt chat_id=%d platform=%s options=%d", chatID, req.Platform, len(req.Options))

	msg := tgbotapi.NewMessage(chatID, i18n.Tf(userLang(userID), i18n.KeyChooseFormat, req.Platform))
	msg.ReplyMarkup = buildMediaOptionsKeyboard(req.Options)
	if _, err := bot.Send(msg); err != nil {
		log.Println("Error sending media options prompt:", err)
	}
}

func executeAutoMediaDownload(bot *tgbotapi.BotAPI, chatID int64, userID int64, link, platform string, hasAudio bool) {
	lang := userLang(userID)
	log.Printf("auto media download start chat_id=%d platform=%s has_audio=%t link=%s", chatID, platform, hasAudio, link)
	waitMsg := tgbotapi.NewMessage(chatID, i18n.Tf(lang, i18n.KeyDownloading, platform))
	sentWaitMsg, err := bot.Send(waitMsg)
	if err != nil {
		log.Println("Error sending progress message:", err)
	} else {
		defer deleteMessage(bot, chatID, sentWaitMsg.MessageID)
	}

	contentPaths, cleanup, err := instagram.DownloadBestContents(link)
	if err != nil {
		log.Printf("%s content download error for %s: %v", platform, link, err)
		sendText(bot, chatID, userFacingDownloadError(lang, err, platform, "content"))
		logDownloadEvent(userID, platform, link, "auto", db.DownloadStatusFailed)
		return
	}
	defer cleanup()

	caption := formatCaption(bot)
	summary, sendErr := sendDownloadedContents(bot, chatID, contentPaths, caption)
	if sendErr != nil {
		log.Println("Error sending content:", sendErr)
		sendText(bot, chatID, userFacingDownloadError(lang, sendErr, platform, "content"))
		logDownloadEvent(userID, platform, link, "auto", db.DownloadStatusFailed)
		return
	}

	if !strings.EqualFold(platform, "Instagram") && summary.AllPhotos && hasAudio {
		if err := downloadAndSendMP3(bot, chatID, link, platform, caption); err != nil {
			log.Println("Optional MP3 send failed for photo content:", err)
		}
	}
	if strings.EqualFold(platform, "Instagram") && summary.AllPhotos {
		if err := downloadAndSendInstagramAttachedAudio(bot, chatID, link, caption); err != nil {
			log.Println("Optional Instagram attached audio send failed:", err)
		}
	}
	logDownloadEvent(userID, platform, link, "auto", db.DownloadStatusOK)
	log.Printf("auto media download complete chat_id=%d platform=%s files=%d all_photos=%t", chatID, platform, len(contentPaths), summary.AllPhotos)
}

func executeMediaOption(bot *tgbotapi.BotAPI, chatID int64, userID int64, req state.PendingRequest, optionKey string) {
	lang := userLang(userID)
	log.Printf("media option selected chat_id=%d platform=%s option=%s", chatID, req.Platform, optionKey)
	waitMsg := tgbotapi.NewMessage(chatID, i18n.Tf(lang, i18n.KeyDownloading, req.Platform))
	sentWaitMsg, err := bot.Send(waitMsg)
	if err != nil {
		log.Println("Error sending progress message:", err)
	} else {
		defer deleteMessage(bot, chatID, sentWaitMsg.MessageID)
	}

	caption := formatCaption(bot)
	selected, ok := findMediaOption(req.Options, optionKey)
	if !ok {
		sendText(bot, chatID, i18n.T(lang, i18n.KeyUnknownOption))
		return
	}

	switch selected.Mode {
	case optionModeVideoMP4:
		if err := downloadAndSendMP4(bot, chatID, req.Payload, selected.Selector, caption); err != nil {
			log.Println("MP4 download/send error:", err)
			sendText(bot, chatID, userFacingDownloadError(lang, err, req.Platform, "mp4"))
			logDownloadEvent(userID, req.Platform, req.Payload, "mp4", db.DownloadStatusFailed)
		} else {
			logDownloadEvent(userID, req.Platform, req.Payload, "mp4", db.DownloadStatusOK)
		}
	case optionModeAudioMP3:
		if err := downloadAndSendMP3(bot, chatID, req.Payload, req.Platform, caption); err != nil {
			log.Println("MP3 download/send error:", err)
			sendText(bot, chatID, userFacingDownloadError(lang, err, req.Platform, "mp3"))
			logDownloadEvent(userID, req.Platform, req.Payload, "mp3", db.DownloadStatusFailed)
		} else {
			logDownloadEvent(userID, req.Platform, req.Payload, "mp3", db.DownloadStatusOK)
		}
	case optionModeBoth:
		mp4Err := downloadAndSendMP4(bot, chatID, req.Payload, selected.Selector, caption)
		if mp4Err != nil {
			log.Println("MP4 part failed in MP4+MP3:", mp4Err)
			sendText(bot, chatID, userFacingDownloadError(lang, mp4Err, req.Platform, "mp4"))
		}
		mp3Err := downloadAndSendMP3(bot, chatID, req.Payload, req.Platform, caption)
		if mp3Err != nil {
			log.Println("MP3 part failed in MP4+MP3:", mp3Err)
			sendText(bot, chatID, userFacingDownloadError(lang, mp3Err, req.Platform, "mp3"))
		}
		if mp4Err == nil && mp3Err == nil {
			logDownloadEvent(userID, req.Platform, req.Payload, "both", db.DownloadStatusOK)
		} else {
			logDownloadEvent(userID, req.Platform, req.Payload, "both", db.DownloadStatusFailed)
		}
	default:
		sendText(bot, chatID, i18n.T(lang, i18n.KeyUnknownOption))
	}
}

func logDownloadEvent(userID int64, platform, link, mode, status string) {
	if err := db.LogDownload(userID, platform, link, mode, status); err != nil {
		log.Printf("failed to log download event user_id=%d platform=%s status=%s: %v", userID, platform, status, err)
	}
}

func userFacingDownloadError(lang string, err error, platform, mode string) string {
	if err == nil {
		return i18n.T(lang, i18n.KeyErrGeneric)
	}

	raw := err.Error()
	maxMB := getConfiguredMaxDownloadSizeMB()
	m := strings.ToLower(strings.TrimSpace(mode))

	if strings.Contains(raw, "file too large:") {
		if strings.Contains(raw, "Telegram limit is") {
			limitMB := getTelegramUploadLimitBytes() / 1024 / 1024
			switch m {
			case "mp4":
				return i18n.Tf(lang, i18n.KeyErrVideoTelegramLimit, platform, limitMB)
			case "mp3":
				return i18n.Tf(lang, i18n.KeyErrAudioTelegramLimit, platform, limitMB)
			default:
				return i18n.Tf(lang, i18n.KeyErrFileTelegramLimit, limitMB)
			}
		}
		switch m {
		case "mp4":
			return i18n.Tf(lang, i18n.KeyErrVideoSizeLimit, platform, maxMB)
		case "mp3":
			return i18n.Tf(lang, i18n.KeyErrAudioSizeLimit, platform, maxMB)
		default:
			return i18n.Tf(lang, i18n.KeyErrFileSizeLimit, maxMB)
		}
	}

	if strings.Contains(strings.ToLower(raw), "command timed out") {
		switch m {
		case "mp4":
			return i18n.Tf(lang, i18n.KeyErrVideoTimeout, platform)
		case "mp3":
			return i18n.Tf(lang, i18n.KeyErrAudioTimeout, platform)
		default:
			return i18n.T(lang, i18n.KeyErrTimeout)
		}
	}

	if strings.Contains(raw, "mp3 download requires ffmpeg/ffprobe") {
		return i18n.T(lang, i18n.KeyErrNoFfmpeg)
	}

	switch m {
	case "mp4":
		return i18n.Tf(lang, i18n.KeyErrVideoSendFailed, platform)
	case "mp3":
		return i18n.Tf(lang, i18n.KeyErrAudioSendFailed, platform)
	default:
		return i18n.T(lang, i18n.KeyErrGeneric)
	}
}

func getConfiguredMaxDownloadSizeMB() int {
	raw := strings.TrimSpace(config.GetEnv("INSTAGRAM_MAX_FILE_SIZE_MB", "2048"))
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 2048
	}
	return v
}

func downloadAndSendMP4(bot *tgbotapi.BotAPI, chatID int64, link, selector, caption string) error {
	cacheKey := cacheKeyForLink(link)
	mode := mp4CacheMode(selector)
	if _, ok := trySendCachedMedia(bot, chatID, cacheKey, mode, caption); ok {
		return nil
	}

	filePath, cleanup, err := instagram.DownloadVideoBySelector(link, selector)
	if err != nil {
		return err
	}
	defer cleanup()

	kind, sent, err := sendDownloadedContent(bot, chatID, filePath, caption)
	if err != nil {
		return err
	}
	cacheSentMedia(cacheKey, mode, kind, sent)
	return nil
}

// mp4CacheMode derives a cache mode that keeps different requested video
// qualities in separate cache slots (mp4:1080, mp4:720, ...). Unknown/empty
// selectors fall back to the generic mp4 slot.
func mp4CacheMode(selector string) string {
	for _, h := range []string{"1080", "720", "480", "360", "240"} {
		if strings.Contains(selector, "height<="+h) {
			return db.MediaCacheModeMP4 + ":" + h
		}
	}
	return db.MediaCacheModeMP4
}

// trySendCachedMedia sends a previously cached file_id if present. Returns
// (sentMessage, true) on a successful cache hit. On an invalid-file_id error it
// drops the stale row and returns ok=false so the caller re-downloads.
func trySendCachedMedia(bot *tgbotapi.BotAPI, chatID int64, cacheKey, mode, caption string) (tgbotapi.Message, bool) {
	if !mediaCacheEnabled() {
		return tgbotapi.Message{}, false
	}

	fileID, fileType, ok, err := db.GetCachedMedia(cacheKey, mode)
	if err != nil {
		log.Printf("media cache lookup failed key=%s mode=%s: %v", cacheKey, mode, err)
		return tgbotapi.Message{}, false
	}
	if !ok {
		return tgbotapi.Message{}, false
	}

	var chattable tgbotapi.Chattable
	switch fileType {
	case "audio":
		audio := tgbotapi.NewAudio(chatID, tgbotapi.FileID(fileID))
		audio.Caption = caption
		chattable = audio
	default:
		video := tgbotapi.NewVideo(chatID, tgbotapi.FileID(fileID))
		video.Caption = caption
		video.SupportsStreaming = true
		chattable = video
	}

	sent, sendErr := bot.Send(chattable)
	if sendErr == nil {
		log.Printf("media cache hit key=%s mode=%s type=%s", cacheKey, mode, fileType)
		return sent, true
	}

	if isInvalidFileIDErr(sendErr) {
		log.Printf("media cache stale key=%s mode=%s, dropping: %v", cacheKey, mode, sendErr)
		if delErr := db.DeleteMediaCache(cacheKey, mode); delErr != nil {
			log.Printf("media cache delete failed key=%s mode=%s: %v", cacheKey, mode, delErr)
		}
		return tgbotapi.Message{}, false
	}

	log.Printf("media cache send failed (non-fileid) key=%s mode=%s: %v", cacheKey, mode, sendErr)
	return tgbotapi.Message{}, false
}

// cacheSentMedia stores the file_id of a freshly sent video/audio for reuse.
func cacheSentMedia(cacheKey, mode, kind string, sent tgbotapi.Message) {
	if !mediaCacheEnabled() {
		return
	}
	fileID := extractFileID(kind, sent)
	if fileID == "" {
		return
	}
	if err := db.UpsertMediaCache(cacheKey, mode, fileID, kind); err != nil {
		log.Printf("media cache store failed key=%s mode=%s: %v", cacheKey, mode, err)
		return
	}
	log.Printf("media cache store key=%s mode=%s type=%s", cacheKey, mode, kind)
}

func sendDownloadedContents(bot *tgbotapi.BotAPI, chatID int64, filePaths []string, caption string) (contentSendSummary, error) {
	if len(filePaths) == 0 {
		return contentSendSummary{}, fmt.Errorf("no downloaded content files found")
	}

	if len(filePaths) == 1 {
		kind, _, err := sendDownloadedContent(bot, chatID, filePaths[0], caption)
		return contentSendSummary{AllPhotos: kind == "photo"}, err
	}

	summary := contentSendSummary{AllPhotos: true}
	canUseMediaGroup := len(filePaths) <= 10
	for _, filePath := range filePaths {
		ext := strings.ToLower(filepath.Ext(filePath))
		if !isImageExt(ext) {
			summary.AllPhotos = false
		}
		if !isImageExt(ext) && !isVideoExt(ext) {
			canUseMediaGroup = false
		}
	}

	if canUseMediaGroup {
		if err := sendDownloadedMediaGroup(bot, chatID, filePaths, caption); err == nil {
			return summary, nil
		} else {
			log.Printf("sendMediaGroup failed for %d files, falling back to sequential send: %v", len(filePaths), err)
		}
	}

	for idx, filePath := range filePaths {
		itemCaption := ""
		if idx == 0 {
			itemCaption = caption
		}
		if _, _, err := sendDownloadedContent(bot, chatID, filePath, itemCaption); err != nil {
			return summary, fmt.Errorf("send item %d (%s): %w", idx+1, filepath.Base(filePath), err)
		}
	}

	return summary, nil
}

func sendDownloadedMediaGroup(bot *tgbotapi.BotAPI, chatID int64, filePaths []string, caption string) error {
	media := make([]interface{}, 0, len(filePaths))
	for idx, filePath := range filePaths {
		ext := strings.ToLower(filepath.Ext(filePath))
		switch {
		case isImageExt(ext):
			item := tgbotapi.NewInputMediaPhoto(tgbotapi.FilePath(filePath))
			if idx == 0 {
				item.Caption = caption
			}
			media = append(media, item)
		case isVideoExt(ext):
			item := tgbotapi.NewInputMediaVideo(tgbotapi.FilePath(filePath))
			item.SupportsStreaming = true
			if idx == 0 {
				item.Caption = caption
			}
			media = append(media, item)
		default:
			return fmt.Errorf("unsupported media group file type: %s", filepath.Base(filePath))
		}
	}

	group := tgbotapi.NewMediaGroup(chatID, media)
	_, err := bot.SendMediaGroup(group)
	return err
}

func downloadAndSendMP3(bot *tgbotapi.BotAPI, chatID int64, link, platform, caption string) error {
	cacheKey := cacheKeyForLink(link)
	if _, ok := trySendCachedMedia(bot, chatID, cacheKey, db.MediaCacheModeMP3, caption); ok {
		return nil
	}

	log.Printf("mp3 download start chat_id=%d platform=%s link=%s", chatID, platform, link)
	filePath, cleanup, err := instagram.DownloadMP3(link)
	if err != nil {
		log.Printf("mp3 download failed chat_id=%d platform=%s link=%s err=%v", chatID, platform, link, err)
		return err
	}
	defer cleanup()

	audioTitle := ""
	if strings.EqualFold(platform, "YouTube") {
		if title, titleErr := instagram.GetMediaTitle(link); titleErr == nil {
			audioTitle = title
		} else {
			log.Printf("GetMediaTitle failed for YouTube link: %v", titleErr)
		}
	}

	log.Printf("mp3 download complete chat_id=%d platform=%s file=%s title=%q", chatID, platform, filepath.Base(filePath), audioTitle)
	sent, err := sendAudioFile(bot, chatID, filePath, caption, audioTitle)
	if err != nil {
		log.Printf("mp3 send failed chat_id=%d platform=%s file=%s err=%v", chatID, platform, filepath.Base(filePath), err)
		return err
	}
	cacheSentMedia(cacheKey, db.MediaCacheModeMP3, "audio", sent)
	log.Printf("mp3 send complete chat_id=%d platform=%s file=%s", chatID, platform, filepath.Base(filePath))
	return nil
}

func downloadAndSendInstagramAttachedAudio(bot *tgbotapi.BotAPI, chatID int64, link, caption string) error {
	filePath, title, cleanup, err := instagram.DownloadInstagramAttachedAudio(link)
	if err != nil {
		return err
	}
	defer cleanup()

	if _, err := sendAudioFile(bot, chatID, filePath, caption, title); err != nil {
		return err
	}

	log.Printf("instagram attached-audio sent title=%q", title)
	return nil
}

func getTelegramUploadLimitBytes() int64 {
	raw := strings.TrimSpace(config.GetEnv("TELEGRAM_UPLOAD_LIMIT_MB", "50"))
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 50 * 1024 * 1024
	}
	return v * 1024 * 1024
}

func checkTelegramFileSize(filePath string) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file stat failed: %w", err)
	}
	limitBytes := getTelegramUploadLimitBytes()
	limitMB := limitBytes / 1024 / 1024
	if stat.Size() > limitBytes {
		return fmt.Errorf("file too large: %d bytes (Telegram limit is %d MB)", stat.Size(), limitMB)
	}
	return nil
}

func sendDownloadedContent(bot *tgbotapi.BotAPI, chatID int64, filePath, caption string) (string, tgbotapi.Message, error) {
	if err := checkTelegramFileSize(filePath); err != nil {
		return "", tgbotapi.Message{}, err
	}
	ext := strings.ToLower(filepath.Ext(filePath))

	if isImageExt(ext) {
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(filePath))
		photo.Caption = caption
		sent, err := bot.Send(photo)
		if err == nil {
			log.Printf("content sent chat_id=%d kind=photo file=%s", chatID, filepath.Base(filePath))
		}
		return "photo", sent, err
	}

	if isAudioExt(ext) {
		sent, err := sendAudioFile(bot, chatID, filePath, caption, "")
		if err != nil {
			return "audio", tgbotapi.Message{}, err
		}
		log.Printf("content sent chat_id=%d kind=audio file=%s", chatID, filepath.Base(filePath))
		return "audio", sent, nil
	}

	if isVideoExt(ext) {
		sent, err := sendVideoWithFallback(bot, chatID, filePath, caption)
		if err != nil {
			return "video", tgbotapi.Message{}, err
		}
		log.Printf("content sent chat_id=%d kind=video file=%s", chatID, filepath.Base(filePath))
		return "video", sent, nil
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = caption
	sent, err := bot.Send(doc)
	if err == nil {
		log.Printf("content sent chat_id=%d kind=document file=%s", chatID, filepath.Base(filePath))
	}
	return "document", sent, err
}

func sendVideoWithFallback(bot *tgbotapi.BotAPI, chatID int64, filePath, caption string) (tgbotapi.Message, error) {
	if sent, err := sendVideo(bot, chatID, filePath, caption); err == nil {
		return sent, nil
	}

	normalizedPath, normCleanup, normErr := instagram.NormalizeForTelegram(filePath)
	if normErr == nil {
		defer normCleanup()
		if sent, retryErr := sendVideo(bot, chatID, normalizedPath, caption); retryErr == nil {
			return sent, nil
		}
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = caption
	return bot.Send(doc)
}

func sendVideo(bot *tgbotapi.BotAPI, chatID int64, filePath, caption string) (tgbotapi.Message, error) {
	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(filePath))
	video.Caption = caption
	video.SupportsStreaming = true
	return bot.Send(video)
}

func sendAudioFile(bot *tgbotapi.BotAPI, chatID int64, filePath, caption, title string) (tgbotapi.Message, error) {
	if err := checkTelegramFileSize(filePath); err != nil {
		return tgbotapi.Message{}, err
	}
	audio := tgbotapi.NewAudio(chatID, tgbotapi.FilePath(filePath))
	audio.Caption = caption
	if strings.TrimSpace(title) != "" {
		audio.Title = title
	}
	return bot.Send(audio)
}

// extractFileID pulls the reusable Telegram file_id from a sent message for the
// given content kind. Returns "" if unavailable.
func extractFileID(kind string, msg tgbotapi.Message) string {
	switch kind {
	case "video":
		if msg.Video != nil {
			return msg.Video.FileID
		}
		if msg.Document != nil {
			return msg.Document.FileID
		}
	case "audio":
		if msg.Audio != nil {
			return msg.Audio.FileID
		}
	case "photo":
		if len(msg.Photo) > 0 {
			return msg.Photo[len(msg.Photo)-1].FileID
		}
	case "document":
		if msg.Document != nil {
			return msg.Document.FileID
		}
	}
	return ""
}

// isInvalidFileIDErr reports whether a Telegram send error indicates the cached
// file_id is no longer valid, so the cache row should be dropped.
func isInvalidFileIDErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "wrong file identifier") ||
		strings.Contains(msg, "wrong remote file identifier") ||
		strings.Contains(msg, "wrong file_id") {
		return true
	}
	return strings.Contains(msg, "file_id") && strings.Contains(msg, "invalid")
}

// mediaCacheEnabled reports whether file_id caching is turned on.
func mediaCacheEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(config.GetEnv("MEDIA_CACHE_ENABLED", "true")), "true")
}

// cacheKeyForLink normalizes a link for use as a media_cache key by stripping
// query and fragment junk (e.g. ?utm=...). Conservative: path is untouched.
func cacheKeyForLink(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u == nil {
		return strings.TrimSpace(link)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isImageExt(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".avi":
		return true
	default:
		return false
	}
}

func isAudioExt(ext string) bool {
	switch ext {
	case ".mp3", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".flac":
		return true
	default:
		return false
	}
}

func formatCaption(bot *tgbotapi.BotAPI) string {
	return fmt.Sprintf("\U0001FA76 @%s \U0001F49C", bot.Self.UserName)
}

// executeForwardCopy copies the source-group post into the user's chat and
// returns the IDs of the copies, so the caller can delete them later.
func executeForwardCopy(bot *tgbotapi.BotAPI, chatID int64, encodedArg string) ([]int, error) {
	payload, err := utils.ParseDeepLinkPayload(encodedArg)
	if err != nil {
		return nil, err
	}

	if payload.MediaGroupID != "" {
		return copySourceMediaGroup(bot, chatID, payload.MediaGroupID)
	}
	if payload.MessageID <= 0 {
		return nil, fmt.Errorf("invalid message payload")
	}

	forward := tgbotapi.NewCopyMessage(chatID, forwardFromChatID, payload.MessageID)
	sent, err := bot.CopyMessage(forward)
	if err != nil {
		return nil, err
	}
	return []int{sent.MessageID}, nil
}

func copySourceMediaGroup(bot *tgbotapi.BotAPI, chatID int64, mediaGroupID string) ([]int, error) {
	messageIDs, err := db.GetSourceMediaGroupMessageIDs(mediaGroupID)
	if err != nil {
		return nil, err
	}
	if len(messageIDs) == 0 {
		return nil, fmt.Errorf("source media group is empty")
	}

	log.Printf("copy source media group chat_id=%d media_group_id=%s items=%d", chatID, mediaGroupID, len(messageIDs))

	if len(messageIDs) == 1 {
		forward := tgbotapi.NewCopyMessage(chatID, forwardFromChatID, messageIDs[0])
		sent, err := bot.CopyMessage(forward)
		if err != nil {
			return nil, err
		}
		return []int{sent.MessageID}, nil
	}

	if copied, err := requestCopyMessages(bot, chatID, forwardFromChatID, messageIDs); err == nil {
		return copied, nil
	} else {
		log.Printf("copyMessages failed for media_group_id=%s, falling back to sequential copy: %v", mediaGroupID, err)
	}

	var copied []int
	for _, messageID := range messageIDs {
		forward := tgbotapi.NewCopyMessage(chatID, forwardFromChatID, messageID)
		sent, err := bot.CopyMessage(forward)
		if err != nil {
			return copied, fmt.Errorf("sequential copy failed for message_id=%d: %w", messageID, err)
		}
		copied = append(copied, sent.MessageID)
	}
	return copied, nil
}

func requestCopyMessages(bot *tgbotapi.BotAPI, chatID, fromChatID int64, messageIDs []int) ([]int, error) {
	if len(messageIDs) == 0 {
		return nil, fmt.Errorf("message_ids is empty")
	}

	params := make(tgbotapi.Params)
	params.AddNonZero64("chat_id", chatID)
	params.AddNonZero64("from_chat_id", fromChatID)
	if err := params.AddInterface("message_ids", messageIDs); err != nil {
		return nil, err
	}

	resp, err := bot.MakeRequest("copyMessages", params)
	if err != nil {
		return nil, err
	}

	var sent []tgbotapi.MessageID
	if err := json.Unmarshal(resp.Result, &sent); err != nil {
		return nil, fmt.Errorf("decode copyMessages result: %w", err)
	}

	copied := make([]int, 0, len(sent))
	for _, m := range sent {
		copied = append(copied, m.MessageID)
	}
	return copied, nil
}

// forwardIntEnv reads a positive integer setting, falling back on any problem.
func forwardIntEnv(key string, fallback int) int {
	if raw := strings.TrimSpace(config.GetEnv(key, "")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// sendSaveNoticeAndScheduleDelete posts the bilingual "save it before it goes"
// notice carrying a resend button, then removes the copied post after the
// configured delay. The notice itself is kept so the button stays clickable.
func sendSaveNoticeAndScheduleDelete(bot *tgbotapi.BotAPI, chatID int64, encodedArg string, copied []int) {
	noticeSeconds := forwardIntEnv("FORWARD_NOTICE_SECONDS", 30)
	notice := fmt.Sprintf(
		"⏳ این پیام بعد از %d ثانیه پاک می‌شود، لطفاً ذخیره‌اش کنید.\n"+
			"⏳ This message will be deleted after %d seconds, please save it.",
		noticeSeconds, noticeSeconds,
	)

	msg := tgbotapi.NewMessage(chatID, notice)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"🔁 ارسال دوباره / Send again",
				callbackResendPrefix+encodedArg,
			),
		),
	)
	if _, err := bot.Send(msg); err != nil {
		log.Println("Error sending save notice:", err)
	}

	if len(copied) == 0 {
		return
	}

	delay := time.Duration(forwardIntEnv("FORWARD_DELETE_SECONDS", 60)) * time.Second
	log.Printf("scheduled delete chat_id=%d messages=%d in=%s", chatID, len(copied), delay)
	go func() {
		time.Sleep(delay)
		for _, messageID := range copied {
			deleteMessage(bot, chatID, messageID)
		}
		log.Printf("auto-deleted forwarded post chat_id=%d messages=%d", chatID, len(copied))
	}()
}

// handleResendCallback re-runs the forward-copy flow for the same source post.
func handleResendCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	chatID := callbackChatID(cb)
	if chatID == 0 {
		return
	}

	if _, err := bot.Request(tgbotapi.NewCallback(cb.ID, "")); err != nil {
		log.Println("Error answering resend callback:", err)
	}

	lang := userLang(cb.From.ID)
	if !middleware.IsUserMember(bot, cb.From.ID) {
		if err := middleware.SendMembershipRequiredPrompt(bot, chatID, cb.From.ID, i18n.T(lang, i18n.KeyMembershipRequired)); err != nil {
			log.Println("Error sending membership prompt:", err)
		}
		return
	}

	// Drop the old notice so repeated resends do not pile up.
	if cb.Message != nil {
		deleteMessage(bot, chatID, cb.Message.MessageID)
	}

	encodedArg := strings.TrimPrefix(cb.Data, callbackResendPrefix)
	log.Printf("resend requested chat_id=%d user_id=%d", chatID, cb.From.ID)

	copied, err := executeForwardCopy(bot, chatID, encodedArg)
	if err != nil {
		log.Println("Error resending message:", err)
		sendText(bot, chatID, i18n.T(lang, i18n.KeyFetchFailed))
		return
	}
	sendSaveNoticeAndScheduleDelete(bot, chatID, encodedArg, copied)
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

	switch {
	case cb.Data == middleware.CheckMembershipCallbackData:
		handleMembershipCheckCallback(bot, cb)
	case strings.HasPrefix(cb.Data, callbackMediaOptionPrefix):
		handleMediaOptionCallback(bot, cb)
	case strings.HasPrefix(cb.Data, callbackLangSetPrefix):
		handleLangSetCallback(bot, cb)
	case strings.HasPrefix(cb.Data, callbackResendPrefix):
		handleResendCallback(bot, cb)
	}
}

func handleMembershipCheckCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	if _, err := bot.Request(tgbotapi.NewCallback(cb.ID, "Checking membership...")); err != nil {
		log.Println("Error answering callback query:", err)
	}

	chatID := cb.From.ID
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	}

	lang := userLang(cb.From.ID)
	req, ok := getPendingDownload(chatID)
	if !ok {
		sendText(bot, chatID, i18n.T(lang, i18n.KeyNoPendingRequest))
		return
	}

	if !middleware.IsUserMember(bot, cb.From.ID) {
		if err := middleware.SendMembershipRequiredPrompt(bot, chatID, cb.From.ID, i18n.T(lang, i18n.KeyStillNotMember)); err != nil {
			log.Println("Error sending membership prompt:", err)
		}
		return
	}

	if cb.Message != nil {
		deleteMessage(bot, chatID, cb.Message.MessageID)
	}
	deletePendingDownload(chatID)
	log.Printf("membership callback success chat_id=%d user_id=%d", chatID, cb.From.ID)
	executePendingRequest(bot, chatID, cb.From.ID, req)
}

func handleMediaOptionCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	if _, err := bot.Request(tgbotapi.NewCallback(cb.ID, "Processing your selection...")); err != nil {
		log.Println("Error answering media option callback:", err)
	}

	chatID := cb.From.ID
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	}

	lang := userLang(cb.From.ID)
	req, ok := getPendingDownload(chatID)
	if !ok || req.Kind != pendingKindMediaChoice {
		sendText(bot, chatID, i18n.T(lang, i18n.KeyNoPendingSelection))
		return
	}

	if !middleware.IsUserMember(bot, cb.From.ID) {
		if err := middleware.SendMembershipRequiredPrompt(bot, chatID, cb.From.ID, i18n.T(lang, i18n.KeyMembershipRequired)); err != nil {
			log.Println("Error sending membership prompt:", err)
		}
		return
	}

	if cb.Message != nil {
		deleteMessage(bot, chatID, cb.Message.MessageID)
	}

	deletePendingDownload(chatID)
	option := strings.TrimPrefix(cb.Data, callbackMediaOptionPrefix)
	executeMediaOption(bot, chatID, cb.From.ID, req, option)
}

func storePendingDownload(chatID int64, req state.PendingRequest) {
	if pendingStore == nil {
		pendingStore = state.NewPendingStoreFromEnv()
	}
	pendingStore.Set(chatID, req)
	log.Printf("pending stored chat_id=%d kind=%s platform=%s", chatID, req.Kind, req.Platform)
}

func getPendingDownload(chatID int64) (state.PendingRequest, bool) {
	if pendingStore == nil {
		pendingStore = state.NewPendingStoreFromEnv()
	}
	return pendingStore.Get(chatID)
}

func deletePendingDownload(chatID int64) {
	if pendingStore == nil {
		pendingStore = state.NewPendingStoreFromEnv()
	}
	pendingStore.Delete(chatID)
	log.Printf("pending deleted chat_id=%d", chatID)
}

func trackAudienceFromMessage(msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}

	contact := db.AudienceContact{
		UserID:       msg.From.ID,
		ChatID:       msg.Chat.ID,
		Username:     msg.From.UserName,
		FirstName:    msg.From.FirstName,
		LastName:     msg.From.LastName,
		ChatType:     msg.Chat.Type,
		ChatTitle:    msg.Chat.Title,
		ChatUsername: msg.Chat.UserName,
		IsBot:        msg.From.IsBot,
	}
	if err := db.UpsertAudienceContact(contact); err != nil {
		log.Printf("audience upsert failed user_id=%d chat_id=%d: %v", contact.UserID, contact.ChatID, err)
	}
}

func trackAudienceFromCallback(cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.From == nil {
		return
	}

	chatID := cb.From.ID
	chatType := "private"
	chatTitle := ""
	chatUsername := ""
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
		chatType = cb.Message.Chat.Type
		chatTitle = cb.Message.Chat.Title
		chatUsername = cb.Message.Chat.UserName
	}

	contact := db.AudienceContact{
		UserID:       cb.From.ID,
		ChatID:       chatID,
		Username:     cb.From.UserName,
		FirstName:    cb.From.FirstName,
		LastName:     cb.From.LastName,
		ChatType:     chatType,
		ChatTitle:    chatTitle,
		ChatUsername: chatUsername,
		IsBot:        cb.From.IsBot,
	}
	if err := db.UpsertAudienceContact(contact); err != nil {
		log.Printf("audience upsert failed user_id=%d chat_id=%d: %v", contact.UserID, contact.ChatID, err)
	}
}

func callbackChatID(cb *tgbotapi.CallbackQuery) int64 {
	if cb == nil || cb.From == nil {
		return 0
	}
	if cb.Message != nil {
		return cb.Message.Chat.ID
	}
	return cb.From.ID
}

func handleAdminRequiredChannelsCommand(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}

	if !isAdmin(msg.From) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Unauthorized")
		_, _ = bot.Send(reply)
		return
	}

	cmd := msg.Command()
	arg := strings.TrimSpace(msg.CommandArguments())

	switch cmd {
	case "req_list":
		channels, err := db.ListAllRequiredChannels()
		if err != nil {
			sendText(bot, msg.Chat.ID, "Failed to read channels: "+err.Error())
			return
		}
		if len(channels) == 0 {
			sendText(bot, msg.Chat.ID, "Required channels list is empty.")
			return
		}
		sendText(bot, msg.Chat.ID, "Required channels:\n@"+strings.Join(channels, "\n@"))
	case "req_add":
		ch := db.NormalizeChannel(arg)
		if ch == "" {
			sendText(bot, msg.Chat.ID, "Usage: /req_add @channel")
			return
		}
		if isStaticRequiredChannel(ch) {
			sendText(bot, msg.Chat.ID, "Channel is already in static required list.")
			return
		}
		if err := db.AddDynamicRequiredChannel(ch); err != nil {
			sendText(bot, msg.Chat.ID, "Failed to add channel: "+err.Error())
			return
		}
		sendText(bot, msg.Chat.ID, "Added: @"+ch)
	case "req_remove":
		ch := db.NormalizeChannel(arg)
		if ch == "" {
			sendText(bot, msg.Chat.ID, "Usage: /req_remove @channel")
			return
		}
		if isStaticRequiredChannel(ch) {
			sendText(bot, msg.Chat.ID, "This channel is static in .env and cannot be removed by command.")
			return
		}
		if err := db.RemoveDynamicRequiredChannel(ch); err != nil {
			sendText(bot, msg.Chat.ID, "Failed to remove channel: "+err.Error())
			return
		}
		sendText(bot, msg.Chat.ID, "Removed: @"+ch)
	}
}

func allowMediaRequest(user *tgbotapi.User) bool {
	if user == nil {
		return true
	}
	if isAdmin(user) {
		return true
	}

	limit := 0
	if raw := strings.TrimSpace(config.GetEnv("RATE_LIMIT_PER_HOUR", "")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit <= 0 {
		return true
	}

	if pendingStore == nil {
		pendingStore = state.NewPendingStoreFromEnv()
	}
	return pendingStore.AllowRequest(user.ID, limit)
}

func handleHelp(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	lang := userLang(msg.From.ID)

	var b strings.Builder
	b.WriteString(i18n.T(lang, i18n.KeyHelpIntro))
	b.WriteString("\n\n")
	b.WriteString(i18n.T(lang, i18n.KeyHelpPlatforms))
	b.WriteString("\n\n")
	b.WriteString(i18n.T(lang, i18n.KeyHelpPickFormat))
	b.WriteString("\n")
	b.WriteString(i18n.T(lang, i18n.KeyHelpPrivateLinks))

	if isAdmin(msg.From) {
		b.WriteString("\n\n")
		b.WriteString(i18n.T(lang, i18n.KeyHelpAdminHeader))
		b.WriteString("\n")
		b.WriteString(i18n.T(lang, i18n.KeyHelpAdminStats))
		b.WriteString("\n")
		b.WriteString(i18n.T(lang, i18n.KeyHelpAdminChannels))
	}

	sendText(bot, msg.Chat.ID, b.String())
}

func handleLang(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	arg := strings.ToLower(strings.TrimSpace(msg.CommandArguments()))
	if arg != "" && i18n.Supported(arg) {
		applyUserLanguage(bot, msg.Chat.ID, msg.From.ID, i18n.Normalize(arg))
		return
	}

	prompt := tgbotapi.NewMessage(msg.Chat.ID, i18n.T(userLang(msg.From.ID), i18n.KeyChooseLanguage))
	prompt.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇬🇧 English", callbackLangSetPrefix+i18n.LangEN),
			tgbotapi.NewInlineKeyboardButtonData("🇷🇺 Русский", callbackLangSetPrefix+i18n.LangRU),
		),
	)
	if _, err := bot.Send(prompt); err != nil {
		log.Println("Error sending language prompt:", err)
	}
}

func handleLangSetCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	if _, err := bot.Request(tgbotapi.NewCallback(cb.ID, "")); err != nil {
		log.Println("Error answering language callback:", err)
	}

	chatID := cb.From.ID
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
		deleteMessage(bot, chatID, cb.Message.MessageID)
	}

	lang := i18n.Normalize(strings.TrimPrefix(cb.Data, callbackLangSetPrefix))
	applyUserLanguage(bot, chatID, cb.From.ID, lang)
}

// applyUserLanguage persists the chosen language and confirms in that language.
func applyUserLanguage(bot *tgbotapi.BotAPI, chatID, userID int64, lang string) {
	if err := db.SetUserLanguage(userID, lang); err != nil {
		log.Printf("set user language failed user_id=%d lang=%s: %v", userID, lang, err)
	} else {
		log.Printf("user language set user_id=%d lang=%s", userID, lang)
	}
	sendText(bot, chatID, i18n.T(lang, i18n.KeyLangChanged))
}

func handleStats(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !isAdmin(msg.From) {
		return
	}

	var b strings.Builder
	b.WriteString("Bot statistics\n\n")

	if users, err := db.CountUniqueUsers(); err != nil {
		log.Println("stats: count unique users failed:", err)
		b.WriteString("Users: unavailable\n")
	} else {
		b.WriteString(fmt.Sprintf("Users: %d\n", users))
	}

	if chats, err := db.CountChatsByType(); err != nil {
		log.Println("stats: count chats by type failed:", err)
	} else if len(chats) > 0 {
		b.WriteString("Chats:\n")
		for _, c := range chats {
			label := c.ChatType
			if label == "" {
				label = "unknown"
			}
			b.WriteString(fmt.Sprintf("  • %s: %d\n", label, c.Count))
		}
	}

	b.WriteString("\n")
	total, totalErr := db.CountDownloads()
	if totalErr != nil {
		log.Println("stats: count downloads failed:", totalErr)
		b.WriteString("Downloads: unavailable")
	} else {
		okCount, _ := db.CountDownloadsByStatus(db.DownloadStatusOK)
		failCount, _ := db.CountDownloadsByStatus(db.DownloadStatusFailed)
		b.WriteString(fmt.Sprintf("Downloads: %d total (%d ok, %d failed)\n", total, okCount, failCount))

		if perPlatform, err := db.DownloadsByPlatform(); err != nil {
			log.Println("stats: downloads by platform failed:", err)
		} else if len(perPlatform) > 0 {
			b.WriteString("By platform:\n")
			for _, p := range perPlatform {
				b.WriteString(fmt.Sprintf("  • %s: %d\n", p.Platform, p.Count))
			}
		}
	}

	sendText(bot, msg.Chat.ID, strings.TrimRight(b.String(), "\n"))
}

func isAdmin(user *tgbotapi.User) bool {
	if user == nil {
		return false
	}

	adminIDStr := strings.TrimSpace(config.GetEnv("ADMIN_USER_ID", ""))
	if adminIDStr == "" {
		log.Println("WARNING: ADMIN_USER_ID is not set — admin commands are disabled")
		return false
	}

	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil || user.ID != adminID {
		return false
	}

	adminUsername := db.NormalizeChannel(config.GetEnv("ADMIN_USERNAME", ""))
	if adminUsername != "" && db.NormalizeChannel(user.UserName) != adminUsername {
		return false
	}

	return true
}

func isStaticRequiredChannel(ch string) bool {
	ch = db.NormalizeChannel(ch)
	base := config.GetEnv("BASE_REQUIRED_CHANNEL", "")
	if strings.TrimSpace(base) == "" {
		base = config.GetEnv("REQUIRED_CHANNELS", "")
	}
	for _, item := range strings.Split(base, ",") {
		if db.NormalizeChannel(item) == ch {
			return true
		}
	}
	return false
}

func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := bot.Send(msg); err != nil {
		log.Println("Error sending message:", err)
	}
}

// userLang resolves the UI language for a user: stored preference, else
// DEFAULT_LANG, normalized to a supported code.
func userLang(userID int64) string {
	if userID != 0 {
		if lang, err := db.GetUserLanguage(userID); err != nil {
			log.Printf("get user language failed user_id=%d: %v", userID, err)
		} else if lang != "" {
			return i18n.Normalize(lang)
		}
	}
	return i18n.Normalize(config.GetEnv("DEFAULT_LANG", i18n.LangEN))
}

func buildDefaultMediaOptions() []state.MediaOption {
	mp4 := []state.MediaOption{
		{
			Key:      "yt_q1080",
			Label:    "MP4 1080p",
			Mode:     optionModeVideoMP4,
			Selector: "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080][ext=mp4]/best[height<=1080]",
		},
		{
			Key:      "yt_q720",
			Label:    "MP4 720p",
			Mode:     optionModeVideoMP4,
			Selector: "bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/best[height<=720][ext=mp4]/best[height<=720]",
		},
		{
			Key:      "yt_q480",
			Label:    "MP4 480p",
			Mode:     optionModeVideoMP4,
			Selector: "bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]/best[height<=480][ext=mp4]/best[height<=480]",
		},
	}

	mp4 = orderByDefaultQuality(mp4)

	return append(mp4,
		state.MediaOption{
			Key:   optionKeyBoth,
			Label: "MP4 + MP3",
			Mode:  optionModeBoth,
		},
		state.MediaOption{
			Key:   optionKeyMP3,
			Label: "MP3",
			Mode:  optionModeAudioMP3,
		},
	)
}

// orderByDefaultQuality moves the option matching DEFAULT_VIDEO_QUALITY to the
// front so it reads as the recommended default. "best" (or unset/unknown) keeps
// the natural high-to-low order.
func orderByDefaultQuality(options []state.MediaOption) []state.MediaOption {
	pref := strings.ToLower(strings.TrimSpace(config.GetEnv("DEFAULT_VIDEO_QUALITY", "best")))
	if pref == "" || pref == "best" {
		return options
	}

	wantKey := "yt_q" + pref
	idx := -1
	for i, opt := range options {
		if opt.Key == wantKey {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return options
	}

	reordered := make([]state.MediaOption, 0, len(options))
	reordered = append(reordered, options[idx])
	reordered = append(reordered, options[:idx]...)
	reordered = append(reordered, options[idx+1:]...)
	return reordered
}

func buildMediaOptionsForPlatform(platform, link string) []state.MediaOption {
	if strings.EqualFold(platform, "YouTube") {
		qualityOptions, err := instagram.ListVideoQualityOptions(link)
		if err != nil {
			log.Printf("ListVideoQualityOptions failed for %s: %v", platform, err)
			return buildDefaultMediaOptions()
		}

		opts := make([]state.MediaOption, 0, len(qualityOptions)+2)
		seen := make(map[string]struct{})
		bestSelector := ""
		bestMP3SizeBytes := int64(0)

		for _, q := range qualityOptions {
			key := "yt_" + sanitizeOptionKey(q.Key)
			if key == "yt_" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			label := q.Label
			if q.EstimatedBytes > 0 {
				label = fmt.Sprintf("%s • %s", label, formatApproxSize(q.EstimatedBytes))
			}
			opts = append(opts, state.MediaOption{
				Key:      key,
				Label:    label,
				Mode:     optionModeVideoMP4,
				Selector: q.Selector,
			})
			if bestSelector == "" {
				bestSelector = q.Selector
				bestMP3SizeBytes = q.EstimatedMP3Bytes
			}
		}

		if bestSelector == "" {
			bestSelector = "best[ext=mp4]/best"
		}

		bothLabel := "MP4 + MP3"
		if len(qualityOptions) > 0 {
			if qualityOptions[0].EstimatedBytes > 0 && bestMP3SizeBytes > 0 {
				bothLabel = fmt.Sprintf("MP4 + MP3 • %s + %s", formatApproxSize(qualityOptions[0].EstimatedBytes), formatApproxSize(bestMP3SizeBytes))
			} else if qualityOptions[0].EstimatedBytes > 0 {
				bothLabel = fmt.Sprintf("MP4 + MP3 • %s", formatApproxSize(qualityOptions[0].EstimatedBytes))
			}
		}

		mp3Label := "MP3"
		if bestMP3SizeBytes > 0 {
			mp3Label = fmt.Sprintf("MP3 • %s", formatApproxSize(bestMP3SizeBytes))
		}

		opts = append(opts,
			state.MediaOption{
				Key:      optionKeyBoth,
				Label:    bothLabel,
				Mode:     optionModeBoth,
				Selector: bestSelector,
			},
			state.MediaOption{
				Key:   optionKeyMP3,
				Label: mp3Label,
				Mode:  optionModeAudioMP3,
			},
		)

		return opts
	}

	return buildDefaultMediaOptions()
}

func sanitizeOptionKey(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatApproxSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	value := float64(bytes) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	if value >= 100 {
		return fmt.Sprintf("%.0f%s", value, suffix)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f%s", value, suffix)
	}
	return fmt.Sprintf("%.2f%s", value, suffix)
}

func buildMediaOptionsKeyboard(options []state.MediaOption) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, (len(options)+1)/2)
	currentRow := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	var mp3Button *tgbotapi.InlineKeyboardButton

	appendButton := func(btn tgbotapi.InlineKeyboardButton) {
		currentRow = append(currentRow, btn)
		if len(currentRow) == 2 {
			rows = append(rows, currentRow)
			currentRow = make([]tgbotapi.InlineKeyboardButton, 0, 2)
		}
	}

	for _, option := range options {
		if strings.TrimSpace(option.Key) == "" {
			continue
		}

		btn := tgbotapi.NewInlineKeyboardButtonData(option.Label, callbackMediaOptionPrefix+option.Key)
		if option.Key == optionKeyMP3 {
			mp3Button = &btn
			continue
		}

		appendButton(btn)
	}

	if mp3Button != nil {
		appendButton(*mp3Button)
	}

	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	if len(rows) == 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("MP4", callbackMediaOptionPrefix+optionKeyMP4),
		))
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func findMediaOption(options []state.MediaOption, key string) (state.MediaOption, bool) {
	for _, option := range options {
		if option.Key == key {
			return option, true
		}
	}
	return state.MediaOption{}, false
}
