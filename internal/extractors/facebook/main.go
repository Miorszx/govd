package facebook

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

var facebookHost = []string{"facebook"}

var ShareExtractor = &models.Extractor{
	ID:          "facebook",
	DisplayName: "Facebook (Share)",

	URLPattern: regexp.MustCompile(`https?://(?:(?:www|m)\.)?facebook\.com/share/(?:(?:r|v|p)/)?(?P<id>[a-zA-Z0-9]+)`),
	Host:       facebookHost,

	Redirect: true,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		var lastErr error

		// For bare share (no r|v|p prefix), try mbasic/share/{id}/ iPhone first
		// This gives photo albums with multiple images + caption directly
		isBareShare := true
		for _, p := range []string{"/share/r/", "/share/v/", "/share/p/"} {
			if bytes.Contains([]byte(ctx.ContentURL), []byte(p)) {
				isBareShare = false
				break
			}
		}
		if isBareShare {
			media, err := tryMbasicShareAlbum(ctx)
			if err == nil && media != nil && len(media.Items) > 0 {
				return &models.ExtractorResponse{Media: media}, nil
			}
		}

		for attempt := 1; attempt <= 3; attempt++ {
			finalURL, err := ctx.FetchLocation(
				ctx.ContentURL,
				&networking.RequestParams{Headers: webHeaders},
			)
			if err == nil && finalURL != "" && finalURL != ctx.ContentURL {
				return &models.ExtractorResponse{URL: finalURL}, nil
			}
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("empty redirect location")
			}
			if attempt < 3 {
				time.Sleep(time.Duration(attempt*500) * time.Millisecond)
			}
		}
		// Fallback: fetch body (some share/v return 200 with og:url meta)
		resp, err := ctx.Fetch(
			"GET",
			ctx.ContentURL,
			&networking.RequestParams{Headers: webHeaders},
		)
		if err == nil {
			defer resp.Body.Close()
			// Use final URL from response request if redirected via http client
			if resp.Request != nil && resp.Request.URL.String() != ctx.ContentURL {
				return &models.ExtractorResponse{URL: resp.Request.URL.String()}, nil
			}
			// Try parse og:url from body - desktop UA often returns 400 for share/p, try iPhone UA
			bodyAll, _ := io.ReadAll(resp.Body)
			// If body doesn't contain S:_I or post_id (photo post), try iPhone UA which returns 47KB with data
			if !bytes.Contains(bodyAll, []byte("post_id")) && !bytes.Contains(bodyAll, []byte("S:_I")) {
				// try iPhone UA for share/p photo posts
				resp2, err2 := ctx.Fetch(
					http.MethodGet,
					ctx.ContentURL,
					&networking.RequestParams{
						Headers: map[string]string{
							"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
						},
					},
				)
				if err2 == nil {
					bodyAll2, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					if len(bodyAll2) > len(bodyAll) {
						bodyAll = bodyAll2
					}
				}
				// For group photo posts like 19Fea5TgkK/, www/share/p iPhone gives 47418 no og:url, but mbasic/share/p iPhone gives 46838 with og:url groups/2807075776107813/posts/3505374679611249/ + og:image single
				if !bytes.Contains(bodyAll, []byte("post_id")) && !bytes.Contains(bodyAll, []byte("S:_I")) {
					resp3, err3 := ctx.Fetch(
						http.MethodGet,
						fmt.Sprintf("https://mbasic.facebook.com/share/p/%s/", ctx.ContentID),
						&networking.RequestParams{
							Headers: map[string]string{
								"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
							},
						},
					)
					if err3 == nil {
						bodyAll3, _ := io.ReadAll(resp3.Body)
						resp3.Body.Close()
						if len(bodyAll3) > len(bodyAll) {
							bodyAll = bodyAll3
						} else if len(bodyAll3) > 1000 {
							// mbasic iPhone 46838 has og:url even if smaller than www 47412
							bodyAll = bodyAll3
						}
					}
				}
			}
			if len(bodyAll) > 50000 {
				bodyAll = bodyAll[:50000]
			}
			body := bodyAll
			if len(body) > 0 {
				if m := regexp.MustCompile(`property=["']og:url["']\s+content=["']([^"']+)["']`).FindSubmatch(body); len(m) == 2 {
					return &models.ExtractorResponse{URL: string(m[1])}, nil
				}
				if m := regexp.MustCompile(`content=["']([^"']+)["']\s+property=["']og:url["']`).FindSubmatch(body); len(m) == 2 {
					return &models.ExtractorResponse{URL: string(m[1])}, nil
				}
				// For share/p which is story.php, FB embeds post_id / story_fbid in JSON
				// S:_I{page_id}:{post_id}: gives both for photo posts like 1FtTAuWcPo
				if m := regexp.MustCompile(`S:_I(\d+):(\d+):`).FindSubmatch(body); len(m) == 3 {
					pageID := string(m[1])
					postID := string(m[2])
					return &models.ExtractorResponse{URL: "https://www.facebook.com/story.php?story_fbid=" + postID + "&id=" + pageID}, nil
				}
				if m := regexp.MustCompile(`"post_id"\s*:\s*"?(\d+)"?`).FindSubmatch(body); len(m) == 2 {
					return &models.ExtractorResponse{URL: "https://www.facebook.com/story.php?story_fbid=" + string(m[1])}, nil
				}
				if m := regexp.MustCompile(`"story_fbid"\s*:\s*"?(\d+)"?`).FindSubmatch(body); len(m) == 2 {
					return &models.ExtractorResponse{URL: "https://www.facebook.com/story.php?story_fbid=" + string(m[1])}, nil
				}
				if m := regexp.MustCompile(`"top_level_post_id"\s*:\s*"?(\d+)"?`).FindSubmatch(body); len(m) == 2 {
					return &models.ExtractorResponse{URL: "https://www.facebook.com/story.php?story_fbid=" + string(m[1])}, nil
				}
			}
		}
		return nil, fmt.Errorf("failed to follow share redirect: %w", lastErr)
	},
}

var Extractor = &models.Extractor{
	ID:          "facebook",
	DisplayName: "Facebook",

	URLPattern: regexp.MustCompile(
		`https?://(?:(?:www|m|mbasic)\.)?facebook\.com/` +
			`(?:watch/?\?(?:[^&]*&)*v=|(?:reel|videos?|posts?|permalink)/|groups/[^/]+/(?:permalink|posts|videos|reels?)/|[^/]+/(?:videos|posts|reels?)/|story\.php\?.*?(?:story_fbid|fbid)=)` +
			`(?P<id>[a-zA-Z0-9]+)`,
	),
	Host: facebookHost,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := GetMedia(ctx)
		if err != nil {
			return nil, err
		}
		return &models.ExtractorResponse{
			Media: media,
		}, nil
	},
}

func GetMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	if ctx.HTTPClient.Cookies == nil {
		return nil, fmt.Errorf("auth cookies are required for facebook")
	}
	videoData, err := GetVideoData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get video data: %w", err)
	}
	return buildMedia(ctx, videoData)
}

// Bitrate hints used to keep the format selector deterministic. The
// Facebook page does not expose real bitrates, but we still need to
// give the sort comparator (highest bitrate wins) a stable signal so
// HD ranks above SD when both fit.
const (
	hdBitrateHint int64 = 4_500_000
	sdBitrateHint int64 = 1_500_000
)

func buildMedia(ctx *models.ExtractorContext, data *VideoData) (*models.Media, error) {
	media := ctx.NewMedia()
	if data.Title != "" {
		media.SetCaption(data.Title)
	}

	item := media.NewItem()

	hdSize := probeFormatSize(ctx, data.HDURL)
	sdSize := probeFormatSize(ctx, data.SDURL)
	tgLimit := util.TelegramBotAPIFileLimit()
	// Telegram's 50 MiB limit is strict; the multipart overhead + slight
	// size variance between HEAD probe and actual download often makes a
	// file that probes as 52.3 MiB (just under 50 MiB) still get rejected
	// with 413 Request Entity Too Large (FD4902CC). Use 95% as safe limit
	// so near-limit files fall back to SD automatically.
	safeLimit := tgLimit * 95 / 100

	hdFitsSafe := data.HDURL != "" && (hdSize == 0 || hdSize <= safeLimit)
	sdFitsSafe := data.SDURL != "" && (sdSize == 0 || sdSize <= safeLimit)
	hdFits := data.HDURL != "" && (hdSize == 0 || hdSize <= tgLimit)
	sdFits := data.SDURL != "" && (sdSize == 0 || sdSize <= tgLimit)
	ctx.Infof(
		"facebook formats: hd=%dB(fits=%t safe=%t) sd=%dB(fits=%t safe=%t) tg_limit=%dB safe_limit=%dB",
		hdSize, hdFits, hdFitsSafe, sdSize, sdFits, sdFitsSafe, tgLimit, safeLimit,
	)

	var formats []*models.MediaFormat

	// Prefer HD only if it fits safely; otherwise fall back to SD to avoid
	// FD4902CC (413) on near-limit files like 52.3 MiB that probe just under 50 MiB.
	if hdFitsSafe {
		formats = append(formats, &models.MediaFormat{
			FormatID:   "hd",
			Type:       database.MediaTypeVideo,
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			URL:        []string{data.HDURL},
			Width:      data.Width,
			Height:     data.Height,
			Bitrate:    hdBitrateHint,
			FileSize:   hdSize,
		})
	}
	if sdFitsSafe {
		formats = append(formats, &models.MediaFormat{
			FormatID:   "sd",
			Type:       database.MediaTypeVideo,
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			URL:        []string{data.SDURL},
			Bitrate:    sdBitrateHint,
			FileSize:   sdSize,
		})
	}

	// If no format fits safely but at least one fits within the hard limit,
	// still include it as last resort — SendMediaGroup may still succeed if
	// Telegram's overhead is low, otherwise the TooLarge handler downstream
	// will surface a friendly error.
	if len(formats) == 0 {
		if hdFits {
			formats = append(formats, &models.MediaFormat{
				FormatID:   "hd",
				Type:       database.MediaTypeVideo,
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
				URL:        []string{data.HDURL},
				Width:      data.Width,
				Height:     data.Height,
				Bitrate:    hdBitrateHint,
				FileSize:   hdSize,
			})
		} else if sdFits {
			formats = append(formats, &models.MediaFormat{
				FormatID:   "sd",
				Type:       database.MediaTypeVideo,
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
				URL:        []string{data.SDURL},
				Bitrate:    sdBitrateHint,
				FileSize:   sdSize,
			})
		}
	}

	if len(formats) == 0 {
		// For reel/video, never fallback to image - fixes "keluar gamba lmao bukan video reel"
		// Merge: check too-large first (local fix), then thumbnail-retry (remote fix)
		isReelURL := false
		if ctx.ContentURL != "" {
			if bytes.Contains([]byte(ctx.ContentURL), []byte("/reel/")) || bytes.Contains([]byte(ctx.ContentURL), []byte("/watch")) {
				isReelURL = true
			}
		}
		if isReelURL {
			if (data.HDURL != "" && hdSize > tgLimit) || (data.SDURL != "" && sdSize > tgLimit) {
				return nil, util.ErrTelegramFileTooLarge
			}
			if data.ImageURL != "" || len(data.ImageURLs) > 0 {
				return nil, fmt.Errorf("reel video not found, only thumbnail - retry for video")
			}
			return nil, fmt.Errorf("no video formats found")
		}
		// Photo post? Try image fallback - support album with multiple images (e.g. 4 Gambar)
		if len(data.ImageURLs) > 0 {
			for i, u := range data.ImageURLs {
				var it *models.MediaItem
				if i == 0 {
					it = item
				} else {
					it = media.NewItem()
				}
				it.AddFormats(&models.MediaFormat{
					FormatID: fmt.Sprintf("image%d", i),
					Type:     database.MediaTypePhoto,
					URL:      []string{u},
				})
			}
			return media, nil
		}
		if data.ImageURL != "" {
			formats = append(formats, &models.MediaFormat{
				FormatID: "image",
				Type:     database.MediaTypePhoto,
				URL:      []string{data.ImageURL},
			})
		} else {
			if (data.HDURL != "" && hdSize > tgLimit) || (data.SDURL != "" && sdSize > tgLimit) {
				return nil, util.ErrTelegramFileTooLarge
			}
			return nil, fmt.Errorf("no video formats found")
		}
	}

	item.AddFormats(formats...)
	return media, nil
}

// tryMbasicShareAlbum fetches mbasic.facebook.com/share/{id}/ with iPhone UA
// and extracts photo album images + caption. Returns nil if not a photo album.
// FB returns inconsistent body sizes (46KB light vs 248KB full), so we retry
// until we get a large body with multiple scontent images.
func tryMbasicShareAlbum(ctx *models.ExtractorContext) (*models.Media, error) {
	mbasicURL := fmt.Sprintf("https://mbasic.facebook.com/share/%s/", ctx.ContentID)
	var bestBody []byte
	for attempt := 1; attempt <= 5; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt*400) * time.Millisecond)
		}
		resp, err := ctx.Fetch(
			http.MethodGet,
			mbasicURL,
			&networking.RequestParams{
				Headers: map[string]string{
					"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
					"Accept-Language": "en-US,en;q=0.5",
				},
			},
		)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) < 1000 {
			continue
		}
		if bestBody == nil || len(body) > len(bestBody) {
			bestBody = body
		}
		// If we have a large body with multiple scontent, we're done
		if len(body) > 150000 {
			break
		}
	}
	if bestBody == nil {
		return nil, fmt.Errorf("mbasic share fetch failed after retries")
	}
	body := bestBody

	// Extract caption from og:description
	var caption string
	if m := ogDescPattern.FindSubmatch(body); len(m) >= 2 {
		caption = html.UnescapeString(string(m[1]))
	}

	// Collect all scontent image URLs, dedup by filename
	var allUrls []string
	seen := map[string]bool{}
	for _, raw := range scontentPattern.FindAllString(string(body), -1) {
		u := unescapeFacebookURL(raw)
		// Skip profile/small images
		if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "p50x50") ||
			strings.Contains(u, "p100x100") || strings.Contains(u, "s120x120") ||
			strings.Contains(u, "emoji") {
			continue
		}
		fn := u
		if idx := strings.Index(fn, "?"); idx != -1 {
			fn = fn[:idx]
		}
		if idx := strings.LastIndex(fn, "/"); idx != -1 {
			fn = fn[idx+1:]
		}
		if seen[fn] {
			continue
		}
		seen[fn] = true
		allUrls = append(allUrls, u)
	}

	// Also check og:image
	if m := ogImagePattern.FindSubmatch(body); len(m) >= 2 {
		u := unescapeFacebookURL(string(m[1]))
		fn := u
		if idx := strings.Index(fn, "?"); idx != -1 {
			fn = fn[:idx]
		}
		if idx := strings.LastIndex(fn, "/"); idx != -1 {
			fn = fn[idx+1:]
		}
		if !seen[fn] {
			seen[fn] = true
			allUrls = append(allUrls, u)
		}
	}

	if len(allUrls) == 0 {
		return nil, fmt.Errorf("no images found in mbasic share")
	}

	// Do NOT upgradeFBImageToHD - hash (oh/oe) is tied to original resolution.
	// Upgrading p600->p1080 causes "Bad URL hash" error. Keep original URLs.

	// Build media with multiple items
	media := ctx.NewMedia()
	if caption != "" {
		media.SetCaption(caption)
	}
	for i, u := range allUrls {
		var item *models.MediaItem
		if i == 0 {
			item = media.NewItem()
		} else {
			item = media.NewItem()
		}
		item.AddFormats(&models.MediaFormat{
			FormatID: fmt.Sprintf("image%d", i),
			Type:     database.MediaTypePhoto,
			URL:      []string{u},
		})
	}
	return media, nil
}
