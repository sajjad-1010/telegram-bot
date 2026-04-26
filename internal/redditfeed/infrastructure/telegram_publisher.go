package infrastructure

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/internal/redditfeed/domain"
	"telegram-bot/providers/instagram"
)

type TelegramPublisher struct {
	bot    *tgbotapi.BotAPI
	chatID int64
}

func NewTelegramPublisher(bot *tgbotapi.BotAPI, chatID int64) *TelegramPublisher {
	return &TelegramPublisher{
		bot:    bot,
		chatID: chatID,
	}
}

func (p *TelegramPublisher) Publish(ctx context.Context, topic domain.Topic, post domain.Post) error {
	if p.bot == nil {
		return fmt.Errorf("telegram bot is nil")
	}
	_ = ctx

	log.Printf("reddit feed publish start topic=%s post_id=%s subreddit=%s", topic, post.ID, post.Subreddit)

	switch {
	case post.NativeMedia && strings.TrimSpace(post.DownloadURL) != "":
		if err := p.publishNativeMedia(ctx, topic, post); err != nil {
			return err
		}
	case post.ExternalMedia && strings.TrimSpace(post.ExternalMediaURL) != "":
		if err := p.sendTextMessage(buildExternalMessage(post)); err != nil {
			return err
		}
		if err := p.sendTopicMarker(topic); err != nil {
			return err
		}
	default:
		if err := p.sendTextMessage(buildTextPostMessage(post)); err != nil {
			return err
		}
		if err := p.sendTopicMarker(topic); err != nil {
			return err
		}
	}

	log.Printf("reddit feed publish complete topic=%s post_id=%s", topic, post.ID)
	return nil
}

func (p *TelegramPublisher) publishNativeMedia(ctx context.Context, topic domain.Topic, post domain.Post) error {
	filePaths, cleanup, err := instagram.DownloadBestContents(post.DownloadURL)
	if err != nil {
		return fmt.Errorf("download reddit native media: %w", err)
	}
	defer cleanup()

	if _, err := p.sendDownloadedContents(filePaths, trimCaption(post.Caption())); err != nil {
		return err
	}

	if err := p.sendTopicMarker(topic); err != nil {
		return err
	}
	return nil
}

func (p *TelegramPublisher) sendTextMessage(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text message is empty")
	}

	msg := tgbotapi.NewMessage(p.chatID, text)
	_, err := p.bot.Send(msg)
	return err
}

func (p *TelegramPublisher) sendTopicMarker(topic domain.Topic) error {
	msg := tgbotapi.NewMessage(p.chatID, topic.String())
	_, err := p.bot.Send(msg)
	return err
}

type publishSummary struct {
	allPhotos bool
}

func (p *TelegramPublisher) sendDownloadedContents(filePaths []string, caption string) (publishSummary, error) {
	if len(filePaths) == 0 {
		return publishSummary{}, fmt.Errorf("no downloaded content files found")
	}

	if len(filePaths) == 1 {
		kind, err := p.sendDownloadedContent(filePaths[0], caption)
		return publishSummary{allPhotos: kind == "photo"}, err
	}

	summary := publishSummary{allPhotos: true}
	canUseMediaGroup := len(filePaths) <= 10
	for _, filePath := range filePaths {
		ext := strings.ToLower(filepath.Ext(filePath))
		if !isImageExt(ext) {
			summary.allPhotos = false
		}
		if !isImageExt(ext) && !isVideoExt(ext) {
			canUseMediaGroup = false
		}
	}

	if canUseMediaGroup {
		if err := p.sendDownloadedMediaGroup(filePaths, caption); err == nil {
			return summary, nil
		}
	}

	for idx, filePath := range filePaths {
		itemCaption := ""
		if idx == 0 {
			itemCaption = caption
		}
		if _, err := p.sendDownloadedContent(filePath, itemCaption); err != nil {
			return summary, fmt.Errorf("send item %d (%s): %w", idx+1, filepath.Base(filePath), err)
		}
	}

	return summary, nil
}

func (p *TelegramPublisher) sendDownloadedMediaGroup(filePaths []string, caption string) error {
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

	group := tgbotapi.NewMediaGroup(p.chatID, media)
	_, err := p.bot.SendMediaGroup(group)
	return err
}

func (p *TelegramPublisher) sendDownloadedContent(filePath, caption string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	if isImageExt(ext) {
		photo := tgbotapi.NewPhoto(p.chatID, tgbotapi.FilePath(filePath))
		photo.Caption = caption
		_, err := p.bot.Send(photo)
		return "photo", err
	}

	if isAudioExt(ext) {
		audio := tgbotapi.NewAudio(p.chatID, tgbotapi.FilePath(filePath))
		audio.Caption = caption
		_, err := p.bot.Send(audio)
		return "audio", err
	}

	if isVideoExt(ext) {
		if err := p.sendVideoWithFallback(filePath, caption); err != nil {
			return "video", err
		}
		return "video", nil
	}

	doc := tgbotapi.NewDocument(p.chatID, tgbotapi.FilePath(filePath))
	doc.Caption = caption
	_, err := p.bot.Send(doc)
	return "document", err
}

func (p *TelegramPublisher) sendVideoWithFallback(filePath, caption string) error {
	video := tgbotapi.NewVideo(p.chatID, tgbotapi.FilePath(filePath))
	video.Caption = caption
	video.SupportsStreaming = true
	if _, err := p.bot.Send(video); err == nil {
		return nil
	}

	normalizedPath, cleanup, err := instagram.NormalizeForTelegram(filePath)
	if err == nil {
		defer cleanup()
		video := tgbotapi.NewVideo(p.chatID, tgbotapi.FilePath(normalizedPath))
		video.Caption = caption
		video.SupportsStreaming = true
		if _, retryErr := p.bot.Send(video); retryErr == nil {
			return nil
		}
	}

	doc := tgbotapi.NewDocument(p.chatID, tgbotapi.FilePath(filePath))
	doc.Caption = caption
	_, err = p.bot.Send(doc)
	return err
}

func buildExternalMessage(post domain.Post) string {
	parts := make([]string, 0, 2)
	if title := strings.TrimSpace(post.Title); title != "" {
		parts = append(parts, title)
	}
	if link := strings.TrimSpace(post.ExternalMediaURL); link != "" {
		parts = append(parts, link)
	}
	return strings.Join(parts, "\n\n")
}

func buildTextPostMessage(post domain.Post) string {
	text := strings.TrimSpace(post.TextBody())
	if text != "" {
		return trimText(text, 4000)
	}
	if link := strings.TrimSpace(post.Permalink); link != "" {
		return link
	}
	return "reddit post"
}

func trimCaption(raw string) string {
	return trimText(raw, 1024)
}

func trimText(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= maxLen {
		return raw
	}
	if maxLen <= 3 {
		return raw[:maxLen]
	}
	return raw[:maxLen-3] + "..."
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
