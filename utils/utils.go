package utils

import ( 
	"log"
    "fmt"
    "strings"
	"strconv"
	"math/rand"

	"telegram-bot/config"

    "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	xorKey uint
)

func init() {
	config.LoadEnv()
	
	xorKeyStr := config.GetEnv("xorKey", "")
		if xorKeyStr != "" {
		key, err := strconv.ParseUint(strings.TrimPrefix(xorKeyStr, "0x"), 16, 32)
		if err != nil {
			log.Printf("Error parsing xorKey: %v", err)
		} else {
			xorKey = uint(key)
		}
	} else {
		log.Println("xorKey is not set!")
	}
}

func GetJoinChannelsButtons(channels []string) []tgbotapi.InlineKeyboardButton {
    var buttons []tgbotapi.InlineKeyboardButton
    for _, ch := range channels {
        btn := tgbotapi.NewInlineKeyboardButtonURL("عضویت در "+ch, "https://t.me/"+ch)
        buttons = append(buttons, btn)
    }
    return buttons
}


func MakeLinkForForwardingMessage(msgID int) tgbotapi.InlineKeyboardButton {
    url := fmt.Sprintf("https://t.me/realblyat_bot?start=msg%d", msgID)
    return tgbotapi.NewInlineKeyboardButtonURL("مشاهده پیام", url)
}

func Encode(messageID int) string {

	u := uint(messageID) ^ xorKey

	base26Str := ToBase26(u)

	var builder strings.Builder
	builder.WriteString(base26Str)
	for builder.Len() < 10 {
		builder.WriteByte(alphabet[rand.Intn(len(alphabet))])
	}

	encoded := "_" + builder.String()

	return encoded
}

func Decode(encoded string) (int, error) {
	
	if len(encoded) != 11 || !strings.HasPrefix(encoded, "_") {
		return 0, fmt.Errorf("invalid format")
	}
	
	base26Str := encoded[1:5]
	
	u, err := fromBase26(base26Str)
	if err != nil {
		return 0, fmt.Errorf("invalid format: %v", err)
	}
	
	u ^= xorKey
	
	return int(u), nil
}

func ToBase26(n uint) (string) {
	if n == 0 {
		return "A"
	}
	var builder strings.Builder
	for n > 0 {
		r := n % 26
		builder.WriteByte(alphabet[r])
		n /= 26
	}
	s := builder.String()
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func fromBase26(s string) (uint, error) {
	var u uint
	for _, c := range s {
		idx := strings.IndexByte(alphabet, byte(c))
		if idx < 0 {
			return 0, fmt.Errorf("invalid character: %c", c)
		}
		if u > (^uint(0)-uint(idx))/26 {
			return 0, fmt.Errorf("overflow")
		}
		u = u*26 + uint(idx)
	}
	return u, nil
}