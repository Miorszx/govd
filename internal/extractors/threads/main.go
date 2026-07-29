package threads

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

var Extractor = &models.Extractor{
	ID:          "threads",
	DisplayName: "Threads",

	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?threads\.[^\/]+\/(?:(?:@[^\/]+)\/)?(?:(?:p(?:ost)?|t)\/(?P<id>[a-zA-Z0-9_-]+)|share\/(?P<shareid>[a-zA-Z0-9_-]+))`),
	Host:       []string{"threads"},

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		// resolve share URLs to real post ID
		if shareID, ok := ctx.MatchGroups["shareid"]; ok && shareID != "" {
			if err := resolveShareURL(ctx, shareID); err != nil {
				return nil, fmt.Errorf("failed to resolve share url: %w", err)
			}
		}
		media, err := GetPostMedia(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get media: %w", err)
		}
		return &models.ExtractorResponse{Media: media}, nil
	},
}

func resolveShareURL(ctx *models.ExtractorContext, shareID string) error {
	shareURL := fmt.Sprintf("https://www.threads.com/share/%s/?__a=1", shareID)
	// use plain http.Client — govd's custom client triggers Threads rate limit
	plainClient := &http.Client{Timeout: 15 * 1000000000} // 15s in nanoseconds
	req, err := http.NewRequestWithContext(ctx.Context, http.MethodGet, shareURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15")
	resp, err := plainClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch share url: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read share response: %w", err)
	}
	ctx.Debugf("share response: %.200s", string(body))
	// JSON: for (;;);{"redirect":"https:\/\/www.threads.com\/\u0040user\/post\/POST_ID?..."}
	re := regexp.MustCompile(`\\/post\\/([a-zA-Z0-9_-]+)`)
	m := re.FindSubmatch(body)
	if len(m) < 2 {
		return fmt.Errorf("no redirect found in share response: %.100s", string(body))
	}
	ctx.ContentID = string(m[1])
	ctx.Infof("resolved share %s -> post %s", shareID, ctx.ContentID)
	return nil
}

func GetPostMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	// try to find the post URL from the page (need @username/post/ID)
	// first try embed as fallback for simple posts, then try page fetch
	postURL := fmt.Sprintf("https://www.threads.com/@_/post/%s", ctx.ContentID)
	cookies := util.GetExtractorCookies(ctx.Extractor.ID)
	resp, err := ctx.Fetch(
		http.MethodGet,
		postURL,
		&networking.RequestParams{
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
				"Accept":     "text/html,application/xhtml+xml",
			},
			Cookies: cookies,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.WriteFile("threads_post", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get post page: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return ParsePostMedia(ctx, body)
}
