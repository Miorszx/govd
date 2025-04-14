package util

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/aki237/nscjar"
)

func GetLocationURL(
	url string,
	userAgent string,
) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	if userAgent == "" {
		userAgent = ChromeUA
	}
	req.Header.Set("User-Agent", ChromeUA)
	session := GetHTTPSession()
	resp, err := session.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	return resp.Request.URL.String(), nil
}

func IsUserAdmin(
	bot *gotgbot.Bot,
	chatID int64,
	userID int64,
) bool {
	chatMember, err := bot.GetChatMember(chatID, userID, nil)
	if err != nil {
		return false
	}
	if chatMember == nil {
		return false
	}
	status := chatMember.GetStatus()
	switch status {
	case "creator":
		return true
	case "administrator":
		if chatMember.MergeChatMember().CanChangeInfo {
			return true
		}
		return false
	}
	return false
}

func EscapeCaption(str string) string {
	// we wont use html.EscapeString
	// cuz it will escape all the characters
	// and we only need to escape < and >
	chars := map[string]string{
		"<": "&lt;",
		">": "&gt;",
	}
	for k, v := range chars {
		str = strings.ReplaceAll(str, k, v)
	}
	return str
}

func GetLastError(err error) error {
	var lastErr error = err
	for {
		unwrapped := errors.Unwrap(lastErr)
		if unwrapped == nil {
			break
		}
		lastErr = unwrapped
	}
	return lastErr
}

func ParseCookieFile(fileName string) ([]*http.Cookie, error) {
	cookiePath := filepath.Join("cookies", fileName)
	cookieFile, err := os.Open(cookiePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cookie file: %w", err)
	}
	defer cookieFile.Close()

	var parser nscjar.Parser
	cookies, err := parser.Unmarshal(cookieFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cookie file: %w", err)
	}
	return cookies, nil
}

func FixURL(url string) string {
	return strings.ReplaceAll(url, "&amp;", "&")
}

func CheckFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}
