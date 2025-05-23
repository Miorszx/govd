package threads

import (
	"fmt"
	"govd/enums"
	"govd/logger"
	"govd/models"
	"govd/util"
	"govd/util/networking"
	"io"
	"net/http"
	"regexp"
)

var Extractor = &models.Extractor{
	Name:       "Threads",
	CodeName:   "threads",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?threads\.[^\/]+\/(?:(?:@[^\/]+)\/)?p(?:ost)?\/(?P<id>[a-zA-Z0-9_-]+)`),
	Host:       []string{"threads"},

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		mediaList, err := GetEmbedMediaList(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get media: %w", err)
		}
		return &models.ExtractorResponse{
			MediaList: mediaList,
		}, nil
	},
}

func GetEmbedMediaList(ctx *models.DownloadContext) ([]*models.Media, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)
	embedURL := fmt.Sprintf(
		"https://www.threads.net/@_/post/%s/embed",
		ctx.MatchedContentID,
	)

	resp, err := util.FetchPage(
		client,
		http.MethodGet,
		embedURL,
		nil,
		headers,
		cookies,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// debugging
	logger.WriteFile("threads_embed", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get embed media: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return ParseEmbedMedia(ctx, body)
}
