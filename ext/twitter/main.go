package twitter

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"govd/enums"
	"govd/logger"
	"govd/models"
	"govd/util"
	"govd/util/networking"

	"github.com/bytedance/sonic"
	"github.com/pkg/errors"
)

const (
	apiHostname = "x.com"
	apiBase     = "https://" + apiHostname + "/i/api/graphql/"
	apiEndpoint = apiBase + "2ICDjqPd81tulZcYrtpTuQ/TweetResultByRestId"
)

var ShortExtractor = &models.Extractor{
	Name:       "Twitter (Short)",
	CodeName:   "twitter",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://t\.co/(?P<id>\w+)`),
	Host:       []string{"t"},
	IsRedirect: true,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		client := networking.GetExtractorHTTPClient(ctx.Extractor)
		cookies := util.GetExtractorCookies(ctx.Extractor)
		resp, err := util.FetchPage(
			client,
			http.MethodGet,
			ctx.MatchedContentURL,
			nil,
			nil,
			cookies,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read body: %w", err)
		}
		matchedURL := Extractor.URLPattern.FindSubmatch(body)
		if matchedURL == nil {
			return nil, errors.New("failed to find url in body")
		}
		return &models.ExtractorResponse{
			URL: string(matchedURL[0]),
		}, nil
	},
}

var Extractor = &models.Extractor{
	Name:       "Twitter",
	CodeName:   "twitter",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?:\/\/(vx)?(twitter|x)\.com\/([^\/]+)\/status\/(?P<id>\d+)`),
	Host: []string{
		"x",
		"twitter",
		"vxtwitter",
	},

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		mediaList, err := MediaListFromAPI(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get media: %w", err)
		}
		return &models.ExtractorResponse{
			MediaList: mediaList,
		}, nil
	},
}

func MediaListFromAPI(ctx *models.DownloadContext) ([]*models.Media, error) {
	var mediaList []*models.Media
	tweetData, err := GetTweetAPI(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tweet data: %w", err)
	}

	caption := CleanCaption(tweetData.FullText)

	var mediaEntities []MediaEntity
	switch {
	case tweetData.ExtendedEntities != nil && len(tweetData.ExtendedEntities.Media) > 0:
		mediaEntities = tweetData.ExtendedEntities.Media
	case tweetData.Entities != nil && len(tweetData.Entities.Media) > 0:
		mediaEntities = tweetData.Entities.Media
	default:
		return nil, nil
	}

	for _, mediaEntity := range mediaEntities {
		media := ctx.Extractor.NewMedia(
			ctx.MatchedContentID,
			ctx.MatchedContentURL,
		)
		media.SetCaption(caption)

		switch mediaEntity.Type {
		case "video", "animated_gif":
			formats, err := ExtractVideoFormats(&mediaEntity)
			if err != nil {
				return nil, err
			}
			for _, format := range formats {
				media.AddFormat(format)
			}
		case "photo":
			media.AddFormat(&models.MediaFormat{
				Type:     enums.MediaTypePhoto,
				FormatID: "photo",
				URL:      []string{mediaEntity.MediaURLHTTPS},
			})
		}

		if len(media.Formats) > 0 {
			mediaList = append(mediaList, media)
		}
	}

	return mediaList, nil
}

func GetTweetAPI(ctx *models.DownloadContext) (*Tweet, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)
	tweetID := ctx.MatchedGroups["id"]
	if cookies == nil {
		return nil, errors.New("cookies not found")
	}
	headers := BuildAPIHeaders(cookies)
	if headers == nil {
		return nil, errors.New("failed to build headers. check cookies")
	}
	query := BuildAPIQuery(tweetID)

	reqURL := apiEndpoint + "?" + query
	resp, err := util.FetchPage(
		client,
		http.MethodGet,
		reqURL,
		nil,
		headers,
		cookies,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	// debugging
	logger.WriteFile("twitter_api_response", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response code: %s", resp.Status)
	}

	var apiResponse APIResponse
	err = sonic.ConfigFastest.NewDecoder(resp.Body).Decode(&apiResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := apiResponse.Data.TweetResult.Result
	if result == nil {
		return nil, errors.New("failed to get tweet result")
	}

	var tweet *Tweet
	switch {
	case result.Tweet != nil:
		tweet = result.Tweet
	case result.Legacy != nil:
		tweet = result.Legacy
	default:
		return nil, errors.New("failed to get tweet data")
	}
	return tweet, nil
}
