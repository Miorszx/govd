package threads

import (
	"fmt"
	"govd/enums"
	"govd/models"
	"govd/util"
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
	IsRedirect: false,

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
	session := util.GetHTTPClient(ctx.Extractor.CodeName)
	embedURL := fmt.Sprintf(
		"https://www.threads.net/@_/post/%s/embed",
		ctx.MatchedContentID,
	)
	req, err := http.NewRequest(
		http.MethodGet,
		embedURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := session.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get embed media: %s", res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return ParseEmbedMedia(ctx, body)
}
