package tiktok

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"govd/enums"
	"govd/models"
	"govd/util"

	"github.com/bytedance/sonic"
	"github.com/pkg/errors"
)

const (
	apiHostname        = "api16-normal-c-useast1a.tiktokv.com"
	installationID     = "7177010410163668742"
	appName            = "musical_ly"
	appID              = "1233"
	appVersion         = "39.8.2"
	manifestAppVersion = "2023508030"
	packageID          = "com.zhiliaoapp.musically/" + manifestAppVersion
	webBase            = "https://www.tiktok.com/@_/video/%s"
	appUserAgent       = packageID + " (Linux; U; Android 13; en_US; Pixel 7; Build/TD1A.220804.031; Cronet/58.0.2991.0)"
)

var baseHost = []string{
	"tiktok",
	"vxtiktok",
}

var VMExtractor = &models.Extractor{
	Name:       "TikTok VM",
	CodeName:   "tiktokvm",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https:\/\/((?:vm|vt|www)\.)?(vx)?tiktok\.com\/(?:t\/)?(?P<id>[a-zA-Z0-9]+)`),
	Host:       baseHost,
	IsRedirect: true,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		client := util.GetHTTPClient(ctx.Extractor.CodeName)
		redirectURL, err := util.GetLocationURL(client, ctx.MatchedContentURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get url location: %w", err)
		}
		return &models.ExtractorResponse{
			URL: redirectURL,
		}, nil
	},
}

var Extractor = &models.Extractor{
	Name:       "TikTok",
	CodeName:   "tiktok",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?:\/\/((www|m)\.)?(vx)?tiktok\.com\/((?:embed|@[\w\.-]+)\/)?(v(ideo)?|p(hoto)?)\/(?P<id>[0-9]+)`),
	Host:       baseHost,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		// method 1: get media from webpage
		mediaList, err := MediaListFromWeb(ctx)
		if err == nil {
			return &models.ExtractorResponse{
				MediaList: mediaList,
			}, nil
		}
		// method 2: get media from api
		mediaList, err = MediaListFromAPI(ctx)
		if err == nil {
			return &models.ExtractorResponse{
				MediaList: mediaList,
			}, nil
		}
		return nil, errors.New("failed to extract media: all methods failed")
	},
}

func MediaListFromAPI(ctx *models.DownloadContext) ([]*models.Media, error) {
	client := util.GetHTTPClient(ctx.Extractor.CodeName)
	details, err := GetVideoAPI(
		client, ctx.MatchedContentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get from api: %w", err)
	}
	caption := details.Desc
	isImageSlide := details.ImagePostInfo != nil
	if !isImageSlide {
		media := ctx.Extractor.NewMedia(
			ctx.MatchedContentID,
			ctx.MatchedContentURL,
		)
		media.SetCaption(caption)
		video := details.Video

		// generic PlayAddr
		if video.PlayAddr != nil {
			format, err := ParsePlayAddr(video, video.PlayAddr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse playaddr: %w", err)
			}
			media.AddFormat(format)
		}
		// hevc PlayAddr
		if video.PlayAddrBytevc1 != nil {
			format, err := ParsePlayAddr(video, video.PlayAddrBytevc1)
			if err != nil {
				return nil, fmt.Errorf("failed to parse playaddr: %w", err)
			}
			media.AddFormat(format)
		}
		// h264 PlayAddr
		if video.PlayAddrH264 != nil {
			format, err := ParsePlayAddr(video, video.PlayAddrH264)
			if err != nil {
				return nil, fmt.Errorf("failed to parse playaddr: %w", err)
			}
			media.AddFormat(format)
		}
		return []*models.Media{media}, nil
	} else {
		images := details.ImagePostInfo.Images
		mediaList := make([]*models.Media, 0, len(images))
		for i := range images {
			image := images[i]
			media := ctx.Extractor.NewMedia(
				ctx.MatchedContentID,
				ctx.MatchedContentURL,
			)
			media.SetCaption(caption)
			media.AddFormat(&models.MediaFormat{
				FormatID: "image",
				Type:     enums.MediaTypePhoto,
				URL:      image.DisplayImage.URLList,
			})
			mediaList = append(mediaList, media)
		}
		return mediaList, nil
	}
}

func MediaListFromWeb(ctx *models.DownloadContext) ([]*models.Media, error) {
	client := util.GetHTTPClient(ctx.Extractor.CodeName)

	var details *WebItemStruct
	var cookies []*http.Cookie
	var err error

	// sometimes web page just returns a
	// login page, so we need to retry
	// a few times to get the correct page
	for range 5 {
		details, cookies, err = GetVideoWeb(
			client, ctx.MatchedContentID)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get from web: %w", err)
	}

	caption := details.Desc

	isImageSlide := details.ImagePost != nil
	if !isImageSlide {
		media := ctx.Extractor.NewMedia(
			ctx.MatchedContentID,
			ctx.MatchedContentURL,
		)
		media.SetCaption(caption)
		video := details.Video
		if video.PlayAddr != "" {
			media.AddFormat(&models.MediaFormat{
				Type:       enums.MediaTypeVideo,
				FormatID:   "video",
				URL:        []string{video.PlayAddr},
				VideoCodec: enums.MediaCodecAVC,
				AudioCodec: enums.MediaCodecAAC,
				Width:      video.Width,
				Height:     video.Height,
				Duration:   video.Duration,
				DownloadConfig: &models.DownloadConfig{
					// avoid 403 error for videos
					Cookies: cookies,
				},
			})
		}
		return []*models.Media{media}, nil
	} else {
		images := details.ImagePost.Images
		mediaList := make([]*models.Media, 0, len(images))
		for i := range images {
			image := images[i]
			media := ctx.Extractor.NewMedia(
				ctx.MatchedContentID,
				ctx.MatchedContentURL,
			)
			media.SetCaption(caption)
			media.AddFormat(&models.MediaFormat{
				Type:     enums.MediaTypePhoto,
				FormatID: "image",
				URL:      image.URL.URLList,
			})
			mediaList = append(mediaList, media)
		}
		return mediaList, nil
	}
}

func GetVideoWeb(
	client models.HTTPClient,
	awemeID string,
) (*WebItemStruct, []*http.Cookie, error) {
	url := fmt.Sprintf(webBase, awemeID)
	req, err := http.NewRequest(
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", appUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	itemStruct, err := ParseUniversalData(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse universal data: %w", err)
	}
	return itemStruct, resp.Cookies(), nil
}

func GetVideoAPI(
	client models.HTTPClient,
	awemeID string,
) (*AwemeDetails, error) {
	apiURL := fmt.Sprintf(
		"https://%s/aweme/v1/multi/aweme/detail/",
		apiHostname,
	)
	queryParams, err := BuildAPIQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to build api query: %w", err)
	}
	postData := BuildPostData(awemeID)

	req, err := http.NewRequest(
		http.MethodPost,
		apiURL,
		postData,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.URL.RawQuery = queryParams.Encode()
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Argus", "")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var data *Response
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	err = decoder.Decode(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	videoData, err := FindVideoData(data, awemeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find video data: %w", err)
	}
	return videoData, nil
}
