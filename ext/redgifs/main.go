package redgifs

import (
	"fmt"
	"govd/enums"
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

var (
	baseApiHeaders = map[string]string{
		"referer":      "https://www.redgifs.com/",
		"origin":       "https://www.redgifs.com",
		"content-type": "application/json",
	}
)

var Extractor = &models.Extractor{
	Name:       "RedGifs",
	CodeName:   "redgifs",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://(?:(?:www\.)?redgifs\.com/(?:watch|ifr)/|thumbs2\.redgifs\.com/)(?P<id>[^-/?#\.]+)`),
	Host: []string{
		"redgifs.com",
		"thumbs2.redgifs.com",
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
	client := util.GetHTTPClient(ctx.Extractor.CodeName)

	response, err := GetVideo(
		client, ctx.MatchedContentID)
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

	if len(media.Formats) > 0 {
		mediaList = append(mediaList, media)
	}

	return mediaList, nil
}

func GetVideo(
	client models.HTTPClient,
	videoID string,
) (*Response, error) {
	url := videoEndpoint + videoID + "?views=true"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	token, err := GetAccessToken(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+token.AccessToken)
	req.Header.Set("user-agent", token.Agent)
	req.Header.Set("x-customheader", "https://www.redgifs.com/watch/"+videoID)
	for k, v := range baseApiHeaders {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get video: %s", res.Status)
	}
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
