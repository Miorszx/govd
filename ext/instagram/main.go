package instagram

import (
	"fmt"
	"govd/enums"
	"govd/models"
	"govd/util"
	"io"
	"net/http"
	"regexp"
)

// as a public service, we can't use the official API
// so we use igram.world API, a third-party service
// that provides a similar functionality
// feel free to open PR, if you want to
// add support for the official Instagram API

const (
	apiHostname  = "api.igram.world"
	apiKey       = "aaeaf2805cea6abef3f9d2b6a666fce62fd9d612a43ab772bb50ce81455112e0"
	apiTimestamp = "1742201548873"

	// todo: Implement a proper way
	// to get the API key and timestamp
)

var instagramHost = []string{
	"instagram.com",
}

var Extractor = &models.Extractor{
	Name:       "Instagram",
	CodeName:   "instagram",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?instagram\.com\/(reel|p|tv)\/(?P<id>[a-zA-Z0-9_-]+)`),
	Host:       instagramHost,
	IsRedirect: false,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		mediaList, err := MediaListFromAPI(ctx, false)
		return &models.ExtractorResponse{
			MediaList: mediaList,
		}, err
	},
}

var StoriesExtractor = &models.Extractor{
	Name:       "Instagram Stories",
	CodeName:   "instagram_stories",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?instagram\.com\/stories\/[a-zA-Z0-9._]+\/(?P<id>\d+)`),
	Host:       instagramHost,
	IsRedirect: false,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		mediaList, err := MediaListFromAPI(ctx, true)
		return &models.ExtractorResponse{
			MediaList: mediaList,
		}, err
	},
}

var ShareURLExtractor = &models.Extractor{
	Name:       "Instagram Share URL",
	CodeName:   "instagram_share",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?:\/\/(www\.)?instagram\.com\/share\/((reels?|video|s|p)\/)?(?P<id>[^\/\?]+)`),
	Host:       instagramHost,
	IsRedirect: true,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		client := util.GetHTTPClient(ctx.Extractor.CodeName)
		redirectURL, err := util.GetLocationURL(
			client,
			ctx.MatchedContentURL,
			igHeaders,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get url location: %w", err)
		}
		return &models.ExtractorResponse{
			URL: redirectURL,
		}, nil
	},
}

func MediaListFromAPI(
	ctx *models.DownloadContext,
	stories bool,
) ([]*models.Media, error) {
	client := util.GetHTTPClient(ctx.Extractor.CodeName)

	var mediaList []*models.Media
	postURL := ctx.MatchedContentURL
	details, err := GetVideoAPI(client, postURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get post: %w", err)
	}
	var caption string
	if !stories {
		caption, err = GetPostCaption(client, postURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get caption: %w", err)
		}
	}
	for _, item := range details.Items {
		media := ctx.Extractor.NewMedia(
			ctx.MatchedContentID,
			ctx.MatchedContentURL,
		)
		media.SetCaption(caption)
		urlObj := item.URL[0]
		contentURL, err := GetCDNURL(urlObj.URL)
		if err != nil {
			return nil, err
		}
		thumbnailURL, err := GetCDNURL(item.Thumb)
		if err != nil {
			return nil, err
		}
		fileExt := urlObj.Ext
		formatID := urlObj.Type
		switch fileExt {
		case "mp4":
			media.AddFormat(&models.MediaFormat{
				Type:       enums.MediaTypeVideo,
				FormatID:   formatID,
				URL:        []string{contentURL},
				VideoCodec: enums.MediaCodecAVC,
				AudioCodec: enums.MediaCodecAAC,
				Thumbnail:  []string{thumbnailURL},
			},
			)
		case "jpg", "webp", "heic", "jpeg":
			media.AddFormat(&models.MediaFormat{
				Type:     enums.MediaTypePhoto,
				FormatID: formatID,
				URL:      []string{contentURL},
			})
		default:
			return nil, fmt.Errorf("unknown format: %s", fileExt)
		}
		mediaList = append(mediaList, media)
	}

	return mediaList, nil
}

func GetVideoAPI(
	client models.HTTPClient,
	contentURL string,
) (*IGramResponse, error) {
	apiURL := fmt.Sprintf(
		"https://%s/api/convert",
		apiHostname,
	)
	payload, err := BuildSignedPayload(contentURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build signed payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", util.ChromeUA)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get response: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	response, err := ParseIGramResponse(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return response, nil
}
