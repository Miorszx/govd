package youtube

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"govd/config"
	"govd/enums"
	"govd/logger"
	"govd/models"
	"govd/util"
	"govd/util/networking"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

var Extractor = &models.Extractor{
	Name:       "YouTube",
	CodeName:   "youtube",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategoryStreaming,
	URLPattern: regexp.MustCompile(`(?:https?:)?(?:\/\/)?(?:(?:www|m)\.)?(?:youtube(?:-nocookie)?\.com\/(?:(?:watch\?(?:.*&)?v=)|(?:embed\/)|(?:v\/)|(?:shorts\/))|youtu\.be\/)(?P<id>[\w-]{11})(?:[?&].*)?`),
	Host: []string{
		"youtube",
		"youtu",
		"youtube-nocookie",
	},

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		video, err := GetVideoFromInv(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get media: %w", err)
		}
		return &models.ExtractorResponse{
			MediaList: []*models.Media{video},
		}, nil
	},
}

func GetVideoFromInv(ctx *models.DownloadContext) (*models.Media, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)

	cfg := config.GetExtractorConfig(ctx.Extractor)
	if cfg == nil {
		return nil, fmt.Errorf("youtube extractor is not configured")
	}
	instance, err := GetInvInstance(cfg)
	if err != nil {
		return nil, err
	}

	videoID := ctx.MatchedContentID
	videoURL := ctx.MatchedContentURL
	media := ctx.Extractor.NewMedia(
		videoID,
		videoURL,
	)

	reqURL := instance +
		invEndpoint +
		videoID +
		"?local=true" // proxied CDN

	zap.S().Debugf("proxied invidious api: %s", reqURL)

	resp, err := util.FetchPage(
		client,
		http.MethodGet,
		reqURL,
		nil,
		nil,
		cookies,
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// debugging
	logger.WriteFile("inv_youtube_response", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response status: %s", resp.Status)
	}

	var data *InvResponse
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	err = decoder.Decode(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	formats := ParseInvFormats(data)
	if len(formats) == 0 {
		return nil, errors.New("no valid formats found")
	}
	media.SetCaption(data.Title)
	for _, format := range formats {
		media.AddFormat(format)
	}
	return media, nil
}
