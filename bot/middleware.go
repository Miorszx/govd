package bot

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type BotClient struct {
	gotgbot.BotClient
}

func (b BotClient) RequestWithContext(
	ctx context.Context,
	token string,
	method string,
	params map[string]string,
	data map[string]gotgbot.FileReader,
	opts *gotgbot.RequestOpts,
) (json.RawMessage, error) {
	if strings.HasPrefix(method, "send") || method == "copyMessage" {
		params["allow_sending_without_reply"] = "true"
	}
	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") {
		params["parse_mode"] = "HTML"
	}
	val, err := b.BotClient.RequestWithContext(ctx, token, method, params, data, opts)
	if err != nil {
		return nil, err
	}
	return val, err
}

func NewBotClient() BotClient {
	botAPIURL := os.Getenv("BOT_API_URL")
	if botAPIURL == "" {
		log.Println("BOT_API_URL is not provided, using default")
		botAPIURL = gotgbot.DefaultAPIURL
	}
	return BotClient{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Transport: &http.Transport{
					// avoid using proxy for telegram
					Proxy: func(r *http.Request) (*url.URL, error) {
						return nil, nil
					},
				},
			},
			UseTestEnvironment: false,
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Minute,
				APIURL:  botAPIURL,
			},
		},
	}
}
