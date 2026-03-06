package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func GetJoinChannelsButtons(channels []string) []tgbotapi.InlineKeyboardButton {
	var buttons []tgbotapi.InlineKeyboardButton
	for _, ch := range channels {
		btn := tgbotapi.NewInlineKeyboardButtonURL("Join "+ch, "https://t.me/"+ch)
		buttons = append(buttons, btn)
	}
	return buttons
}

func MakeDeepLink(botUsername, payload string) string {
	cleanUser := strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	return fmt.Sprintf("https://t.me/%s?start=%s", cleanUser, payload)
}

func MakeForwardButton(botUsername, payload string) tgbotapi.InlineKeyboardButton {
	url := MakeDeepLink(botUsername, payload)
	return tgbotapi.NewInlineKeyboardButtonURL("Open message", url)
}

func Encode(num int) string {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, int64(num))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func Decode(encoded string) (int, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return 0, errors.New("invalid base64 format")
	}
	buf := bytes.NewReader(data)
	var num int64
	err = binary.Read(buf, binary.LittleEndian, &num)
	if err != nil {
		return 0, errors.New("invalid binary data")
	}
	return int(num), nil
}
