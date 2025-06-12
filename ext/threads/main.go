package threads

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/govdbot/govd/enums"
	"github.com/govdbot/govd/logger"
	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util"
	"github.com/govdbot/govd/util/networking"
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
		if err == nil {
			return &models.ExtractorResponse{
				MediaList: mediaList,
			}, nil
		}
		mediaList, err = GetPageMediaList(ctx)
		if err == nil {
			return &models.ExtractorResponse{
				MediaList: mediaList,
			}, nil
		}
		return nil, fmt.Errorf("failed to get media list: %w", err)
	},
}

func GetEmbedMediaList(ctx *models.DownloadContext) ([]*models.Media, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)
	embedURL := fmt.Sprintf(
		embedBase,
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
	mediaList, err := ParseEmbedMedia(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse embed media: %w", err)
	}
	if len(mediaList) == 0 {
		return nil, fmt.Errorf("no media found in embed")
	}
	return mediaList, nil
}

func GetPageMediaList(ctx *models.DownloadContext) ([]*models.Media, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)
	embedURL := fmt.Sprintf(
		pageBase,
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
	logger.WriteFile("threads_page", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get page media: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	relayData, err := FindPostRelayData(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("failed to find post relay data: %w", err)
	}

	// debugging
	logger.WriteFile("threads_relay_data", relayData)

	data, err := ParsePostRelayData(ctx, relayData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse post relay data: %w", err)
	}
	return data, nil
}
