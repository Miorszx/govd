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
		// share/r = reel share, share/v = group video share, share/p = photo share, share/{id} bare = album/photo
		// Per user request: no fallback chains, each type has single method
		isBareShare := true
		for _, p := range []string{"/share/r/", "/share/v/", "/share/p/"} {
			if bytes.Contains([]byte(ctx.ContentURL), []byte(p)) {
				isBareShare = false
				break
			}
		}
		// BARE share/{id} METHOD: redirect via facebookexternalhit -> final post URL (100044139261197/posts/27816362968003357)
		// Tested: share/1BSen1YRcQ no-cookie external 323KB final /100044139261197/posts/27816362968003357/?rdid=... + og:url Zalora.../posts/1537175967763697/
		// Old tryMbasicShareAlbum used iPhone+cookies 59K no scontent flagged - removed
		if isBareShare {
			webHeaders := map[string]string{
				"User-Agent":      "facebookexternalhit/1.1",
				"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"Accept-Language": "en-US,en;q=0.5",
			}
			finalURL, err := ctx.FetchLocation(ctx.ContentURL, &networking.RequestParams{Headers: webHeaders})
			if err == nil && finalURL != "" && finalURL != ctx.ContentURL {
				return &models.ExtractorResponse{URL: finalURL}, nil
			}
			return nil, fmt.Errorf("failed to resolve bare share via redirect - flagged cookies")
		}

		// share/r METHOD: redirect share/r -> reel/{id}?rdid=...&share_url=... via FetchLocation
		// Use facebookexternalhit UA only, no fallback mbasic/www - per user request no fallback
		if bytes.Contains([]byte(ctx.ContentURL), []byte("/share/r/")) {
			webHeaders := map[string]string{
				"User-Agent":      "facebookexternalhit/1.1",
				"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"Accept-Language": "en-US,en;q=0.5",
			}
			finalURL, err := ctx.FetchLocation(ctx.ContentURL, &networking.RequestParams{Headers: webHeaders})
			if err == nil && finalURL != "" && finalURL != ctx.ContentURL {
				return &models.ExtractorResponse{URL: finalURL}, nil
			}
			return nil, fmt.Errorf("failed to resolve share/r via redirect - flagged cookies")
		}

		// share/p & share/v METHOD: mbasic/share/{id}/ iPhone -> og:url -> plugins HD only, no fallback
		iPhoneHeaders := map[string]string{
			"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
		}

		mbasicShareURL := fmt.Sprintf("https://mbasic.facebook.com/share/%s/", ctx.ContentID)
		if bytes.Contains([]byte(ctx.ContentURL), []byte("/share/v/")) {
			mbasicShareURL = fmt.Sprintf("https://mbasic.facebook.com/share/v/%s/", ctx.ContentID)
		} else if bytes.Contains([]byte(ctx.ContentURL), []byte("/share/p/")) {
			mbasicShareURL = fmt.Sprintf("https://mbasic.facebook.com/share/p/%s/", ctx.ContentID)
		}

		if resp, err := ctx.Fetch(http.MethodGet, mbasicShareURL, &networking.RequestParams{Headers: iPhoneHeaders}); err == nil && resp.StatusCode == 200 {
			bodyAll, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(bodyAll) > 1000 {
				if m := regexp.MustCompile(`property=["']og:url["']\s+content=["']([^"']+)["']`).FindSubmatch(bodyAll); len(m) == 2 {
					return &models.ExtractorResponse{URL: string(m[1])}, nil
				}
				if m := regexp.MustCompile(`content=["']([^"']+)["']\s+property=["']og:url["']`).FindSubmatch(bodyAll); len(m) == 2 {
					return &models.ExtractorResponse{URL: string(m[1])}, nil
				}
			}
		}

		return nil, fmt.Errorf("failed to resolve share url via mbasic og:url - flagged cookies")
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

// tryMbasicShareAlbum fetches mbasic.facebook.com/share/{id}/ with iPhone UA + shares via group permalink
// METHOD V2 per user request: fresh scontent oh= only, no og:image fallback, no upgrade p1080 (causes 403 Bad Hash)
// Tested on share/p/1Cs9f4wm7M: mbasic/share/p iPhone 47K -> og:url groups/.../posts/351067... -> mbasic/groups/... iPhone 222KB 11 scontent oh= fresh dl 200 OK
func tryMbasicShareAlbum(ctx *models.ExtractorContext) (*models.Media, error) {
	iphoneHeaders := map[string]string{
		"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.5",
	}

	fetch := func(url string) ([]byte, string, error) {
		resp, err := ctx.Fetch(http.MethodGet, url, &networking.RequestParams{Headers: iphoneHeaders})
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("status %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if len(body) < 1000 {
			return nil, "", fmt.Errorf("body too small %d", len(body))
		}
		finalURL := ""
		if resp.Request != nil {
			finalURL = resp.Request.URL.String()
		}
		return body, finalURL, nil
	}

	// Step 1: mbasic/share/{id} iPhone
	mbasicShareURL := fmt.Sprintf("https://mbasic.facebook.com/share/%s/", ctx.ContentID)
	var body []byte
	var finalURL string
	var bestBody []byte
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt*400) * time.Millisecond)
		}
		b, fURL, err := fetch(mbasicShareURL)
		if err != nil {
			continue
		}
		if bestBody == nil || len(b) > len(bestBody) {
			bestBody = b
			finalURL = fURL
		}
		if len(b) > 100000 {
			break
		}
	}
	if bestBody == nil {
		return nil, fmt.Errorf("mbasic share fetch failed")
	}
	body = bestBody

	// Step 1b: if share/p gives og:url group post, fetch that group permalink with mbasic iPhone for fresh images
	// This is the working method: share/p/1Cs9f4wm7M -> groups/280707.../posts/351067... -> mbasic/groups/... 222KB 11 scontent oh=
	if m := regexp.MustCompile(`property="og:url" content="([^"]+)"`).FindSubmatch(body); len(m) == 2 {
		ogURL := string(m[1])
		// convert to mbasic
		mbasicOG := strings.Replace(ogURL, "https://www.facebook.com", "https://mbasic.facebook.com", 1)
		mbasicOG = strings.Replace(mbasicOG, "https://m.facebook.com", "https://mbasic.facebook.com", 1)
		if !strings.Contains(mbasicOG, "mbasic.facebook.com") {
			mbasicOG = "https://mbasic.facebook.com" + strings.TrimPrefix(ogURL, "https://www.facebook.com")
		}
		if b2, _, err := fetch(mbasicOG); err == nil && len(b2) > len(body) {
			body = b2
			finalURL = mbasicOG
		} else if b2, _, err := fetch(mbasicOG); err == nil && len(b2) > 1000 {
			// even if smaller than share body, if share body has only 46KB and no scontent, use permalink body (222KB)
			if len(b2) > 20000 {
				body = b2
			}
		}
	}

	// Extract caption from og:description or title
	var caption string
	if m := regexp.MustCompile(`<meta\s+property="og:description" content="([^"]*)"`).FindSubmatch(body); len(m) >= 2 {
		caption = html.UnescapeString(string(m[1]))
	}
	if caption == "" {
		if m := regexp.MustCompile(`<meta\s+property="og:title" content="([^"]*)"`).FindSubmatch(body); len(m) >= 2 {
			caption = html.UnescapeString(string(m[1]))
		}
	}

	// Fresh scontent oh= only, no og:image, no upgrade
	reFresh := regexp.MustCompile(`https://[^"'\s]*scontent[^"'\s]*oh=[^"'\s]+`)
	var allUrls []string
	seen := map[string]bool{}
	for _, raw := range reFresh.FindAllString(string(body), -1) {
		u := unescapeFacebookURL(raw)
		u = strings.ReplaceAll(u, "&amp;", "&")
		u = strings.ReplaceAll(u, `\/`, "/")
		// filter tiny/profile/emoji
		if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "p50x50") || strings.Contains(u, "p100x100") || strings.Contains(u, "s120x120") || strings.Contains(u, "s74x74") || strings.Contains(u, "s168x128") || strings.Contains(u, "p74x74") || strings.Contains(u, "p120x120") || strings.Contains(u, "p32x32") || strings.Contains(u, "emoji") || strings.Contains(u, "m1/v/t6") {
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
		if len(allUrls) >= 10 {
			break
		}
	}

	_ = finalURL

	if len(allUrls) == 0 {
		return nil, fmt.Errorf("no fresh scontent oh= images found")
	}

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
