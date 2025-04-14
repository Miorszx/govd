package pinterest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"govd/enums"
	"govd/models"
	"govd/util"
)

const (
	pinResourceEndpoint = "https://www.pinterest.com/resource/PinResource/get/"
	shortenerAPIFormat  = "https://api.pinterest.com/url_shortener/%s/redirect/"
)

var ShortExtractor = &models.Extractor{
	Name:       "Pinterest (Short)",
	CodeName:   "pinterest:short",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://(\w+\.)?pin\.\w+/(?P<id>\w+)`),
	IsRedirect: true,

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		shortURL := fmt.Sprintf(shortenerAPIFormat, ctx.MatchedContentID)
		location, err := util.GetLocationURL(shortURL, "")
		if err != nil {
			return nil, fmt.Errorf("failed to get real url: %w", err)
		}
		return &models.ExtractorResponse{
			URL: location,
		}, nil
	},
}

var Extractor = &models.Extractor{
	Name:       "Pinterest",
	CodeName:   "pinterest",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://(\w+\.)?pinterest[\.\w]+/pin/(?P<id>\d+)`),

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		media, err := ExtractPinMedia(ctx)
		if err != nil {
			return nil, err
		}
		return &models.ExtractorResponse{
			MediaList: media,
		}, nil
	},
}

func ExtractPinMedia(ctx *models.DownloadContext) ([]*models.Media, error) {
	pinID := ctx.MatchedContentID
	contentURL := ctx.MatchedContentURL

	pinData, err := GetPinData(pinID)
	if err != nil {
		return nil, err
	}

	media := ctx.Extractor.NewMedia(pinID, contentURL)
	media.SetCaption(pinData.Title)

	if pinData.Videos != nil && pinData.Videos.VideoList != nil {
		formats, err := ParseVideoObject(pinData.Videos)
		if err != nil {
			return nil, err
		}
		for _, format := range formats {
			media.AddFormat(format)
		}
		return []*models.Media{media}, nil
	}

	if pinData.StoryPinData != nil && len(pinData.StoryPinData.Pages) > 0 {
		for _, page := range pinData.StoryPinData.Pages {
			for _, block := range page.Blocks {
				if block.BlockType == 3 && block.Video != nil { // blockType 3 = Video
					formats, err := ParseVideoObject(block.Video)
					if err != nil {
						return nil, err
					}
					for _, format := range formats {
						media.AddFormat(format)
					}
					return []*models.Media{media}, nil
				}
			}
		}
	}

	if pinData.Images != nil && pinData.Images.Orig != nil {
		imageURL := pinData.Images.Orig.URL
		media.AddFormat(&models.MediaFormat{
			FormatID: "photo",
			Type:     enums.MediaTypePhoto,
			URL:      []string{imageURL},
		})
		return []*models.Media{media}, nil
	} else if pinData.StoryPinData != nil && len(pinData.StoryPinData.Pages) > 0 {
		for _, page := range pinData.StoryPinData.Pages {
			if page.Image != nil && page.Image.Images.Originals != nil {
				media.AddFormat(&models.MediaFormat{
					FormatID: "photo",
					Type:     enums.MediaTypePhoto,
					URL:      []string{page.Image.Images.Originals.URL},
				})
				return []*models.Media{media}, nil
			}
		}
	}

	if pinData.Embed != nil && pinData.Embed.Type == "gif" {
		media.AddFormat(&models.MediaFormat{
			FormatID:   "gif",
			Type:       enums.MediaTypeVideo,
			VideoCodec: enums.MediaCodecAVC,
			URL:        []string{pinData.Embed.Src},
		})
		return []*models.Media{media}, nil
	}

	return nil, fmt.Errorf("no media found for pin ID: %s", pinID)
}

func GetPinData(pinID string) (*PinData, error) {
	params := BuildPinRequestParams(pinID)

	req, err := http.NewRequest("GET", pinResourceEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	q := req.URL.Query()
	for key, value := range params {
		q.Add(key, value)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", util.ChromeUA)

	// fix 403 error
	req.Header.Set("X-Pinterest-PWS-Handler", "www/[username].js")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad response: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var pinResponse PinResponse
	err = json.Unmarshal(body, &pinResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pinResponse.ResourceResponse.Data, nil
}
