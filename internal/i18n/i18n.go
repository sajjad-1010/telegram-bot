// Package i18n provides minimal message translation for the bot UI (en + ru).
package i18n

import (
	"fmt"
	"strings"
)

// Supported language codes.
const (
	LangEN = "en"
	LangRU = "ru"
)

// Translation keys. Kept as exported constants so callers get compile-time
// checking instead of stringly-typed lookups.
const (
	KeyStart                 = "start"
	KeyWelcome               = "welcome"
	KeyInvalidLink           = "invalid_link"
	KeyFetchFailed           = "fetch_failed"
	KeyUnknownPending        = "unknown_pending"
	KeyUnknownOption         = "unknown_option"
	KeyRateLimited           = "rate_limited"
	KeyMembershipRequired    = "membership_required"
	KeyStillNotMember        = "still_not_member"
	KeyNoPendingRequest      = "no_pending_request"
	KeyNoPendingSelection    = "no_pending_selection"
	KeyChooseFormat          = "choose_format"
	KeyDownloading           = "downloading"
	KeyLangChanged           = "lang_changed"
	KeyChooseLanguage        = "choose_language"

	KeyHelpIntro             = "help_intro"
	KeyHelpPlatforms         = "help_platforms"
	KeyHelpPickFormat        = "help_pick_format"
	KeyHelpPrivateLinks      = "help_private_links"
	KeyHelpAdminHeader       = "help_admin_header"
	KeyHelpAdminStats        = "help_admin_stats"
	KeyHelpAdminChannels     = "help_admin_channels"

	// Download error messages (format verbs preserved).
	KeyErrGeneric            = "err_generic"
	KeyErrVideoTelegramLimit = "err_video_tg_limit"
	KeyErrAudioTelegramLimit = "err_audio_tg_limit"
	KeyErrFileTelegramLimit  = "err_file_tg_limit"
	KeyErrVideoSizeLimit     = "err_video_size_limit"
	KeyErrAudioSizeLimit     = "err_audio_size_limit"
	KeyErrFileSizeLimit      = "err_file_size_limit"
	KeyErrVideoTimeout       = "err_video_timeout"
	KeyErrAudioTimeout       = "err_audio_timeout"
	KeyErrTimeout            = "err_timeout"
	KeyErrNoFfmpeg           = "err_no_ffmpeg"
	KeyErrVideoSendFailed    = "err_video_send_failed"
	KeyErrAudioSendFailed    = "err_audio_send_failed"
)

var translations = map[string]map[string]string{
	LangEN: {
		KeyStart:              "Open the private link of a file to receive it.",
		KeyWelcome: "👋 Welcome! I download media from social links.\n\n" +
			"📥 *How to use:* just send me a link.\n\n" +
			"*Supported:*\n" +
			"• Instagram — reel / post / story / TV\n" +
			"• TikTok\n" +
			"• YouTube — video / shorts / live\n" +
			"• Reddit\n" +
			"• Twitter / X\n" +
			"• Pinterest\n\n" +
			"🎞 For some links you pick the format (MP4 / MP3).\n\n" +
			"*Commands:*\n" +
			"• /help — full guide\n" +
			"• /lang — change language (English / Русский)",
		KeyInvalidLink:        "Invalid link. Please open a valid file link.",
		KeyFetchFailed:        "Failed to fetch file. Try again later.",
		KeyUnknownPending:     "Unknown pending request. Please send the link again.",
		KeyUnknownOption:      "Unknown download option.",
		KeyRateLimited:        "You're sending requests too fast. Please try again later.",
		KeyMembershipRequired: "Join required channels first, then tap Check membership.",
		KeyStillNotMember:     "You are still not a member of all required channels.",
		KeyNoPendingRequest:   "No pending request found. Please send the link again.",
		KeyNoPendingSelection: "No pending media selection found. Send the link again.",
		KeyChooseFormat:       "Choose format for %s:",
		KeyDownloading:        "Downloading from %s, please wait...",
		KeyLangChanged:        "Language set to English.",
		KeyChooseLanguage:     "Choose a language:",

		KeyHelpIntro:         "Send a link and I'll fetch the media.",
		KeyHelpPlatforms:     "Supported platforms:\n• Instagram (reel / post / story / TV)\n• TikTok\n• YouTube (video / shorts / live)\n• Reddit\n• Twitter / X\n• Pinterest",
		KeyHelpPickFormat:    "For some links you'll be asked to pick a format (MP4 / MP3 / both).",
		KeyHelpPrivateLinks:  "Private file links open via /start and deliver the file to you.",
		KeyHelpAdminHeader:   "Admin commands:",
		KeyHelpAdminStats:    "• /stats — usage statistics",
		KeyHelpAdminChannels: "• /req_list, /req_add <ch>, /req_remove <ch> — required channels",

		KeyErrGeneric:            "Download failed. Please try again later.",
		KeyErrVideoTelegramLimit: "%s video exceeds Telegram's %dMB upload limit. Please choose a lower quality.",
		KeyErrAudioTelegramLimit: "%s audio exceeds Telegram's %dMB upload limit.",
		KeyErrFileTelegramLimit:  "The file exceeds Telegram's %dMB upload limit.",
		KeyErrVideoSizeLimit:     "%s video is larger than the current %dMB limit. Choose a lower quality or use MP3.",
		KeyErrAudioSizeLimit:     "%s audio is larger than the current %dMB limit.",
		KeyErrFileSizeLimit:      "The downloaded file is larger than the current %dMB limit.",
		KeyErrVideoTimeout:       "%s download took too long and timed out. Try a lower quality.",
		KeyErrAudioTimeout:       "%s audio download took too long and timed out. Please try again later.",
		KeyErrTimeout:            "Download took too long and timed out. Please try again later.",
		KeyErrNoFfmpeg:           "MP3 conversion is not available right now because ffmpeg/ffprobe is not configured on the server.",
		KeyErrVideoSendFailed:    "Failed to send %s video. Please try another quality.",
		KeyErrAudioSendFailed:    "Failed to send %s audio. Please try again later.",
	},
	LangRU: {
		KeyStart:              "Откройте приватную ссылку файла, чтобы получить его.",
		KeyWelcome: "👋 Привет! Я скачиваю медиа по ссылкам из соцсетей.\n\n" +
			"📥 *Как пользоваться:* просто отправьте мне ссылку.\n\n" +
			"*Поддерживается:*\n" +
			"• Instagram — reel / пост / истории / TV\n" +
			"• TikTok\n" +
			"• YouTube — видео / shorts / трансляции\n" +
			"• Reddit\n" +
			"• Twitter / X\n" +
			"• Pinterest\n\n" +
			"🎞 Для некоторых ссылок нужно выбрать формат (MP4 / MP3).\n\n" +
			"*Команды:*\n" +
			"• /help — полное руководство\n" +
			"• /lang — сменить язык (English / Русский)",
		KeyInvalidLink:        "Неверная ссылка. Пожалуйста, откройте корректную ссылку на файл.",
		KeyFetchFailed:        "Не удалось получить файл. Попробуйте позже.",
		KeyUnknownPending:     "Неизвестный запрос. Пожалуйста, отправьте ссылку ещё раз.",
		KeyUnknownOption:      "Неизвестный вариант загрузки.",
		KeyRateLimited:        "Вы отправляете запросы слишком часто. Попробуйте позже.",
		KeyMembershipRequired: "Сначала подпишитесь на обязательные каналы, затем нажмите «Проверить подписку».",
		KeyStillNotMember:     "Вы всё ещё не подписаны на все обязательные каналы.",
		KeyNoPendingRequest:   "Активный запрос не найден. Пожалуйста, отправьте ссылку ещё раз.",
		KeyNoPendingSelection: "Выбор медиа не найден. Отправьте ссылку ещё раз.",
		KeyChooseFormat:       "Выберите формат для %s:",
		KeyDownloading:        "Загружаю из %s, пожалуйста, подождите...",
		KeyLangChanged:        "Язык изменён на русский.",
		KeyChooseLanguage:     "Выберите язык:",

		KeyHelpIntro:         "Отправьте ссылку, и я скачаю медиа.",
		KeyHelpPlatforms:     "Поддерживаемые платформы:\n• Instagram (reel / пост / истории / TV)\n• TikTok\n• YouTube (видео / shorts / трансляции)\n• Reddit\n• Twitter / X\n• Pinterest",
		KeyHelpPickFormat:    "Для некоторых ссылок нужно будет выбрать формат (MP4 / MP3 / оба).",
		KeyHelpPrivateLinks:  "Приватные ссылки на файлы открываются через /start и доставляют файл вам.",
		KeyHelpAdminHeader:   "Команды администратора:",
		KeyHelpAdminStats:    "• /stats — статистика использования",
		KeyHelpAdminChannels: "• /req_list, /req_add <ch>, /req_remove <ch> — обязательные каналы",

		KeyErrGeneric:            "Не удалось скачать. Попробуйте позже.",
		KeyErrVideoTelegramLimit: "Видео из %s превышает лимит загрузки Telegram %dМБ. Пожалуйста, выберите более низкое качество.",
		KeyErrAudioTelegramLimit: "Аудио из %s превышает лимит загрузки Telegram %dМБ.",
		KeyErrFileTelegramLimit:  "Файл превышает лимит загрузки Telegram %dМБ.",
		KeyErrVideoSizeLimit:     "Видео из %s больше текущего лимита %dМБ. Выберите более низкое качество или используйте MP3.",
		KeyErrAudioSizeLimit:     "Аудио из %s больше текущего лимита %dМБ.",
		KeyErrFileSizeLimit:      "Скачанный файл больше текущего лимита %dМБ.",
		KeyErrVideoTimeout:       "Загрузка из %s заняла слишком много времени и была прервана. Попробуйте более низкое качество.",
		KeyErrAudioTimeout:       "Загрузка аудио из %s заняла слишком много времени и была прервана. Попробуйте позже.",
		KeyErrTimeout:            "Загрузка заняла слишком много времени и была прервана. Попробуйте позже.",
		KeyErrNoFfmpeg:           "Конвертация в MP3 сейчас недоступна, потому что ffmpeg/ffprobe не настроен на сервере.",
		KeyErrVideoSendFailed:    "Не удалось отправить видео из %s. Попробуйте другое качество.",
		KeyErrAudioSendFailed:    "Не удалось отправить аудио из %s. Попробуйте позже.",
	},
}

// Normalize maps arbitrary input to a supported language code, defaulting to en.
func Normalize(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case LangRU, "russian", "rus", "ру", "рус":
		return LangRU
	default:
		return LangEN
	}
}

// T returns the translation for key in lang, falling back to English and then
// the key itself so a missing translation never blanks the UI.
func T(lang, key string) string {
	lang = Normalize(lang)
	if s, ok := translations[lang][key]; ok {
		return s
	}
	if s, ok := translations[LangEN][key]; ok {
		return s
	}
	return key
}

// Tf is fmt.Sprintf over T for messages with format verbs.
func Tf(lang, key string, args ...any) string {
	return fmt.Sprintf(T(lang, key), args...)
}

// Supported returns true if lang is a recognized code (before normalization).
func Supported(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == LangEN || l == LangRU
}
