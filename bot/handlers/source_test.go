package handlers

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func setSource(t *testing.T, source, links int64, channel *tgbotapi.Chat) {
	t.Helper()
	prevSource, prevLinks := forwardFromChatID, linksChatID
	sourceChannelMu.Lock()
	prevChannel := sourceChannel
	sourceChannel = channel
	sourceChannelMu.Unlock()
	forwardFromChatID, linksChatID = source, links
	t.Cleanup(func() {
		forwardFromChatID, linksChatID = prevSource, prevLinks
		sourceChannelMu.Lock()
		sourceChannel = prevChannel
		sourceChannelMu.Unlock()
	})
}

func TestLinkDestinationGroupRepliesInPlace(t *testing.T) {
	setSource(t, -1001, 0, nil)
	chatID, replyTo, note, err := linkDestination(42)
	if err != nil || chatID != -1001 || replyTo != 42 || note != "" {
		t.Fatalf("got chat=%d reply=%d note=%q err=%v", chatID, replyTo, note, err)
	}
}

func TestLinkDestinationChannelGoesToAdmin(t *testing.T) {
	t.Setenv("ADMIN_USER_ID", "777")
	setSource(t, -1009876543210, 0, &tgbotapi.Chat{ID: -1009876543210, Type: "channel"})
	chatID, replyTo, note, err := linkDestination(5)
	if err != nil || chatID != 777 || replyTo != 0 {
		t.Fatalf("got chat=%d reply=%d err=%v, want admin DM without reply", chatID, replyTo, err)
	}
	if note != "https://t.me/c/9876543210/5" {
		t.Fatalf("note = %q", note)
	}
}

func TestLinkDestinationChannelWithoutTargetFails(t *testing.T) {
	t.Setenv("ADMIN_USER_ID", "")
	setSource(t, -100123, 0, &tgbotapi.Chat{ID: -100123, Type: "channel"})
	if _, _, _, err := linkDestination(1); err == nil {
		t.Fatal("a channel with no LINKS_CHAT_ID/ADMIN_USER_ID must not post the link into the channel")
	}
}

func TestLinkDestinationLinksChatOverrides(t *testing.T) {
	setSource(t, -100123, -200, &tgbotapi.Chat{ID: -100123, Type: "channel", UserName: "mychan"})
	chatID, _, note, err := linkDestination(9)
	if err != nil || chatID != -200 || note != "https://t.me/mychan/9" {
		t.Fatalf("got chat=%d note=%q err=%v", chatID, note, err)
	}
}
