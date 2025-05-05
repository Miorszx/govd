package util

import (
	"crypto/rand"
	"fmt"
	"govd/models"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/publicsuffix"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/aki237/nscjar"
)

var cookiesCache = make(map[string][]*http.Cookie)

func GetLocationURL(
	client models.HTTPClient,
	url string,
	headers map[string]string,
) (string, error) {
	if client == nil {
		client = GetDefaultHTTPClient()
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", ChromeUA)
	}
	resp, err := client.Do(req)
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
	var lastErr = err
	for {
		unwrapped := errors.Unwrap(lastErr)
		if unwrapped == nil {
			break
		}
		lastErr = unwrapped
	}
	return lastErr
}

func RandomBase64(length int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	const mask = 63 // 6 bits, since len(letters) == 64

	result := make([]byte, length)
	random := make([]byte, length)
	_, err := rand.Read(random)
	if err != nil {
		return strings.Repeat("A", length)
	}
	for i, b := range random {
		result[i] = letters[int(b)&mask]
	}
	return string(result)
}

func RandomAlphaString(length int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const lettersLen = byte(len(letters))
	const maxByte = 255 - (255 % lettersLen) // 255 - (255 % 52) = 255 - 47 = 208

	result := make([]byte, length)
	i := 0
	for i < length {
		b := make([]byte, 1)
		_, err := rand.Read(b)
		if err != nil {
			return strings.Repeat("a", length)
		}
		if b[0] > maxByte {
			continue // avoid bias
		}
		result[i] = letters[b[0]%lettersLen]
		i++
	}
	return string(result)
}

func ParseCookieFile(fileName string) ([]*http.Cookie, error) {
	cachedCookies, ok := cookiesCache[fileName]
	if ok {
		return cachedCookies, nil
	}
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
	cookiesCache[fileName] = cookies
	return cookies, nil
}

func FixURL(url string) string {
	return strings.ReplaceAll(url, "&amp;", "&")
}

func CheckFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func CleanupDownloadsDir() {
	downloadsDir := os.Getenv("DOWNLOAD_DIR")
	if downloadsDir == "" {
		downloadsDir = "downloads"
	}
	filepath.Walk(downloadsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == downloadsDir {
			return nil
		}
		if time.Since(info.ModTime()) > 30*time.Minute {
			if info.IsDir() {
				os.RemoveAll(path)
			} else {
				os.Remove(path)
			}
		}
		return nil
	})
}

func ExtractBaseHost(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	host := parsedURL.Hostname()
	etld, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", fmt.Errorf("failed to get eTLD+1: %w", err)
	}
	parts := strings.Split(etld, ".")
	if len(parts) == 0 {
		return "", errors.New("invalid domain structure")
	}
	return parts[0], nil
}

func StartDownloadsCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for {
			CleanupDownloadsDir()
			<-ticker.C
		}
	}()
}
