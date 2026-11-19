package tiktok

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

var VMExtractor = &models.Extractor{
	ID:          "tiktok",
	DisplayName: "TikTok VM",

	URLPattern: regexp.MustCompile(`https:\/\/((?:vm|vt|www)\.)?(vx)?tiktok\.com\/(?:t\/)?(?P<id>[a-zA-Z0-9-]+)`),
	Host:       []string{"tiktok", "vxtiktok"},
	Redirect:   true,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		redirectURL, err := ctx.FetchLocation(ctx.ContentURL, nil)
		if err != nil {
			return nil, err
		}
		parsedURL, err := url.Parse(redirectURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redirect url: %w", err)
		}

		if parsedURL.Path == "/login" {
			ctx.Debugf("tiktok is geo restricted in your region, attemping bypass...")
			realURL := parsedURL.Query().Get("redirect_url")
			if realURL == "" {
				return nil, util.ErrGeoRestrictedContent
			}
			return &models.ExtractorResponse{
				URL: realURL,
			}, nil
		}
		return &models.ExtractorResponse{
			URL: redirectURL,
		}, nil
	},
}

var Extractor = &models.Extractor{
	ID:          "tiktok",
	DisplayName: "TikTok",

	URLPattern: regexp.MustCompile(`https?:\/\/((www|m)\.)?(vx)?tiktok\.com\/((?:embed|@[\w\.-]*)\/)?(v(ideo)?|p(hoto)?)\/(?P<id>[0-9]+)`),
	Host:       []string{"tiktok", "vxtiktok"},

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := GetMedia(ctx)
		if err != nil {
			return nil, err
		}
		return &models.ExtractorResponse{
			URL:   ctx.ContentURL,
			Media: media,
		}, nil
	},
}

func GetMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	var details *WebItemStruct
	var cookies []*http.Cookie
	var err error

	// sometimes web page just returns a
	// login page, so we need to retry
	// a few times to get the correct page
	for range 5 {
		details, cookies, err = GetVideoWeb(ctx)
		if err == nil {
			return mediaFromWeb(ctx, details, cookies)
		}
		if !shouldRetryVideoWeb(err) {
			break
		}
	}
	return nil, err
}

func shouldRetryVideoWeb(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, util.ErrAuthenticationNeeded):
		return false
	case errors.Is(err, util.ErrTikTokIPBlocked):
		return false
	case errors.Is(err, util.ErrUnavailable):
		return false
	default:
		return true
	}
}

func mediaFromWeb(
	ctx *models.ExtractorContext,
	details *WebItemStruct,
	cookies []*http.Cookie,
) (*models.Media, error) {
	media := ctx.NewMedia()
	media.SetCaption(details.Desc)

	isImageSlide := details.ImagePost != nil
	if !isImageSlide {
		item := media.NewItem()
		video := details.Video
		if video == nil {
			return nil, util.ErrUnavailable
		}

		// Enumerate the whole quality ladder first. PlayAddrStruct is only the
		// default gear, so relying on it alone caps every download at whatever
		// TikTok felt like serving the crawler (typically 540p or 720p) even
		// when a 1080p rendition exists in bitrateInfo.
		seen := make(map[string]bool)
		for _, rung := range video.BitrateInfo {
			if rung == nil || rung.PlayAddr == nil || len(rung.PlayAddr.URLList) == 0 {
				continue
			}
			key := rung.PlayAddr.URI
			if key == "" {
				key = rung.PlayAddr.URLList[0]
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			item.AddFormats(&models.MediaFormat{
				Type:       database.MediaTypeVideo,
				FormatID:   key,
				URL:        rung.PlayAddr.URLList,
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
				FileSize:   rung.PlayAddr.SizeBytes(),
				Width:      rung.PlayAddr.Width,
				Height:     rung.PlayAddr.Height,
				Bitrate:    rung.Bitrate,
				Duration:   video.Duration,
				DownloadSettings: &models.DownloadSettings{
					// avoid 403 error for videos
					Cookies: cookies,
				},
			})
		}

		// Fall back to the default gear so a page without bitrateInfo still
		// resolves to something playable.
		if video.PlayAddr != nil && len(video.PlayAddr.URLList) > 0 {
			key := video.PlayAddr.URI
			if key == "" {
				key = video.PlayAddr.URLList[0]
			}
			if !seen[key] {
				item.AddFormats(&models.MediaFormat{
					Type:       database.MediaTypeVideo,
					FormatID:   key,
					URL:        video.PlayAddr.URLList,
					VideoCodec: database.MediaCodecAvc,
					AudioCodec: database.MediaCodecAac,
					FileSize:   video.PlayAddr.SizeBytes(),
					Width:      video.PlayAddr.Width,
					Height:     video.PlayAddr.Height,
					Duration:   video.Duration,
					DownloadSettings: &models.DownloadSettings{
						Cookies: cookies,
					},
				})
			}
		}

		if len(item.Formats) == 0 {
			return nil, util.ErrUnavailable
		}
		return media, nil
	}

	images := details.ImagePost.Images
	for _, image := range images {
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			Type:     database.MediaTypePhoto,
			FormatID: "image",
			URL:      image.URL.URLList,
		})
	}
	return media, nil
}
