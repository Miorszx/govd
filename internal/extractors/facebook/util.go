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
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

var webHeaders = map[string]string{
	"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"Accept-Language":           "en-US,en;q=0.5",
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
}

// videoFetchHeaders for Facebook CDN: browser User-Agents are rate-limited
// on progressive video URLs, so use a non-browser UA for HEAD/GET.
var videoFetchHeaders = map[string]string{
	"User-Agent": "facebookexternalhit/1.1",
	"Accept":     "*/*",
	"Range":      "bytes=0-0",
}

var (
	hdURLPattern = regexp.MustCompile(
		`"progressive_url"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"\s*,\s*"failure_reason"\s*:\s*[^,]+\s*,\s*"metadata"\s*:\s*\{\s*"quality"\s*:\s*"HD"\s*\}`,
	)
	sdURLPattern = regexp.MustCompile(
		`"progressive_url"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"\s*,\s*"failure_reason"\s*:\s*[^,]+\s*,\s*"metadata"\s*:\s*\{\s*"quality"\s*:\s*"SD"\s*\}`,
	)
	titlePattern = regexp.MustCompile(
		`"title"\s*:\s*\{\s*"text"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`,
	)
	// HTML <title> tag - for m.facebook.com reels where og:title/og:description are truncated but <title> has full caption
	htmlTitlePattern = regexp.MustCompile(
		`<title[^>]*>([^<]+)</title>`,
	)
	// FB modern pages embed the post caption in several places.
	// These are tried in order; the longest non-empty match wins.
	ogDescPattern = regexp.MustCompile(
		`<meta\s+property="og:description"\s+content="([^"]*)"\s*/?>`,
	)
	ogTitlePattern = regexp.MustCompile(
		`<meta\s+property="og:title"\s+content="([^"]*)"\s*/?>`,
	)
	ogImagePattern = regexp.MustCompile(
		`<meta[^>]+property="og:image"[^>]+content="([^"]+)"`,
	)
	ogVideoPattern = regexp.MustCompile(
		`<meta[^>]+property="og:video"[^>]+content="([^"]+)"`,
	)
	scontentPattern = regexp.MustCompile(
		`https://[^"\s]*scontent[^"\s]*`,
	)
	// Facepager-style: attachments JSON contains HD src directly
	graphImageSrcPattern = regexp.MustCompile(`"src"\s*:\s*"(https://[^"]*scontent[^"]+)"`)
	graphImageSourcePattern = regexp.MustCompile(`"source"\s*:\s*"(https://[^"]*scontent[^"]+)"`)
	// FB image sizing markers like p600x600, p394x394, p720x720 etc – we upgrade to p1080x1080 for HD
	fbImageSizePattern = regexp.MustCompile(`p(\d+)x(\d+)`)
	messagePattern = regexp.MustCompile(
		`"(?:message|attached_story)"\s*:\s*\{\s*"text"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`,
	)
	storyPattern = regexp.MustCompile(
		`"story"\s*:\s*\{\s*"comet_sections"\s*:\s*\{[^}]*"message"\s*:\s*\{\s*"text"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`,
	)
	// creation_story.comet_sections – another FB wrapper (reels posted via page)
	creationStoryPattern = regexp.MustCompile(
		`"creation_story"\s*:\s*\{[^}]{0,500}"message"\s*:\s*\{\s*"text"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`,
	)
	// "subtitle" / "description" plain text blocks that sometimes hold the caption
	descriptionPattern = regexp.MustCompile(
		`"description"\s*:\s*\{\s*"text"\s*:\s*"([^"\\]{20,}[^"\\]*(?:\\.[^"\\]*)*)"`,
	)
)

func tryFetchHDFromPlugins(ctx *models.ExtractorContext, videoID string) (string, string) {
	// Plugins endpoint gives hd_src only with desktop UA - iPhone UA returns 63719 no HD
	pluginsURL := "https://www.facebook.com/plugins/video.php?href=https://www.facebook.com/reel/" + videoID + "&show_text=0"
	resp, err := ctx.Fetch(
		"GET",
		pluginsURL,
		&networking.RequestParams{
			Headers: webHeaders,
		},
	)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ""
	}
	var hd, sd string
	if m := regexp.MustCompile(`"hd_src"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
		hd = unescapeFacebookURL(string(m[1]))
		hd = strings.ReplaceAll(hd, `\/`, "/")
		hd = strings.ReplaceAll(hd, "&amp;", "&")
	}
	if m := regexp.MustCompile(`"sd_src"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
		sd = unescapeFacebookURL(string(m[1]))
		sd = strings.ReplaceAll(sd, `\/`, "/")
		sd = strings.ReplaceAll(sd, "&amp;", "&")
	}
	return hd, sd
}

func GetVideoData(ctx *models.ExtractorContext) (*VideoData, error) {
	isReel := strings.Contains(ctx.ContentURL, "/reel/") || strings.Contains(ctx.ContentURL, "/watch")
	contentURL := ctx.ContentURL
	if isReel {
		contentURL = strings.Replace(contentURL, "www.facebook.com", "m.facebook.com", 1)
		contentURL = strings.Replace(contentURL, "mbasic.facebook.com", "m.facebook.com", 1)
		if strings.Contains(contentURL, "/watch") && ctx.ContentID != "" {
			contentURL = "https://m.facebook.com/reel/" + ctx.ContentID
		}
	} else {
		contentURL = strings.Replace(contentURL, "m.facebook.com", "www.facebook.com", 1)
		contentURL = strings.Replace(contentURL, "mbasic.facebook.com", "www.facebook.com", 1)
		if strings.Contains(contentURL, "/watch") && ctx.ContentID != "" {
			contentURL = "https://www.facebook.com/reel/" + ctx.ContentID
			isReel = true
			contentURL = strings.Replace(contentURL, "www.facebook.com", "m.facebook.com", 1)
		}
	}

	// Option B: Try Graph API first for HD (if FACEBOOK_ACCESS_TOKEN set)
	// This gives HD src without oh= signature issue (no 403) and handles albums + videos
	// Supports: share/p, share/v, permalink, etc
	// Photo: {pageID}_{postID}?fields=attachments{media{image{src,width,height}},subattachments}
	// Video: {videoID}?fields=source,format
	if config.Env.FacebookAccessToken != "" {
		// Extract pageID and postID for Graph API
		var pageID, postID string
		if m := regexp.MustCompile(`[?&]id=(\d+)`).FindStringSubmatch(contentURL); len(m) == 2 {
			pageID = m[1]
			postID = ctx.ContentID
		}
		if pageID == "" {
			if m := regexp.MustCompile(`facebook\.com/(\d+)/(?:posts|videos|reels|permalink)/`).FindStringSubmatch(contentURL); len(m) == 2 {
				pageID = m[1]
				if postID == "" {
					postID = ctx.ContentID
				}
			}
		}
		if pageID == "" {
			if m := regexp.MustCompile(`facebook\.com/groups/(\d+)/`).FindStringSubmatch(contentURL); len(m) == 2 {
				pageID = m[1]
				if postID == "" {
					postID = ctx.ContentID
				}
			}
		}
		// For bare share/<id> like 1CKF4qojsQ, try to get pageID via S:_I if token exists, but we have postID from ctx
		// Also extract photoIDs from contentID for direct photo lookup (for p albums)
		var photoIDs []string
		if len(ctx.ContentID) > 10 {
			// If contentID looks like a post ID, use it as postID
			if postID == "" {
				postID = ctx.ContentID
			}
		}
		// Try Graph API for photo album HD
		if postID != "" {
			if gd, err := tryGraphAPIHD(ctx, pageID, postID, photoIDs); err == nil && gd != nil {
				// Upgrade any remaining p600 to p1080 (should already be HD from Graph, but keep)
				if gd.ImageURL != "" {
					gd.ImageURL = upgradeFBImageToHD(gd.ImageURL)
				}
				for i := range gd.ImageURLs {
					gd.ImageURLs[i] = upgradeFBImageToHD(gd.ImageURLs[i])
				}
				return gd, nil
			}
		}
		// Try Graph API for video (share/v, reel, etc)
		if ctx.ContentID != "" {
			if gv, err := tryGraphAPIVideo(ctx, ctx.ContentID); err == nil && gv != nil {
				if gv.HDURL != "" || gv.SDURL != "" || gv.ImageURL != "" {
					return gv, nil
				}
			}
		}
		// Also try with postID as videoID if ContentID is share ID like 1BNWPp61LJ which resolves to group permalink
		if postID != "" && postID != ctx.ContentID {
			if gv, err := tryGraphAPIVideo(ctx, postID); err == nil && gv != nil {
				if gv.HDURL != "" || gv.SDURL != "" {
					return gv, nil
				}
			}
		}
	}

	// convert watch URLs to reel permalink,
	// /watch/?v=XXX pages return wrong video data when scraped
	if strings.Contains(contentURL, "/watch") && ctx.ContentID != "" {
		contentURL = "https://www.facebook.com/reel/" + ctx.ContentID
		isReel = true
		if !strings.Contains(contentURL, "m.facebook.com") {
			contentURL = strings.Replace(contentURL, "www.facebook.com", "m.facebook.com", 1)
		}
	}

	// Facebook serves variable HTML: sometimes a full video page (with
	// progressive_url + caption), sometimes a light/blocked variant with
	// neither, sometimes HTTP 400/429 anti-bot. A follow-up request frequently
	// lands on a different edge and returns the complete page, so retry the
	// fetch with a short backoff before giving up. Using `impersonate: true`
	// in private/config.yaml (chrome TLS) is REQUIRED to avoid the 400.
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			sleepMS := time.Duration(300*(1<<(attempt-2))) * time.Millisecond
			if sleepMS > 3*time.Second {
				sleepMS = 3 * time.Second
			}
			time.Sleep(sleepMS)
		}

		reqHeaders := map[string]string{
			"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
		}

		resp, err := ctx.Fetch(
			http.MethodGet,
			contentURL,
			&networking.RequestParams{
				Headers: reqHeaders,
			},
		)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			continue
		}

		logger.WriteFile("fb_response", resp)

		if resp.StatusCode != http.StatusOK {
			// FB anti-bot often returns 400/429 on this VPS; treat as retryable
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == 429 || resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("failed to get page: %s (anti-bot, retryable)", resp.Status)
				// keep body snippet for debug if small
				if len(body) > 0 && len(body) < 500 {
					lastErr = fmt.Errorf("%w body=%q", lastErr, string(body))
				}
				continue
			}
			lastErr = fmt.Errorf("failed to get page: %s", resp.Status)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		// For photo posts, try mbasic iPhone UA for higher res dst-webp (720x670) vs og:image 645x600
		if !isReel {
			mbasicURL := strings.Replace(contentURL, "www.facebook.com", "mbasic.facebook.com", 1)
			mbasicURL = strings.Replace(mbasicURL, "m.facebook.com", "mbasic.facebook.com", 1)
			respM, errM := ctx.Fetch(
				http.MethodGet,
				mbasicURL,
				&networking.RequestParams{
					Headers: map[string]string{
						"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
						"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
						"Accept-Language": "en-US,en;q=0.5",
					},
				},
			)
			if errM == nil && respM.StatusCode == 200 {
				bodyM, _ := io.ReadAll(respM.Body)
				respM.Body.Close()
				if len(bodyM) > len(body) && strings.Contains(string(bodyM), "dst-webp") {
					body = bodyM
				}
			}
		}

		// If FB returns a very small / login / checkpoint page (< 5KB), treat as blocked variant
		if len(body) < 5000 {
			// quick check for known blocked markers before parsing
			lb := string(body)
			if strings.Contains(lb, "login") && strings.Contains(lb, "checkpoint") ||
				strings.Contains(lb, "temporarily blocked") ||
				strings.Contains(lb, "you have been temporarily blocked") {
				lastErr = fmt.Errorf("no video URLs found in page (blocked/login page, len=%d)", len(body))
				continue
			}
		}

		data, err := parseVideoFromBody(body, ctx.ContentID)
		if err != nil {
			// Only retry on the "no video URLs" variant (light/blocked
			// page). Other errors (real parse failures) should surface.
			if strings.Contains(err.Error(), "no video URLs found") {
				lastErr = err
				continue
			}
			return nil, err
		}
		if isReel && data.HDURL == "" && data.SDURL == "" && data.ImageURL != "" {
			lastErr = fmt.Errorf("thumbnail only, retrying for video")
			if attempt < maxAttempts {
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}
		// Even if video URLs found but caption is empty, do NOT fail —
		// video itself is valid, caption is best-effort.
		_ = body
		// Reel: fallback to plugins/video.php for HD (desktop UA returns hd_src even with flagged cookies)
		if isReel {
			if data.HDURL == "" {
				if hd, sd := tryFetchHDFromPlugins(ctx, ctx.ContentID); hd != "" || sd != "" {
					if hd != "" {
						data.HDURL = hd
					}
					if data.SDURL == "" && sd != "" {
						data.SDURL = sd
					}
					if data.HDURL != "" || data.SDURL != "" {
						return data, nil
					}
				}
			}
		}
		return data, nil
	}

	// Fallback for photo posts like share/p: try mbasic for og:image
	// ContentURL like story.php?story_fbid=POST_ID&id=PAGE_ID
	if lastErr != nil && len(ctx.ContentID) > 0 {
		// try to get pageID from contentURL id= param
		var pageID string
		var bestBody []byte
		if m := regexp.MustCompile(`[?&]id=(\d+)`).FindStringSubmatch(contentURL); len(m) == 2 {
			pageID = m[1]
		}
		// If no pageID, try from path like /{page_id}/posts/{post_id}/ for bare share/<id> like 1CKF4qojsQ
		if pageID == "" {
		    if m := regexp.MustCompile(`facebook\.com/(\d+)/(?:posts|videos|reels|permalink)/`).FindStringSubmatch(contentURL); len(m) == 2 {
		        pageID = m[1]
		    }
		}
		// For group posts like groups/2807075776107813/posts/3505374679611249/ (19Fea5TgkK/ & 19HrxCEJWd/)
		if pageID == "" {
		    if m := regexp.MustCompile(`facebook\.com/groups/(\d+)/`).FindStringSubmatch(contentURL); len(m) == 2 {
		        pageID = m[1]
		    }
		}
		// If still no pageID, try to get it from S:_I token by fetching story.php with iPhone UA (for 1FtTAuWcPo etc)
		if pageID == "" {
			storyURL := fmt.Sprintf("https://www.facebook.com/story.php?story_fbid=%s", ctx.ContentID)
			respS, errS := ctx.Fetch(
				http.MethodGet,
				storyURL,
				&networking.RequestParams{
					Headers: map[string]string{
						"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
					},
				},
			)
			if errS == nil && respS.StatusCode == 200 {
				bodyS, _ := io.ReadAll(respS.Body)
				respS.Body.Close()
				if m := regexp.MustCompile(`S:_I(\d+):(\d+):`).FindSubmatch(bodyS); len(m) == 3 {
					pageID = string(m[1])
				}
			}
		}
		// Combined fallback for album posts: mbasic + photo.php (like GabrielVelasco repo selenium scroll, but pure-Go)
		// mbasic gives 1 image, photo.php with desktop UA gives 10 scontent including album images (handles https:// and https:\/\/ escaped)
		var allUrls []string
		collectFunc := func(body []byte) {
			if len(body) < 1000 {
				return
			}
			// Find both https:// and https:\/\/ escaped variants
			// First unescape \/
				strBody := string(body)
				// Also handle escaped scontent: https:\/\/scontent...
				// Replace \/ with / for matching
				unescapedForMatch := strings.ReplaceAll(strBody, `\/`, "/")
				for _, raw := range scontentPattern.FindAllString(unescapedForMatch, 30) {
					u := upgradeFBImageToHD(unescapeFacebookURL(raw))
					// Filter junk: require signed URL (oh=) and exclude tiny/profile frames and keyframes
					if !strings.Contains(u, "oh=") {
						continue
					}
					if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "t39.30808-2") || strings.Contains(u, "p50x50") || strings.Contains(u, "p100x100") || strings.Contains(u, "emoji") || strings.Contains(u, "m1/v/t6") || strings.Contains(u, "/t45.") || strings.Contains(u, "s120x120") || strings.Contains(u, "p120x120") || strings.Contains(u, "s64x64") || strings.Contains(u, "p64x64") || strings.Contains(u, "_s.jpg") || strings.Contains(u, "_q.jpg") {
						continue
					}
					fn := u
					if idx := strings.Index(fn, "?"); idx != -1 {
						fn = fn[:idx]
					}
					if idx := strings.LastIndex(fn, "/"); idx != -1 {
						fn = fn[idx+1:]
					}
					if fn == "" {
						continue
					}
					dup := false
					for _, e := range allUrls {
						ef := e
						if idx := strings.Index(ef, "?"); idx != -1 {
							ef = ef[:idx]
						}
						if idx := strings.LastIndex(ef, "/"); idx != -1 {
							ef = ef[idx+1:]
						}
						if ef == fn {
							dup = true
							break
						}
					}
					if dup {
						continue
					}
					allUrls = append(allUrls, u)
				}
			}
		if pageID != "" {
			// Retry mbasic fetch up to 6 times to get the large album body (248KB) which contains all 3 images
			// Small body (46KB) only has 1 image, large body (248KB) has 3 images with oh= signatures
			mbasicURL := fmt.Sprintf("https://mbasic.facebook.com/%s/posts/%s/", pageID, ctx.ContentID)
			for mbAttempt := 1; mbAttempt <= 6; mbAttempt++ {
				if mbAttempt > 1 {
					time.Sleep(time.Duration(mbAttempt*300) * time.Millisecond)
				}
				resp2, err2 := ctx.Fetch(
					http.MethodGet,
					mbasicURL,
					&networking.RequestParams{
						Headers: map[string]string{
							"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
						},
					},
				)
				if err2 != nil || resp2.StatusCode != 200 {
					if resp2 != nil {
						resp2.Body.Close()
					}
					continue
				}
				body2, _ := io.ReadAll(resp2.Body)
				resp2.Body.Close()
				if len(body2) < 1000 {
					continue
				}
				// keep best (largest) body
				if bestBody == nil || len(body2) > len(bestBody) {
					bestBody = body2
				}
				// if we already have a large body with multiple scontent, break early
				if len(body2) > 150000 {
					break
				}
			}
			if bestBody != nil {
				// og:image first
				if m := ogImagePattern.FindSubmatch(bestBody); len(m) >= 2 {
					u := upgradeFBImageToHD(unescapeFacebookURL(string(m[1])))
					fn := u
					if idx := strings.Index(fn, "?"); idx != -1 {
						fn = fn[:idx]
					}
					if idx := strings.LastIndex(fn, "/"); idx != -1 {
						fn = fn[idx+1:]
					}
					dup := false
					for _, e := range allUrls {
						ef := e
						if idx := strings.Index(ef, "?"); idx != -1 {
							ef = ef[:idx]
						}
						if idx := strings.LastIndex(ef, "/"); idx != -1 {
							ef = ef[idx+1:]
						}
						if ef == fn {
							dup = true
							break
						}
					}
					if !dup {
						allUrls = append(allUrls, u)
					}
				}
				collectFunc(bestBody)
			}
		}
		// Fallback photo.php?fbid=POST_ID with desktop UA - contains all album images (GabrielVelasco approach pure-Go)
		photoURL := fmt.Sprintf("https://www.facebook.com/photo.php?fbid=%s", ctx.ContentID)
		respPhoto, errPhoto := ctx.Fetch(
			http.MethodGet,
			photoURL,
			&networking.RequestParams{
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
				},
			},
		)
		if errPhoto == nil && respPhoto.StatusCode == 200 {
			bodyPhoto, _ := io.ReadAll(respPhoto.Body)
			respPhoto.Body.Close()
			collectFunc(bodyPhoto)
		}
		if len(allUrls) > 0 {
			if len(allUrls) > 10 {
				allUrls = allUrls[:10]
			}
			for i := range allUrls {
				allUrls[i] = upgradeFBImageToHD(allUrls[i])
			}
			// try extract caption from bestBody if available
			caption := ""
			if bestBody != nil {
				if m := ogDescPattern.FindSubmatch(bestBody); len(m) >= 2 {
					caption = html.UnescapeString(string(m[1]))
				}
			}
			if len(allUrls) == 1 {
				return &VideoData{
					ImageURL: allUrls[0],
					Title:    caption,
				}, nil
			}
			return &VideoData{
				ImageURLs: allUrls,
				Title:     caption,
			}, nil
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no video URLs found in page")
	}
	// Facepager-style token hint: if we tried mbasic and got login redirect, suggest Graph API token
	// This helps user understand why share/v 1BNWPp61LJ gives thumbnail 720x720 not video
	// and why 19HrxCEJWd gives 1 image not 3
	// No token config currently, but log hint for debugging (logger already writes fb_response)
	return nil, lastErr
}

func parseVideoFromBody(body []byte, videoID string) (*VideoData, error) {
	data := &VideoData{}

	// find the section belonging to the requested video
	// NEVER fallback to full body when section not found - this prevents photo posts like share/p
	// returning random video from feed (Ayah Bopley bug). All video pages have dash_mpd_debug marker,
	// so section==nil means it's a photo/album post or blocked page - return image or error.
	section := findVideoSection(body, videoID)
	if section == nil {
		section = []byte{}
	}

	if match := hdURLPattern.FindSubmatch(section); len(match) >= 2 {
		data.HDURL = unescapeFacebookURL(string(match[1]))
	}
	if match := sdURLPattern.FindSubmatch(section); len(match) >= 2 {
		data.SDURL = unescapeFacebookURL(string(match[1]))
	}

	// findVideoSection anchors on dash_mpd_debug.mpd?v=VIDEO_ID and bounds
	// the block by the following "id":"VIDEO_ID" marker. On some page
	// variants that marker lands BEFORE the progressive_url's trailing
	// failure_reason/metadata (which carries the HD/SD quality), so the
	// URL regexes above cannot match inside the truncated slice and we
	// end up with no URLs even though the page clearly has them. Retry
	// on the full body (single-video posts/reels) so the extraction does
	// not falsely fail. Caption extraction already runs against body.
	if data.HDURL == "" && data.SDURL == "" {
		// Don't fallback to full body if we already determined it's feed/photo (section empty)
		if len(section) == 0 {
			// Try image extraction for photo posts - support album 4 gambar
			// Facepager-style: attachments JSON contains HD src in "src":"https://scontent..." and subattachments for albums
			var urls []string
			// Phase 1: Graph-style src (attachments.media.image.src) - this gives HD directly like Facepager preset
			// These are like {"src":"https://scontent...","width":1080} from attachments field
			seenSrc := map[string]struct{}{}
			for _, m := range graphImageSrcPattern.FindAllSubmatch(body, 20) {
				if len(m) < 2 { continue }
				raw := string(m[1])
				// unescape
				raw = unescapeFacebookURL(raw)
				raw = upgradeFBImageToHD(raw)
				if !strings.Contains(raw, "oh=") { continue }
				if _, ok := seenSrc[raw]; ok { continue }
				seenSrc[raw] = struct{}{}
				// filter tiny
				if strings.Contains(raw, "t39.30808-1") || strings.Contains(raw, "p50x50") || strings.Contains(raw, "s120x120") {
					continue
				}
				urls = append(urls, raw)
			}
			// Phase 2: source field (images{source})
			for _, m := range graphImageSourcePattern.FindAllSubmatch(body, 20) {
				if len(m) < 2 { continue }
				raw := unescapeFacebookURL(string(m[1]))
				raw = upgradeFBImageToHD(raw)
				if !strings.Contains(raw, "oh=") { continue }
				if _, ok := seenSrc[raw]; ok { continue }
				seenSrc[raw] = struct{}{}
				if strings.Contains(raw, "t39.30808-1") { continue }
				urls = append(urls, raw)
			}
			// Phase 3: og:image - PRIORITY for single photo posts, it's the actual post image not random scontent
			if match := ogImagePattern.FindSubmatch(body); len(match) >= 2 {
				ogURL := unescapeFacebookURL(string(match[1]))
				// Don't upgrade og:image - p1080 upgrade often breaks with Bad URL hash for certain image types
				// Use original p600x600 which works reliably (44KB 645x600)
				if ogURL != "" && !strings.Contains(ogURL, "p50x50") && !strings.Contains(ogURL, "s120x120") {
					// Prepend og:image so it's first priority
					urls = append([]string{ogURL}, urls...)
				}
			}
			// Phase 4: classic scontent pattern (existing)
			for _, raw := range scontentPattern.FindAllString(string(body), 20) {
				u := upgradeFBImageToHD(unescapeFacebookURL(raw))
				if !strings.Contains(u, "oh=") {
					continue
				}
				if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "t39.30808-2") || strings.Contains(u, "p50x50") || strings.Contains(u, "p100x100") || strings.Contains(u, "m1/v/t6") || strings.Contains(u, "s120x120") || strings.Contains(u, "p120x120") {
					continue
				}
				fn := u
				if idx := strings.Index(fn, "?"); idx != -1 {
					fn = fn[:idx]
				}
				if idx := strings.LastIndex(fn, "/"); idx != -1 {
					fn = fn[idx+1:]
				}
				dup := false
				for _, e := range urls {
					ef := e
					if idx := strings.Index(ef, "?"); idx != -1 {
						ef = ef[:idx]
					}
					if idx := strings.LastIndex(ef, "/"); idx != -1 {
						ef = ef[idx+1:]
					}
					if ef == fn {
						dup = true
						break
					}
				}
				if dup {
					continue
				}
				urls = append(urls, u)
			}
			if len(urls) > 0 {
				if len(urls) > 10 {
					urls = urls[:10]
				}
				if len(urls) == 1 {
					data.ImageURL = urls[0]
				} else {
					data.ImageURLs = urls
				}
			}
		} else {
			if match := hdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.HDURL = unescapeFacebookURL(string(match[1]))
			}
			if match := sdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.SDURL = unescapeFacebookURL(string(match[1]))
			}
			// og:video fallback for m.facebook.com reels with flagged cookies (sve_sd mp4)
			if data.HDURL == "" && data.SDURL == "" {
				if m := ogVideoPattern.FindSubmatch(body); len(m) >= 2 {
					u := unescapeFacebookURL(string(m[1]))
					u = strings.ReplaceAll(u, "&amp;", "&")
					if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
						data.SDURL = u
					}
				}
			}
			// browser_native_* / playable_url fallback for m pages
			if data.HDURL == "" && data.SDURL == "" {
				if m := regexp.MustCompile(`"browser_native_hd_url"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
					data.HDURL = unescapeFacebookURL(string(m[1]))
				}
				if m := regexp.MustCompile(`"browser_native_sd_url"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
					data.SDURL = unescapeFacebookURL(string(m[1]))
				}
				if data.HDURL == "" {
					if m := regexp.MustCompile(`"playable_url_quality_hd"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
						data.HDURL = unescapeFacebookURL(string(m[1]))
					}
				}
				if data.SDURL == "" {
					if m := regexp.MustCompile(`"playable_url"\s*:\s*"([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
						data.SDURL = unescapeFacebookURL(string(m[1]))
					}
				}
			}
			// last resort thumbnail
			if data.HDURL == "" && data.SDURL == "" {
				if match := ogImagePattern.FindSubmatch(body); len(match) >= 2 {
					data.ImageURL = unescapeFacebookURL(string(match[1]))
				}
				if data.ImageURL == "" {
					for _, raw := range scontentPattern.FindAllString(string(body), 5) {
						u := unescapeFacebookURL(raw)
						if !strings.Contains(u, "oh=") {
							continue
						}
						if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "p50x50") {
							continue
						}
						data.ImageURL = u
						break
					}
				}
			}
		}
	}
	// title can be anywhere in the page
	if match := titlePattern.FindSubmatch(body); len(match) >= 2 {
		data.Title = unescapeUnicode(string(match[1]))
	}
	// HTML <title> tag often has fuller caption than og:title for m reels - prefer it if longer
	if match := htmlTitlePattern.FindSubmatch(body); len(match) >= 2 {
		t := unescapeUnicode(string(match[1]))
		t = html.UnescapeString(t)
		t = strings.TrimSpace(t)
		// Remove zero-width unicode characters (LRM, RLM, ZWSP, etc.) that FB injects
		t = strings.Map(func(r rune) rune {
			switch r {
			case 0x200E, 0x200F, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
				return -1
			}
			return r
		}, t)
		t = strings.TrimSpace(t)
		// Remove trailing "| Facebook" suffix
		if idx := strings.LastIndex(t, " | Facebook"); idx != -1 {
			t = t[:idx]
		}
		// Strip trailing page name like " | Modern Warfare - DS" or " | SameerGamer" if it looks like metadata
		if idx := strings.LastIndex(t, " | "); idx != -1 {
			suffix := t[idx+3:]
			if len(suffix) < 50 && !strings.Contains(suffix, "#") && suffix != "" {
				t = t[:idx]
			}
		}
		t = strings.TrimSpace(t)
		// Skip generic "Facebook" title
		if len(t) > len(data.Title) && !strings.EqualFold(t, "Facebook") && !strings.HasPrefix(strings.ToLower(t), "log in") {
			data.Title = t
		}
	}

	// Facebook stores the post caption / description in several places
	// depending on page variant. Collect all candidates and keep the
	// longest one that actually looks like a caption (not a short token).
	// PRIORITY: caption anchored to this videoID (message closest to "id":"videoID")
	candidates := []string{}
	anchoredCaption := findCaptionAnchoredToID(body, videoID)

	// helper: decode + html-unescape + trim + basic sanity (FB often wraps caption in entities)
	addCandidate := func(raw string) {
		s := unescapeUnicode(raw)
		s = html.UnescapeString(s)
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// discard very short tokens that look like UI labels, not captions
		if len(s) < 3 {
			return
		}
		// FB sometimes repeats "Facebook" / "Log in" as og:description fallback – skip
		lower := strings.ToLower(s)
		if lower == "facebook" || strings.HasPrefix(lower, "log in to facebook") || strings.HasPrefix(lower, "log into facebook") {
			return
		}
		s = cleanFacebookCaption(s)
		if s == "" {
			return
		}
		candidates = append(candidates, s)
	}

	// Anchored caption wins only if substantial (len>=15 or has hashtag or contains series marker)
	// This avoids picking "Admin" or short UI labels from feed
	if anchoredCaption != "" {
		anchoredCaption = strings.TrimSpace(html.UnescapeString(unescapeUnicode(anchoredCaption)))
		if len(anchoredCaption) >= 3 && !strings.EqualFold(anchoredCaption, "facebook") {
			anchoredCaption = cleanFacebookCaption(anchoredCaption)
			if anchoredCaption != "" {
				lowerA := strings.ToLower(anchoredCaption)
				isSubstantial := len(anchoredCaption) >= 15 || strings.Contains(anchoredCaption, "#") || strings.Contains(lowerA, "random memes from my phone")
				if isSubstantial {
					data.Title = anchoredCaption
					if data.HDURL != "" || data.SDURL != "" {
						return data, nil
					}
				} else {
					// not substantial, treat as candidate
					candidates = append(candidates, anchoredCaption)
				}
			}
		}
	}

	// Only run other regexes if anchored did not already succeed with URLs.
	// This saves ~6 regex scans (60MB) on the happy path.
	if data.Title == "" || data.HDURL == "" && data.SDURL == "" {
		// lightly: try cheap patterns first (title already tried, so og tags)
		if match := ogDescPattern.FindSubmatch(body); len(match) >= 2 {
			addCandidate(string(match[1]))
		}
		if len(candidates) == 0 { // avoid extra scans if we already have a candidate
			if match := ogTitlePattern.FindSubmatch(body); len(match) >= 2 {
				addCandidate(string(match[1]))
			}
		}
		if match := messagePattern.FindSubmatch(body); len(match) >= 2 {
			addCandidate(string(match[1]))
		}
		if len(candidates) == 0 {
			if match := storyPattern.FindSubmatch(body); len(match) >= 2 {
				addCandidate(string(match[1]))
			}
			if match := creationStoryPattern.FindSubmatch(body); len(match) >= 2 {
				addCandidate(string(match[1]))
			}
			if match := descriptionPattern.FindSubmatch(body); len(match) >= 2 {
				addCandidate(string(match[1]))
			}
		}
		// Also scrape ALL "message":{"text":"..."} occurrences and keep longest (page may have multiple)
		// Only if we still need a caption - this is the heaviest scan (FindAllSubmatch)
		if len(candidates) == 0 {
			allMessages := messagePattern.FindAllSubmatch(body, 10)
			for _, m := range allMessages {
				if len(m) >= 2 {
					addCandidate(string(m[1]))
				}
			}
		}
	}

	best := data.Title
	for _, c := range candidates {
		// prefer the longest non-trivial caption
		if len(c) > len(best) {
			best = c
		}
	}
	data.Title = best

	// For photo posts we already have ImageURL/ImageURLs, return even if no video URLs
	if data.ImageURL != "" || len(data.ImageURLs) > 0 {
		return data, nil
	}
	if data.HDURL == "" && data.SDURL == "" {
		return nil, fmt.Errorf("no video URLs found in page")
	}

	return data, nil
}

// findCaptionAnchoredToID looks for the caption belonging to a specific videoID.
// Search only before ID marker for closest message, to avoid picking next video's caption.
func findCaptionAnchoredToID(body []byte, videoID string) string {
	if videoID == "" {
		return ""
	}
	idMarker := []byte(`"id":"` + videoID + `"`)
	var best string
	bestDist := 1000000
	for offset := 0; ; {
		idx := bytes.Index(body[offset:], idMarker)
		if idx == -1 {
			break
		}
		absIdx := offset + idx
		start := absIdx - 15000
		if start < 0 {
			start = 0
		}
		window := body[start:absIdx]
		all := messagePattern.FindAllSubmatch(window, -1)
		for i := len(all) - 1; i >= 0; i-- {
			m := all[i]
			if len(m) < 2 {
				continue
			}
			pos := bytes.Index(window, m[0])
			if pos != -1 {
				cs := pos - 500
				if cs < 0 {
					cs = 0
				}
				if strings.Contains(string(window[cs:pos]), "context_layout") {
					continue
				}
			}
			candidate := string(m[1])
			if len(candidate) < 3 {
				continue
			}
			candidate = cleanFacebookCaption(candidate)
			if candidate == "" {
				continue
			}
			dist := absIdx - (start + pos)
			if best == "" || dist < bestDist || (dist == bestDist && len(candidate) > len(best)) {
				best = candidate
				bestDist = dist
			}
		}
		offset = absIdx + len(idMarker)
	}
	if best != "" {
		return best
	}
	if videoID != "" {
		if pure := findPureCaptionAnchored(body, videoID); pure != "" {
			return pure
		}
	}
	if pure := findPureFacebookCaption(body); pure != "" {
		return pure
	}
	return ""
}


// findPureCaptionAnchored searches pure caption path anchored near specific videoID
func findPureCaptionAnchored(body []byte, videoID string) string {
	idMarker := []byte(`"id":"` + videoID + `"`)
	for offset := 0; ; {
		idx := bytes.Index(body[offset:], idMarker)
		if idx == -1 {
			break
		}
		absIdx := offset + idx
		// Look backwards 20KB for creation_story block belonging to this video
		start := absIdx - 20000
		if start < 0 {
			start = 0
		}
		window := body[start:absIdx]
		// Search creation_story within this anchored window (closest to ID)
		// Take last occurrence of creation_story in window
		csIdx := bytes.LastIndex(window, []byte(`"creation_story"`))
		if csIdx != -1 {
			csAbs := start + csIdx
			end := csAbs + 6000
			if end > len(body) {
				end = len(body)
			}
			csWindow := body[csAbs:end]
			if bytes.Contains(csWindow, []byte(`"comet_sections"`)) && bytes.Contains(csWindow, []byte(`"message"`)) {
				matches := messagePattern.FindAllSubmatch(csWindow, -1)
				for i := len(matches) - 1; i >= 0; i-- {
					m := matches[i]
					if len(m) < 2 {
						continue
					}
					matchPos := bytes.Index(csWindow, m[0])
					if matchPos == -1 {
						continue
					}
					checkStart := matchPos - 500
					if checkStart < 0 {
						checkStart = 0
					}
					preceding := string(csWindow[checkStart:matchPos])
					if strings.Contains(preceding, "context_layout") {
						continue
					}
					candidate := string(m[1])
					if len(candidate) >= 3 {
						return candidate
					}
				}
			}
		}
		// Also try content path in same anchored window
		contentIdx := bytes.LastIndex(window, []byte(`"content"`))
		if contentIdx != -1 {
			cAbs := start + contentIdx
			end := cAbs + 6000
			if end > len(body) {
				end = len(body)
			}
			cWindow := body[cAbs:end]
			if bytes.Contains(cWindow, []byte(`"comet_sections"`)) && bytes.Contains(cWindow, []byte(`"message"`)) {
				matches := messagePattern.FindAllSubmatch(cWindow, -1)
				for i := len(matches) - 1; i >= 0; i-- {
					m := matches[i]
					if len(m) < 2 {
						continue
					}
					matchPos := bytes.Index(cWindow, m[0])
					checkStart := matchPos - 500
					if checkStart < 0 {
						checkStart = 0
					}
					preceding := string(cWindow[checkStart:matchPos])
					if strings.Contains(preceding, "context_layout") {
						continue
					}
					candidate := string(m[1])
					if len(candidate) >= 3 {
						return candidate
					}
				}
			}
		}
		offset = absIdx + len(idMarker)
	}
	return ""
}

// findPureFacebookCaption uses Facebook data structure path creation_story.comet_sections.message.story.message.text
// and content path comet_sections.content.story.comet_sections.message.text
// It searches for pure caption without metadata prefix
func findPureFacebookCaption(body []byte) string {
	// Search for creation_story -> comet_sections -> message -> story -> message -> text
	// We do sequential byte scans to avoid heavy regex backtrack
	// Find "creation_story"
	searchStart := 0
	for {
		idx := bytes.Index(body[searchStart:], []byte(`"creation_story"`))
		if idx == -1 {
			break
		}
		absIdx := searchStart + idx
		// Look ahead 6KB for comet_sections -> message -> story -> message -> text
		end := absIdx + 6000
		if end > len(body) {
			end = len(body)
		}
		window := body[absIdx:end]
		// quick check contains sequence
		if bytes.Contains(window, []byte(`"comet_sections"`)) && bytes.Contains(window, []byte(`"message"`)) {
			// Extract message text via regex within this small window (cheap)
			// Look for "message":{"text":"..."} but prefer the one inside story.message
			// Use our messagePattern on window, take last (closest to story)
			matches := messagePattern.FindAllSubmatch(window, -1)
			if len(matches) > 0 {
				// The pure caption is typically the last message in creation_story block
				// Filter out those that are inside context_layout (check preceding 200 bytes)
				for i := len(matches) - 1; i >= 0; i-- {
					m := matches[i]
					if len(m) < 2 {
						continue
					}
					// Find position of this match
					matchPos := bytes.Index(window, m[0])
					if matchPos == -1 {
						continue
					}
					// Check 400 bytes before for context_layout (dirty)
					checkStart := matchPos - 400
					if checkStart < 0 {
						checkStart = 0
					}
					preceding := string(window[checkStart:matchPos])
					if strings.Contains(preceding, "context_layout") {
						continue // skip dirty category
					}
					candidate := string(m[1])
					if len(candidate) >= 3 {
						return candidate
					}
				}
			}
		}
		searchStart = absIdx + len(`"creation_story"`)
		// Only need first few creation_story blocks
		if searchStart > 500000 { // limit search to first 500KB to avoid scanning whole 2MB
			break
		}
	}

	// Second attempt: content path comet_sections.content.story.comet_sections.message.text
	searchStart = 0
	for {
		idx := bytes.Index(body[searchStart:], []byte(`"content"`))
		if idx == -1 {
			break
		}
		absIdx := searchStart + idx
		end := absIdx + 6000
		if end > len(body) {
			end = len(body)
		}
		window := body[absIdx:end]
		if bytes.Contains(window, []byte(`"comet_sections"`)) && bytes.Contains(window, []byte(`"message"`)) {
			matches := messagePattern.FindAllSubmatch(window, -1)
			if len(matches) > 0 {
				for i := len(matches) - 1; i >= 0; i-- {
					m := matches[i]
					if len(m) < 2 {
						continue
					}
					matchPos := bytes.Index(window, m[0])
					checkStart := matchPos - 400
					if checkStart < 0 {
						checkStart = 0
					}
					preceding := string(window[checkStart:matchPos])
					if strings.Contains(preceding, "context_layout") {
						continue
					}
					candidate := string(m[1])
					if len(candidate) >= 3 {
						return candidate
					}
				}
			}
		}
		searchStart = absIdx + len(`"content"`)
		if searchStart > 500000 {
			break
		}
	}

	return ""
}

// findVideoSection returns the slice of body containing the video delivery
// data for the given videoID, anchored by dash_mpd_debug.mpd?v=VIDEO_ID
// and bounded by the closing "id":"VIDEO_ID".
func findVideoSection(body []byte, videoID string) []byte {
	if videoID == "" {
		return nil
	}

	anchor := []byte("dash_mpd_debug.mpd?v=" + videoID)
	start := bytes.Index(body, anchor)
	if start == -1 {
		return nil
	}

	remaining := body[start:]

	// look for "id":"VIDEO_ID" which closes the videoDeliveryResponseResult block
	endMarker := []byte(`"id":"` + videoID + `"`)
	endIdx := bytes.Index(remaining, endMarker)
	if endIdx > 0 {
		return remaining[:endIdx+len(endMarker)]
	}

	// fallback: take a generous window
	maxLen := 20000
	if maxLen > len(remaining) {
		maxLen = len(remaining)
	}
	return remaining[:maxLen]
}

func unescapeFacebookURL(s string) string {
	s = strings.ReplaceAll(s, `\/`, "/")
	s = unescapeUnicode(s)
	s = html.UnescapeString(s)
	return s
}

// cleanFacebookCaption mimics IG/Threads clean caption approach.
// FB often returns "PageName\n\nActual caption" in message/creation_story fields,
// where first part may be page category (short). Uses dedicated
// EdgeMediaToCaption field that is pure caption; we strip the prefix for FB to match.
func cleanFacebookCaption(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Remove zero-width unicode characters (LRM, RLM, ZWSP, etc.) that FB injects
	s = strings.Map(func(r rune) rune {
		switch r {
		case 0x200E, 0x200F, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// FB JSON encodes newlines as \n literal, not real newline - decode them
	// Also handle double-escaped \\n from HTML-embedded JSON
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\r", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Normalize: \r\n -> \n (after decoding)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	// Unescape \/ -> / (FB escapes slashes in JSON)
	s = strings.ReplaceAll(s, "\\/", "/")
	// Split by double newline – FB separates page name and caption with \n\n
	parts := strings.Split(s, "\n\n")
	if len(parts) >= 2 {
		// New: if any later part contains distinctive meme series marker "Random memes from my phone"
		// or contains hashtags #reels etc and first part short, treat earlier parts as prefix and return from marker onwards
		lowerFull := strings.ToLower(s)
		// check for series marker
		if idx := strings.Index(lowerFull, "random memes from my phone"); idx != -1 {
			for i, p := range parts {
				if strings.Contains(strings.ToLower(p), "random memes from my phone") {
					rest := strings.TrimSpace(strings.Join(parts[i:], "\n\n"))
					if rest != "" {
						return rest
					}
					break
				}
			}
		}
		first := strings.TrimSpace(parts[0])
		// If first part itself contains single newline, e.g. category newline caption
		// then first line of it may be the page name
		if strings.Contains(first, "\n") {
			subParts := strings.Split(first, "\n")
			subFirst := strings.TrimSpace(subParts[0])
			subRest := strings.TrimSpace(strings.Join(subParts[1:], "\n"))
			restAll := subRest
			if len(parts) > 1 {
				restAll = restAll + "\n\n" + strings.TrimSpace(strings.Join(parts[1:], "\n\n"))
			}
			restAll = strings.TrimSpace(restAll)
			if subFirst != "" && restAll != "" && looksLikeFBPageName(subFirst) && len(restAll) > len(subFirst) {
				return restAll
			}
		}
		rest := strings.TrimSpace(strings.Join(parts[1:], "\n\n"))
		if first == "" || rest == "" {
			// fall through
		} else {
			if isOnlyHashtags(rest) {
				return s
			}
			if looksLikeFBPageName(first) && len(rest) > len(first) {
				return rest
			}
			if !strings.Contains(first, "#") && !strings.Contains(first, "❌") && !strings.Contains(first, "✅") && strings.Contains(rest, "#") {
				restWithoutTags := stripHashtags(rest)
				if len(strings.TrimSpace(restWithoutTags)) >= 10 {
					if len(first) <= 80 {
						wordsFirst := len(strings.Fields(first))
						if wordsFirst <= 6 {
							return rest
						}
					}
				}
			}
		}
		return s
	}
	// Fallback: try single newline split if first line looks like page name
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 {
		first := strings.TrimSpace(lines[0])
		rest := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if first != "" && rest != "" {
			lowerRest := strings.ToLower(rest)
			if strings.Contains(lowerRest, "random memes from my phone") {
				if looksLikeFBPageName(first) || len(strings.Fields(first)) <= 6 {
					if !isOnlyHashtags(rest) {
						return rest
					}
				}
			}
			if len(strings.Fields(first)) <= 2 && strings.Contains(rest, "#") {
				if looksLikeFBPageName(first) && len(rest) > len(first) {
					if !isOnlyHashtags(rest) {
						return rest
					}
				}
			}
		}
	}
	return s
}

func isOnlyHashtags(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return false
	}
	hashtagCount := 0
	for _, t := range tokens {
		if strings.HasPrefix(t, "#") {
			hashtagCount++
		}
	}
	if float64(hashtagCount)/float64(len(tokens)) >= 0.7 {
		for _, t := range tokens {
			if !strings.HasPrefix(t, "#") && len(t) > 3 {
				return false
			}
		}
		return true
	}
	return false
}

func stripHashtags(s string) string {
	tokens := strings.Fields(s)
	var kept []string
	for _, t := range tokens {
		if !strings.HasPrefix(t, "#") {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, " ")
}

func looksLikeFBPageName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len(s) > 80 {
		return false
	}
	if strings.Contains(s, "#") {
		return false
	}
	if strings.Contains(s, "http://") || strings.Contains(s, "https://") {
		return false
	}
	if strings.Contains(s, "\n") {
		return false
	}
	if strings.Contains(s, "❌") || strings.Contains(s, "✅") {
		return false
	}
	words := strings.Fields(s)
	if len(words) <= 4 {
		if len(s) <= 40 {
			return true
		}
	}
	lower := strings.ToLower(s)
	junkPrefixes := []string{"general", "meme", "funny", "reels", "viral"}
	for _, jp := range junkPrefixes {
		if strings.HasPrefix(lower, jp) && len(s) <= 40 {
			return true
		}
	}
	if len(words) <= 6 && len(s) <= 50 && !strings.Contains(s, ".") && !strings.Contains(s, "?") && !strings.Contains(s, "!") {
		if len(words) <= 4 {
			return true
		}
		if len(s) <= 30 {
			return true
		}
	}
	return false
}

// probeFormatSize asks the Facebook CDN for the size of a progressive
// video URL. We send a HEAD first; some FBCDN edges reject HEAD with
// 405, so we fall back to a single-byte ranged GET. Any unrecoverable
// error returns 0 -- callers must treat that as "size unknown" and
// fall back to a different format.
func probeFormatSize(ctx *models.ExtractorContext, url string) int64 {
	if url == "" {
		return 0
	}

	if size := probeContentLength(ctx, http.MethodHead, url, nil); size > 0 {
		return size
	}

	rangeHeaders := map[string]string{
		"User-Agent": videoFetchHeaders["User-Agent"],
		"Accept":     "*/*",
		"Range":      "bytes=0-0",
	}
	return probeContentLength(ctx, http.MethodGet, url, rangeHeaders)
}

func probeContentLength(
	ctx *models.ExtractorContext,
	method string,
	url string,
	headers map[string]string,
) int64 {
	if headers == nil {
		headers = map[string]string{
			"User-Agent": videoFetchHeaders["User-Agent"],
			"Accept":     "*/*",
		}
	}
	resp, err := ctx.Fetch(
		method,
		url,
		&networking.RequestParams{Headers: headers},
	)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent {
		if total := parseContentRangeTotal(resp.Header.Get("Content-Range")); total > 0 {
			return total
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength > 0 {
		return resp.ContentLength
	}
	return 0
}

func parseContentRangeTotal(header string) int64 {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	idx := strings.LastIndex(header, "/")
	if idx == -1 {
		return 0
	}
	totalPart := strings.TrimSpace(header[idx+1:])
	if totalPart == "" || totalPart == "*" {
		return 0
	}
	var total int64
	for _, c := range totalPart {
		if c < '0' || c > '9' {
			return 0
		}
		total = total*10 + int64(c-'0')
	}
	return total
}

func unescapeUnicode(s string) string {
	// Facebook embeds JSON inside HTML, so backslashes are themselves
	// escaped: "\uD83E\uDD40" arrives as "\\uD83E\\uDD40". Normalise the
	// double backslash form back to a single one before decoding.
	s = strings.ReplaceAll(s, `\\u`, `\u`)
	s = strings.ReplaceAll(s, `\\/`, `/`)

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		if i+5 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
			r, ok := decodeHex4(s[i+2 : i+6])
			if ok {
				// High surrogate? Look ahead for a following \uXXXX low surrogate.
				if utf16.IsSurrogate(r) && i+11 < len(s) &&
					s[i+6] == '\\' && s[i+7] == 'u' {
					if r2, ok2 := decodeHex4(s[i+8 : i+12]); ok2 {
						if dec := utf16.DecodeRune(r, r2); dec != unicode.ReplacementChar {
							b.WriteRune(dec)
							i += 12
							continue
						}
					}
				}
				if utf8.ValidRune(r) {
					b.WriteRune(r)
					i += 6
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// decodeHex4 parses a 4-char hex sequence (e.g. "D83E") into a rune.
func upgradeFBImageToHD(u string) string {
	if u == "" {
		return u
	}
	if !strings.Contains(u, "scontent") {
		return u
	}
	// For dst-webp_p394x394_q70 URLs, upgrading stp breaks oh= signature (403 mismatch)
	// Only upgrade images that have ctp= (cp0_dst-jpg...ctp=p600) which keeps signature valid
	// Test: p600 with ctp -> 78KB 200 OK, p394 dst-webp -> 403 mismatch
	if strings.Contains(u, "p394x394") && strings.Contains(u, "dst-webp") {
		return u // keep original, don't break signature
	}
	if strings.Contains(u, "p843x403") {
		return u // also breaks sig
	}
	upgraded := fbImageSizePattern.ReplaceAllStringFunc(u, func(m string) string {
		sub := fbImageSizePattern.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		var w int
		fmt.Sscanf(sub[1], "%d", &w)
		if w >= 1080 {
			return m
		}
		return "p1080x1080"
	})
	return upgraded
}

func decodeHex4(h string) (rune, bool) {
	var r rune
	for j := 0; j < 4; j++ {
		r <<= 4
		c := h[j]
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return r, true
}
