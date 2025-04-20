package redgifs

import (
	"fmt"
	"govd/models"
	"govd/util"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
)

var accessToken *Token

func GetAccessToken(ctx *models.DownloadContext) (*Token, error) {
	if accessToken == nil || time.Now().Unix() >= accessToken.ExpiresIn {
		if err := RefreshAccessToken(ctx); err != nil {
			return nil, err
		}
	}
	return accessToken, nil
}

func RefreshAccessToken(ctx *models.DownloadContext) error {
	req, err := http.NewRequest(http.MethodGet, tokenEndpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", util.ChromeUA)
	res, err := ctx.Extractor.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get access token: %s", res.Status)
	}
	var token Token
	err = sonic.ConfigFastest.NewDecoder(res.Body).Decode(&token)
	if err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	token.ExpiresIn = time.Now().Add(23 * time.Hour).Unix()
	accessToken = &token
	return nil
}
