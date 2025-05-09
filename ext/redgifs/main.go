package redgifs

import (
	"fmt"
	"govd/enums"
	"govd/logger"
	"govd/models"
	"govd/util"
	"net/http"
	"regexp"

	"github.com/bytedance/sonic"
)

const (
	baseAPI       = "https://api.redgifs.com/v2/"
	tokenEndpoint = baseAPI + "auth/temporary"
	videoEndpoint = baseAPI + "gifs/"
)

var baseAPIHeaders = map[string]string{
	"Referer":      "https://www.redgifs.com/",
	"Origin":       "https://www.redgifs.com",
	"Content-Type": "application/json",
}

var Extractor = &models.Extractor{
	Name:       "RedGifs",
	CodeName:   "redgifs",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://(?:(?:www\.)?redgifs\.com/(?:watch|ifr)/|thumbs2\.redgifs\.com/)(?P<id>[^-/?#\.]+)`),
	Host:       []string{"redgifs"},

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
	session := util.GetHTTPClient(ctx.Extractor.CodeName)

	response, err := GetVideo(
		session,
		ctx.MatchedContentID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get from api: %w", err)
	}
	gif := response.Gif
	media := ctx.Extractor.NewMedia(
		ctx.MatchedContentID,
		ctx.MatchedContentURL,
	)

	if gif.Description != "" {
		media.SetCaption(gif.Description)
	}
	media.NSFW = true // always nsfw

	if gif.Urls.Sd != "" {
		format := &models.MediaFormat{
			FormatID:   "sd",
			Type:       enums.MediaTypeVideo,
			URL:        []string{gif.Urls.Sd},
			VideoCodec: enums.MediaCodecAVC,
			Width:      int64(gif.Width / 2),
			Height:     int64(gif.Height / 2),
		}
		if gif.HasAudio {
			format.AudioCodec = enums.MediaCodecAAC
		}
		media.AddFormat(format)
	}

	if gif.Urls.Hd != "" {
		format := &models.MediaFormat{
			FormatID:   "hd",
			Type:       enums.MediaTypeVideo,
			URL:        []string{gif.Urls.Hd},
			VideoCodec: enums.MediaCodecAVC,
			Width:      int64(gif.Width),
			Height:     int64(gif.Height),
		}
		if gif.HasAudio {
			format.AudioCodec = enums.MediaCodecAAC
		}
		media.AddFormat(format)
	}

	if gif.Urls.Poster != "" {
		thumbnails := []string{gif.Urls.Poster}
		if gif.Urls.Thumbnail != "" {
			thumbnails = append(thumbnails, gif.Urls.Thumbnail)
		}

		for _, format := range media.Formats {
			format.Thumbnail = thumbnails
			format.Duration = int64(gif.Duration)
		}
	}

	return []*models.Media{media}, nil
}

func GetVideo(
	session models.HTTPClient,
	videoID string,
) (*Response, error) {
	url := videoEndpoint + videoID + "?views=true"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	token, err := GetAccessToken(session)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("User-Agent", token.Agent)
	req.Header.Set("X-Customheader", "https://www.redgifs.com/watch/"+videoID)
	for k, v := range baseAPIHeaders {
		req.Header.Set(k, v)
	}
	res, err := session.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get video: %s", res.Status)
	}

	// debugging
	logger.WriteFile("redgifs_api_response", res)

	var response Response
	err = sonic.ConfigFastest.NewDecoder(res.Body).Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if response.Gif == nil {
		return nil, fmt.Errorf("failed to get video: %s", res.Status)
	}
	return &response, nil
}
