package handlers

import (
	"os"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-bot/utils"
)

func testBot() *tgbotapi.BotAPI {
	return &tgbotapi.BotAPI{Self: tgbotapi.User{UserName: "stashdwlbot"}}
}

func TestPayloadFromLink(t *testing.T) {
	bot := testBot()
	payload := utils.Encode(157)

	cases := []struct {
		name    string
		token   string
		want    string
		wantErr string
	}{
		{"full link", "https://t.me/stashdwlbot?start=" + payload, payload, ""},
		{"telegram.me host", "https://telegram.me/stashdwlbot?start=" + payload, payload, ""},
		{"case insensitive username", "https://t.me/StashDwlBot?start=" + payload, payload, ""},
		{"album link", "https://t.me/stashdwlbot?start=mg_123", "mg_123", ""},
		{"bare payload", payload, payload, ""},
		{"trailing punctuation", "https://t.me/stashdwlbot?start=mg_123,", "mg_123", ""},
		{"other host", "https://example.com/stashdwlbot?start=" + payload, "", "لینک نامعتبر"},
		{"other bot", "https://t.me/someotherbot?start=" + payload, "", "لینک این ربات نیست"},
		{"no start param", "https://t.me/stashdwlbot", "", "لینک خراب"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := payloadFromLink(bot, tc.token)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCountLinkStartsFindsGluedLinks(t *testing.T) {
	glued := "https://t.me/stashdwlbot?start=AAAhttps://t.me/stashdwlbot?start=BBB"
	if got := countLinkStarts(glued); got != 2 {
		t.Fatalf("want 2 starts in a glued token, got %d", got)
	}
	if got := countLinkStarts("https://t.me/stashdwlbot?start=AAA"); got != 1 {
		t.Fatalf("want 1 start, got %d", got)
	}
}

func TestDeliveryCaption(t *testing.T) {
	t.Run("default is the fixed caption", func(t *testing.T) {
		os.Unsetenv("DELIVERY_CAPTION")
		if got := deliveryCaption("caption of the group post"); got != defaultDeliveryCaption {
			t.Fatalf("want %q, got %q", defaultDeliveryCaption, got)
		}
	})

	t.Run("env replaces it", func(t *testing.T) {
		os.Setenv("DELIVERY_CAPTION", "متن جدید")
		defer os.Unsetenv("DELIVERY_CAPTION")
		if got := deliveryCaption("caption of the group post"); got != "متن جدید" {
			t.Fatalf("want the env value, got %q", got)
		}
	})

	t.Run("empty env falls back to the source caption", func(t *testing.T) {
		os.Setenv("DELIVERY_CAPTION", "  ")
		defer os.Unsetenv("DELIVERY_CAPTION")
		if got := deliveryCaption("caption of the group post"); got != "caption of the group post" {
			t.Fatalf("want the source caption, got %q", got)
		}
	})
}
