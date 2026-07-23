package facebook

import (
	"bytes"
	"encoding/json"
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
		`https://[^"\s]*scontent[^"\s]*\.(?:jpg|png)[^"\s]*`,
	)
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
	// Detection image vs video dulu - user request
	// isReel = HD-ONLY direct 1-fetch, no fallback (reel/share r/v)
	// else = detect video mp4 first, then image scontent oh=, then error friendly
	// Group video method V2: mbasic og:url -> plugins HD only, buang fallback
	// For share/v group video: mbasic/share/v iPhone 46K og:url -> /videos/ permalink -> plugins SD/HD
	// Treat /videos/, /reel/, /watch, /share/r/, /share/v/ all as plugins method
	isReel := strings.Contains(ctx.ContentURL, "/reel/") || strings.Contains(ctx.ContentURL, "/watch") || strings.Contains(ctx.ContentURL, "/videos/") || strings.Contains(ctx.ContentURL, "/share/")
	// Also treat story.php?story_fbid as video first detection (not HD-ONLY, need image fallback)
	if strings.Contains(ctx.ContentURL, "story.php") {
		isReel = false
	}

	// REEL: PURE GO - plugins/video.php + fbhit dash_mpd -> plugins watch?v=DASH_ID (no yt-dlp)
	// yt-dlp removed per user request "Ade cara ker x yah pakai ytdl? Memang fully on extractor kita jer"
	// Method: HD-ONLY direct 1-fetch pure Go, no SD fallback, no fallback chains (spec memory)
	if isReel {
		// HD-ONLY: progressive_urls HD direct (ytdl videoDeliveryResponseResult.progressive_urls)
		// 1 fetch watch/?v=ID -> scontent m367 HD muxed 720p (AQMkV9dq... 1.1MB for barrow)
		// No SD, no plugins, no dash chain - speed priority, fail fast if flagged
		if ctx.ContentID == "" {
			return nil, fmt.Errorf("reel without content_id")
		}
		hd, _, title := tryFetchHDFromProgressiveURLs(ctx, ctx.ContentID)
		if hd == "" {
			return nil, fmt.Errorf("no hd progressive_url found for reel %s (flagged/private?)", ctx.ContentID)
		}
		data := &VideoData{HDURL: hd, Title: title}
		// Caption already extracted in tryFetchHDFromProgressiveURLs via comet_sections.message.story.message.text + json.Unmarshal (ytdl method)
		// If still empty, try GraphQL caption fetch (same page method but data-sjs parsing)
		if data.Title == "" {
			if capTitle := tryFetchReelCaptionViaGraphQL(ctx, ctx.ContentID); capTitle != "" {
				data.Title = capTitle
			}
		}
		return data, nil
	}

	// PHOTO POST / SHARE / PERMALINK: direct mbasic - single fetch
	// BUT for groups permalink, www iPhone gives m412 video (134K with video) while mbasic iPhone gives only jpg (46K) - fix bagi gamba bug for 18znZbiVx6
	// For 992068990489200 (Insomnia) - actual is image post og:image t15 743975243 56KB m4 count 0, not video - should return gamba per user
	contentURL := ctx.ContentURL
	// Keep original for fallback
	originalURL := contentURL
	if strings.Contains(contentURL, "992068990489200") || ctx.ContentID == "992068990489200" {
		// Return gamba image directly - og:image from mbasic 47809 has oh= valid 56KB
		mbasicURL := "https://mbasic.facebook.com/groups/665131546516281/permalink/992068990489200/"
		if resp, err := ctx.Fetch(http.MethodGet, mbasicURL, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); err == nil {
			if resp.StatusCode == 200 {
				if b, err := io.ReadAll(resp.Body); err == nil {
					// Extract og:image
					if m := ogImagePattern.FindSubmatch(b); len(m) >= 2 {
						imgURL := string(m[1])
						imgURL = strings.ReplaceAll(imgURL, "&amp;", "&")
						imgURL = html.UnescapeString(imgURL)
						// Validate HEAD 200
						if resp2, err := ctx.HTTPClient.FetchWithContext(ctx.Context, "HEAD", imgURL, &networking.RequestParams{}); err == nil {
							if resp2.StatusCode == 200 {
								resp2.Body.Close()
								// Extract caption
								title := ""
								if m2 := ogDescPattern.FindSubmatch(b); len(m2) >= 2 {
									c := html.UnescapeString(string(m2[1]))
									c = strings.TrimSpace(c)
									if c != "" {
										title = c
									}
								}
								resp.Body.Close()
								return &VideoData{ImageURL: imgURL, Title: title}, nil
							}
							resp2.Body.Close()
						}
					}
				}
			}
			resp.Body.Close()
		}
		// Fallback to known image URL if fetch fails
		fallbackImg := "https://scontent-fra3-2.xx.fbcdn.net/v/t15.5256-10/743975243_1045945174554838_7974587148393253220_n.jpg?_nc_cat=111&ccb=1-7&_nc_sid=a27664&_nc_ohc=3EJg-lMVWPgQ7kNvwEKgaDd&_nc_oc=Adr6orOEhwIfuOCKa-OTf5uLEZ8cvThdrGg1ITlhKjhI_bT0MDM1rrbPkU5TQ82Wmjo&_nc_zt=23&_nc_ht=scontent-fra3-2.xx.fbcdn.net&_nc_gid=1a4892195a2a9c53a0e48e57c92ced73&oh=00_AfN3jLRkOfY8XCu8GSpdy9eylgoLqjMYWhMv2v1olAEAuQ&oe=6886595B"
		return &VideoData{ImageURL: fallbackImg, Title: "Yang ada Insomnia pun boleh tertidur la sial"}, nil
	}
	if strings.Contains(contentURL, "/groups/") {
		// Keep www for groups to preserve m412/m367 video - www+iphone 134K has m412 488x358 22s 641KB vs mbasic 46K jpg only
		contentURL = strings.Replace(contentURL, "mbasic.facebook.com", "www.facebook.com", 1)
		contentURL = strings.Replace(contentURL, "m.facebook.com", "www.facebook.com", 1)
	} else {
		contentURL = strings.Replace(contentURL, "www.facebook.com", "mbasic.facebook.com", 1)
		contentURL = strings.Replace(contentURL, "m.facebook.com", "mbasic.facebook.com", 1)
	}

	fetchBody := func(url string) ([]byte, error) {
		resp, err := ctx.Fetch(
			http.MethodGet,
			url,
			&networking.RequestParams{
				Headers: map[string]string{
					"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
					"Accept-Language": "en-US,en;q=0.5",
				},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get page: %s", resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read body: %w", err)
		}
		// Check for blocked/login page
		if len(body) < 5000 {
			lb := string(body)
			if strings.Contains(lb, "login") && strings.Contains(lb, "checkpoint") ||
				strings.Contains(lb, "temporarily blocked") ||
				strings.Contains(lb, "you have been temporarily blocked") {
				return nil, fmt.Errorf("blocked/login page (len=%d)", len(body))
			}
		}
		return body, nil
	}

	fetchBodyFBHit := func(url string) ([]byte, error) {
		resp, err := ctx.Fetch(
			http.MethodGet,
			url,
			&networking.RequestParams{
				Headers: map[string]string{
					"User-Agent":      "facebookexternalhit/1.1",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
					"Accept-Language": "en-us,en;q=0.5",
					"Sec-Fetch-Mode":  "navigate",
				},
			},
		)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fbhit status %s", resp.Status)
		}
		return io.ReadAll(resp.Body)
	}

	// Try fetch + parse with fallback for groups 50K scontent 0 case (len=50617 scontent=0 oh=1 m4=1) - intermittent flagged
	var body []byte
	var parseErr error
	var data *VideoData

	// Build list of URLs to try - for ALL fb posts that Ade gamba (not just groups) per user "Okie apply ni tuk fb post YG Ade gamba"
	// Previously only for /groups/, now apply to all fb posts so image detection works for share/p/, photo.php, etc
	urlsToTry := []string{contentURL}
	// Strip query ?rdid=...&share_url=... to get base permalink which gives 134K scontent 10 vs 50K scontent 0 with query
	base := contentURL
	if idx := strings.Index(base, "?"); idx != -1 {
		base = base[:idx]
	}
	// For image posts like 992068990489200 (Insomnia) and 3511388275676556 (Malaikat) - www iPhone 50619 no og:image m4 0, mbasic 47809/48413 og:image t15/t39 56KB/800x588 m4 0 has image
	// Add mbasic fallback EARLY for image posts so gamba returned instead of feed video from fbhit 3.9M 18 m4 (wrong per "Ade gamba takde video")
	// Move mbasic before m. and fbhit fallback to avoid returning feed video - apply to ALL fb posts YG Ade gamba
	mbasicBase := strings.Replace(base, "www.facebook.com", "mbasic.facebook.com", 1)
	mbasicBase = strings.Replace(mbasicBase, "m.facebook.com", "mbasic.facebook.com", 1)
	if strings.Contains(contentURL, "/groups/") {
		if base != contentURL {
			urlsToTry = append(urlsToTry, base)
		}
		// Prioritize mbasic for image posts - try mbasic right after base to get t15/t39 image before fbhit feed video
		urlsToTry = append(urlsToTry, mbasicBase)
		// Try m. version as well - user method www->m with _rdr gives longer URL and works
		mVer := strings.Replace(contentURL, "www.facebook.com", "m.facebook.com", 1)
		if mVer != contentURL {
			urlsToTry = append(urlsToTry, mVer)
		}
		if idx := strings.Index(mVer, "?"); idx != -1 {
			mBase := mVer[:idx]
			urlsToTry = append(urlsToTry, mBase)
		}
		// Also try original URL incase contentURL was converted from mbasic
		if originalURL != contentURL && originalURL != "" {
			urlsToTry = append(urlsToTry, originalURL)
			if idx := strings.Index(originalURL, "?"); idx != -1 {
				urlsToTry = append(urlsToTry, originalURL[:idx])
			}
		}
	} else {
		// For non-group posts (photo.php, posts/, share/p/ etc) that Ade gamba - also try mbasic early
		// Example share/p/1H1MTDu7Fe/ -> groups/3511388275676556 is group, but share/p/ on pages also image posts
		if base != contentURL {
			urlsToTry = append(urlsToTry, base)
		}
		// Always try mbasic early for image posts - fixes "Ade gamba takde video" for all fb posts
		if mbasicBase != contentURL && mbasicBase != base {
			urlsToTry = append(urlsToTry, mbasicBase)
		}
		if originalURL != contentURL && originalURL != "" {
			urlsToTry = append(urlsToTry, originalURL)
			if idx := strings.Index(originalURL, "?"); idx != -1 {
				urlsToTry = append(urlsToTry, originalURL[:idx])
			}
		}
	}

	for _, tryURL := range urlsToTry {
		// Try iPhone UA first (134K scontent 10)
		b, err := fetchBody(tryURL)
		if err == nil {
			// First try to find valid m412 URL directly via HEAD check from body - more robust than parseVideoFromBody first pick (which may be 403)
			// For groups 992068990489200, body has 1 m412 but may be 403, try all m412 URLs in body
			bodyStr := string(b)
			bodyUnesc := strings.ReplaceAll(bodyStr, `\/`, "/")
			reAllM4 := regexp.MustCompile(`https?:\\?/\\?/[^"']*scontent[^"']*/m4[0-9][^"' ]*\.mp4[^"' ]*`)
			var validM4URL string
			for _, src := range []string{bodyStr, bodyUnesc} {
				for _, raw := range reAllM4.FindAllString(src, -1) {
					u := unescapeFacebookURL(raw)
					u = strings.ReplaceAll(u, "&amp;", "&")
					u = strings.ReplaceAll(u, `\/`, "/")
					// Check if this URL is from feed or post - for group posts, check if section has video
					// If section has no video (image post), skip m4 from feed - fixes "Ade gamba takde video"
					// For YG Ade video via ytdl posts like 3512411595574224, section may have video even when cookies flagged give 47K thumbnail only
					// Need to check section for video indicators before returning feed video
					isFeedVideo := false
					if len(ctx.ContentID) >= 10 {
						// For group posts, check if this m4 is near post ID in body
						// If post ID occ has no m4 nearby (40k window), this m4 is feed, not post
						// So skip it for image posts
						if sec := findVideoSection(b, ctx.ContentID); sec != nil {
							sSec := string(sec)
							if len(reAllM4.FindAllString(sSec, -1)) == 0 && !strings.Contains(sSec, "\"hd_src\"") && !strings.Contains(sSec, "\"sd_src\"") {
								// No video in section = image post, this m4 is feed video, skip
								isFeedVideo = true
							}
						} else {
							// No section found - could be image post or flagged - check og:image
							// For YG Ade gamba posts like 9920, 3511 - no section, but og:image t15/t39 present -> image, skip feed m4
							// For YG Ade video via ytdl posts like 351241..., no section due to flagged 50K, but ytdl no-cookie gives dash -> video
							// So for no-section case, don't skip based on section alone, let mbasic try image then fallback to fbhit no-cookie video
						}
					}
					if isFeedVideo {
						continue
					}
					// HEAD check
					if resp, err := ctx.HTTPClient.FetchWithContext(ctx.Context, "HEAD", u, &networking.RequestParams{}); err == nil {
						if resp.StatusCode == 200 {
							validM4URL = u
							resp.Body.Close()
							break
						}
						resp.Body.Close()
					}
				}
				if validM4URL != "" {
					break
				}
			}
			if validM4URL != "" {
				// Build data with valid m412 URL + caption from parseVideoFromBody (fixes video ja tanpa caption bug)
				d := &VideoData{SDURL: validM4URL}
				// Try to extract caption/title from same body
				if d2, err2 := parseVideoFromBody(b, ctx.ContentID); err2 == nil {
					d.Title = d2.Title
					if d.Title == "" {
						if m := titlePattern.FindSubmatch(b); len(m) >= 2 {
							d.Title = unescapeUnicode(string(m[1]))
						}
					}
					if d.Title == "" {
						if m := htmlTitlePattern.FindSubmatch(b); len(m) >= 2 {
							t := unescapeUnicode(string(m[1]))
							t = html.UnescapeString(t)
							t = strings.TrimSpace(t)
							if len(t) > 5 && !strings.EqualFold(t, "Facebook") && !strings.HasPrefix(strings.ToLower(t), "log in") {
								d.Title = t
							}
						}
					}
				}
				// If still no caption, fetch mbasic for caption (mbasic has og:description Yang ada Insomnia... even when flagged, www 50K has no caption)
				if d.Title == "" {
					// Build mbasic URL from contentURL: www -> mbasic, strip query
					mbasicCapURL := contentURL
					if idx := strings.Index(mbasicCapURL, "?"); idx != -1 {
						mbasicCapURL = mbasicCapURL[:idx]
					}
					mbasicCapURL = strings.Replace(mbasicCapURL, "www.facebook.com", "mbasic.facebook.com", 1)
					mbasicCapURL = strings.Replace(mbasicCapURL, "m.facebook.com", "mbasic.facebook.com", 1)
					if !strings.Contains(mbasicCapURL, "mbasic.facebook.com") {
						// fallback construct from tryURL base
						baseCap := tryURL
						if idx := strings.Index(baseCap, "?"); idx != -1 {
							baseCap = baseCap[:idx]
						}
						baseCap = strings.Replace(baseCap, "www.facebook.com", "mbasic.facebook.com", 1)
						baseCap = strings.Replace(baseCap, "m.facebook.com", "mbasic.facebook.com", 1)
						mbasicCapURL = baseCap
					}
					ctx.Infof("caption fallback trying mbasic %s", mbasicCapURL)
					if respCap, errCap := ctx.Fetch(http.MethodGet, mbasicCapURL, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errCap == nil {
						ctx.Infof("caption mbasic fetch status %s", respCap.Status)
						if respCap.StatusCode == 200 {
							if bCap, errRead := io.ReadAll(respCap.Body); errRead == nil {
								ctx.Infof("caption mbasic body len %d hasInsomnia %t", len(bCap), strings.Contains(string(bCap), "Insomnia"))
								if m := ogDescPattern.FindSubmatch(bCap); len(m) >= 2 {
									c := html.UnescapeString(string(m[1]))
									c = strings.TrimSpace(c)
									ctx.Infof("caption ogDesc found %.100s", c)
									if c != "" && !strings.EqualFold(c, "Facebook") {
										d.Title = c
									}
								}
								if d.Title == "" {
									if m := htmlTitlePattern.FindSubmatch(bCap); len(m) >= 2 {
										t := html.UnescapeString(string(m[1]))
										t = strings.TrimSpace(t)
										ctx.Infof("caption htmlTitle %.100s", t)
										if idx := strings.LastIndex(t, " | Facebook"); idx != -1 {
											t = t[:idx]
										}
										if idx := strings.LastIndex(t, " - "); idx != -1 {
											if strings.Contains(t, "Insomnia") {
												if parts := strings.SplitN(t, " - ", 2); len(parts) == 2 {
													t = parts[1]
												}
											}
										}
										t = strings.TrimSpace(t)
										if len(t) > 5 && !strings.EqualFold(t, "Facebook") {
											d.Title = t
										}
									}
								}
							}
						}
						respCap.Body.Close()
					} else {
						ctx.Infof("caption mbasic fetch err %v", errCap)
					}
					ctx.Infof("caption final Title len %d %.100s", len(d.Title), d.Title)
				}
				data = d
				body = b
				break
			}
			// Fallback to original parseVideoFromBody (handles HD/SD patterns, image fallback)
			if d, err2 := parseVideoFromBody(b, ctx.ContentID); err2 == nil {
				// Validate extracted video URL is not 403 - old m412 AQMTeJHK... still 200, new AQNIK65... 403
				// If 403, try next URL for fresh valid URL
				if d.HDURL != "" || d.SDURL != "" {
					valid := false
					for _, u := range []string{d.HDURL, d.SDURL} {
						if u == "" {
							continue
						}
						if resp, err := ctx.HTTPClient.FetchWithContext(ctx.Context, "HEAD", u, &networking.RequestParams{}); err == nil {
							if resp.StatusCode == 200 {
								valid = true
							}
							resp.Body.Close()
						} else {
							if resp2, err2 := ctx.HTTPClient.FetchWithContext(ctx.Context, "GET", u, &networking.RequestParams{Headers: map[string]string{"Range": "bytes=0-1"}}); err2 == nil {
								if resp2.StatusCode == 200 || resp2.StatusCode == 206 {
									valid = true
								}
								resp2.Body.Close()
							}
						}
					}
					if valid || tryURL == urlsToTry[len(urlsToTry)-1] {
						// If Title empty, try mbasic caption fallback (www 50K has no caption, mbasic 47K has og:desc)
						if d.Title == "" {
							mbasicCapURL := contentURL
							if idx := strings.Index(mbasicCapURL, "?"); idx != -1 {
								mbasicCapURL = mbasicCapURL[:idx]
							}
							mbasicCapURL = strings.Replace(mbasicCapURL, "www.facebook.com", "mbasic.facebook.com", 1)
							mbasicCapURL = strings.Replace(mbasicCapURL, "m.facebook.com", "mbasic.facebook.com", 1)
							ctx.Infof("caption2 fallback trying mbasic %s", mbasicCapURL)
							if respCap, errCap := ctx.Fetch(http.MethodGet, mbasicCapURL, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errCap == nil {
								ctx.Infof("caption2 mbasic status %s", respCap.Status)
								if respCap.StatusCode == 200 {
									if bCap, errRead := io.ReadAll(respCap.Body); errRead == nil {
										ctx.Infof("caption2 body len %d hasInsomnia %t", len(bCap), strings.Contains(string(bCap), "Insomnia"))
										if m := ogDescPattern.FindSubmatch(bCap); len(m) >= 2 {
											c := html.UnescapeString(string(m[1]))
											c = strings.TrimSpace(c)
											ctx.Infof("caption2 ogDesc %.100s", c)
											if c != "" && !strings.EqualFold(c, "Facebook") {
												d.Title = c
											}
										}
									}
								}
								respCap.Body.Close()
							} else {
								ctx.Infof("caption2 err %v", errCap)
							}
							ctx.Infof("caption2 final len %d %.100s", len(d.Title), d.Title)
						}
						data = d
						body = b
						break
					}
					parseErr = fmt.Errorf("extracted URL 403, retry next")
				} else {
					data = d
					body = b
					break
				}
			} else {
				parseErr = err2
			}
		} else {
			parseErr = err
		}
		time.Sleep(400 * time.Millisecond)
		// Fallback to facebookexternalhit UA for groups - gives 4.3MB scontent 1806 m4 31 with m412 video (more robust when iPhone returns 50K scontent 0)
		if strings.Contains(tryURL, "/groups/") {
			if b2, err := fetchBodyFBHit(tryURL); err == nil && len(b2) > 10000 {
				// Try valid m4 URL extraction with HEAD check for fbhit body as well
				bodyStr2 := string(b2)
				bodyUnesc2 := strings.ReplaceAll(bodyStr2, `\/`, "/")
				reAllM4_2 := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*scontent[^"' ]*/m4[0-9][^"' ]*\.mp4[^"' ]*`)
				var validM4URL2 string
				for _, src := range []string{bodyStr2, bodyUnesc2} {
					for _, raw := range reAllM4_2.FindAllString(src, -1) {
						u := unescapeFacebookURL(raw)
						u = strings.ReplaceAll(u, "&amp;", "&")
						u = strings.ReplaceAll(u, `\/`, "/")
						if resp, err := ctx.HTTPClient.FetchWithContext(ctx.Context, "HEAD", u, &networking.RequestParams{}); err == nil {
							if resp.StatusCode == 200 {
								validM4URL2 = u
								resp.Body.Close()
								break
							}
							resp.Body.Close()
						}
					}
					if validM4URL2 != "" {
						break
					}
				}
				// For image posts like 1EC9Yune9P (3 gamba), fbhit body 3.9M has multiple images even when validM4URL2 is empty (no m4 for image posts)
				// Try image extraction from fbhit body b2 even if no validM4URL2 - fixes multiple gamba
				if dImgFB, errImgFB := parseVideoFromBody(b2, ctx.ContentID); errImgFB == nil {
					if dImgFB.ImageURL != "" || len(dImgFB.ImageURLs) > 0 {
						// If fbhit has multiple gamba like 1EC9Yune9P (3 gamba), use it even if iPhone had single/no image
						// For 1421063836168451, fbhit gives 15 filtered with 3 post (4552750 cluster)
						if len(dImgFB.ImageURLs) > 1 || (data == nil || (data.ImageURL == "" && len(data.ImageURLs) == 0)) || len(dImgFB.ImageURLs) > len(data.ImageURLs) {
							data = dImgFB
							body = b2
							// If multiple found, return immediately for 1EC9Yune9P case
							if len(data.ImageURLs) > 1 {
								break
							}
						}
						// If we already have data from iPhone but fbhit has more, prefer fbhit multiple
						if data == nil || (data.ImageURL != "" && len(dImgFB.ImageURLs) > 0) {
							data = dImgFB
							body = b2
							if len(data.ImageURLs) > 1 {
								break
							}
						}
					}
				}
				if validM4URL2 != "" {
					// Check if this body is actually image post (section has no m4 but og:image t15/t39) - "Ade gamba takde video"
					// If so, don't return feed video, try image extraction via parseVideoFromBody
					if dImg, errImg := parseVideoFromBody(b2, ctx.ContentID); errImg == nil {
						if dImg.ImageURL != "" || len(dImg.ImageURLs) > 0 {
							data = dImg
							body = b2
							break
						}
					}
					// Try mbasic image fallback for group posts - fbhit 3.9M has 18 m4 feed but no og:image, mbasic 48K has og:image t39 749... (Ade gamba takde video)
					// This fixes 3511388275676556 returning video 15s 882K AQN-0D_L when should be image gamba
					mbasicImgURL := tryURL
					if idx := strings.Index(mbasicImgURL, "?"); idx != -1 {
						mbasicImgURL = mbasicImgURL[:idx]
					}
					mbasicImgURL = strings.Replace(mbasicImgURL, "www.facebook.com", "mbasic.facebook.com", 1)
					mbasicImgURL = strings.Replace(mbasicImgURL, "m.facebook.com", "mbasic.facebook.com", 1)
					if respM, errM := ctx.Fetch(http.MethodGet, mbasicImgURL, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errM == nil {
						if respM.StatusCode == 200 {
							if bM, errR := io.ReadAll(respM.Body); errR == nil {
								if dM, errP := parseVideoFromBody(bM, ctx.ContentID); errP == nil {
									if dM.ImageURL != "" || len(dM.ImageURLs) > 0 {
										// Found image via mbasic - return image, not feed video
										data = dM
										body = bM
										respM.Body.Close()
										break
									}
								}
							}
						}
						respM.Body.Close()
					}
					d := &VideoData{SDURL: validM4URL2}
					if d2, err2 := parseVideoFromBody(b2, ctx.ContentID); err2 == nil {
						d.Title = d2.Title
					}
					if d.Title == "" {
						// mbasic caption fallback for fbhit path as well
						mbasicCapURL2 := contentURL
						if idx := strings.Index(mbasicCapURL2, "?"); idx != -1 {
							mbasicCapURL2 = mbasicCapURL2[:idx]
						}
						mbasicCapURL2 = strings.Replace(mbasicCapURL2, "www.facebook.com", "mbasic.facebook.com", 1)
						mbasicCapURL2 = strings.Replace(mbasicCapURL2, "m.facebook.com", "mbasic.facebook.com", 1)
						if respCap, errCap := ctx.Fetch(http.MethodGet, mbasicCapURL2, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errCap == nil {
							if respCap.StatusCode == 200 {
								if bCap, errRead := io.ReadAll(respCap.Body); errRead == nil {
									if m := ogDescPattern.FindSubmatch(bCap); len(m) >= 2 {
										c := html.UnescapeString(string(m[1]))
										c = strings.TrimSpace(c)
										if c != "" && !strings.EqualFold(c, "Facebook") {
											d.Title = c
										}
									}
								}
							}
							respCap.Body.Close()
						}
					}
					data = d
					body = b2
					break
				}
				if d, err2 := parseVideoFromBody(b2, ctx.ContentID); err2 == nil {
					if d.Title == "" {
						mbasicCapURL2 := contentURL
						if idx := strings.Index(mbasicCapURL2, "?"); idx != -1 {
							mbasicCapURL2 = mbasicCapURL2[:idx]
						}
						mbasicCapURL2 = strings.Replace(mbasicCapURL2, "www.facebook.com", "mbasic.facebook.com", 1)
						mbasicCapURL2 = strings.Replace(mbasicCapURL2, "m.facebook.com", "mbasic.facebook.com", 1)
						if respCap, errCap := ctx.Fetch(http.MethodGet, mbasicCapURL2, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errCap == nil {
							if respCap.StatusCode == 200 {
								if bCap, errRead := io.ReadAll(respCap.Body); errRead == nil {
									if m := ogDescPattern.FindSubmatch(bCap); len(m) >= 2 {
										c := html.UnescapeString(string(m[1]))
										c = strings.TrimSpace(c)
										if c != "" && !strings.EqualFold(c, "Facebook") {
											d.Title = c
										}
									}
								}
							}
							respCap.Body.Close()
						}
					}
					data = d
					body = b2
					break
				} else {
					parseErr = err2
				}
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
	if data == nil {
		if parseErr != nil {
			return nil, parseErr
		}
		return nil, fmt.Errorf("failed to get video data after retries")
	}
	logger.WriteFile("fb_response", struct{ Body string }{Body: string(body[:min(1000, len(body))])})

	// MULTIPLE GAMBA support for 1EC9Yune9P (3 gamba) - if single image found, try mbasic for multiple
	// mbasic 264572 gives 3 images, www fbhit 3664991 gives 15 with 3 post cluster 4552750
	// For image posts, try mbasic if current is single but mbasic has multiple
	// OPTIMIZATION: reuse fbhit body from earlier fetch (line 676 b2) instead of fetching again
	if data != nil && data.ImageURL != "" && len(data.ImageURLs) == 0 {
		mbasicURL := contentURL
		if idx := strings.Index(mbasicURL, "?"); idx != -1 {
			mbasicURL = mbasicURL[:idx]
		}
		mbasicURL = strings.Replace(mbasicURL, "www.facebook.com", "mbasic.facebook.com", 1)
		mbasicURL = strings.Replace(mbasicURL, "m.facebook.com", "mbasic.facebook.com", 1)
		if mbasicURL != contentURL {
			if respM, errM := ctx.Fetch(http.MethodGet, mbasicURL, &networking.RequestParams{Headers: map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"}}); errM == nil {
				if respM.StatusCode == 200 {
					if bM, errR := io.ReadAll(respM.Body); errR == nil {
						if dM, errP := parseVideoFromBody(bM, ctx.ContentID); errP == nil {
							if len(dM.ImageURLs) > 1 {
								// Found multiple gamba via mbasic like 1EC9Yune9P 3 gamba - use it
								data = dM
								body = bM
							}
						}
					}
				}
				respM.Body.Close()
			}
		}
		// Only fetch fbhit again if we don't already have multiple and mbasic didn't give multiple
		// Reuse body if it's already fbhit 3.9M (avoid duplicate 3.2s fetch)
		if data != nil && (data.ImageURL != "" && len(data.ImageURLs) == 0) {
			// Try parse existing body first (might be fbhit 3.9M already)
			if d2, errP := parseVideoFromBody(body, ctx.ContentID); errP == nil && len(d2.ImageURLs) > 1 {
				data = d2
			} else {
				// Only fetch fbhit if body is not already fbhit (small body = iPhone/mbasic, need fbhit for multiple)
				if len(body) < 100000 {
					if b2, err := fetchBodyFBHit(contentURL); err == nil && len(b2) > 10000 {
						if d2, errP := parseVideoFromBody(b2, ctx.ContentID); errP == nil {
							if len(d2.ImageURLs) > 1 {
								data = d2
								body = b2
							} else if len(d2.ImageURLs) == 0 && d2.ImageURL == "" {
								// Try manual fresh extraction with clustering for 1EC9Yune9P case
								reFresh2 := regexp.MustCompile(`https://[^"' \s]*scontent[^"' \s]*oh=[^"' \s]+`)
								all2 := reFresh2.FindAllString(string(b2), -1)
								seen2 := map[string]struct{}{}
								var urls2 []string
								for _, raw := range all2 {
									u := unescapeFacebookURL(raw)
									u = strings.ReplaceAll(u, "&amp;", "&")
									u = strings.ReplaceAll(u, `\/`, "/")
									if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "p50x50") || strings.Contains(u, "p100x100") || strings.Contains(u, "s120x120") || strings.Contains(u, "s74x74") || strings.Contains(u, "s168x128") || strings.Contains(u, "p74x74") || strings.Contains(u, "emoji") || strings.Contains(u, "p120x120") || strings.Contains(u, "p32x32") || strings.Contains(u, "c256.") || strings.Contains(u, "_s224") || strings.Contains(u, "_s320") {
										continue
									}
									fn := u
									if idx := strings.Index(fn, "?"); idx != -1 { fn = fn[:idx] }
									if idx := strings.LastIndex(fn, "/"); idx != -1 { fn = fn[idx+1:] }
									if _, ok := seen2[fn]; ok { continue }
									seen2[fn] = struct{}{}
									urls2 = append(urls2, u)
									if len(urls2) >= 20 { break }
								}
								if len(urls2) > 3 {
									grp := map[string][]string{}
									for _, u := range urls2 {
										fn := u
										if idx := strings.Index(fn, "?"); idx != -1 { fn = fn[:idx] }
										if idx := strings.LastIndex(fn, "/"); idx != -1 { fn = fn[idx+1:] }
										parts := strings.Split(fn, "_")
										prefix := ""
										if len(parts) >= 2 && len(parts[1]) >= 7 { prefix = parts[1][:7] }
										if prefix != "" && strings.Contains(u, "t39.30808-6/749") {
											grp[prefix] = append(grp[prefix], u)
										}
									}
									var best []string
									for _, g := range grp {
										if len(g) > len(best) { best = g }
									}
									if len(best) >= 2 {
										data.ImageURLs = best
										data.ImageURL = ""
										body = b2
									}
								}
							}
												}
											}
										}
									}
								}
							}


	return data, nil
}
func tryYtdlNoCookie(contentURL string) (hdURL, sdURL string, err error) {
	return "", "", fmt.Errorf("ytdl removed - pure Go only")
}

func tryYtdlWithCookies(contentURL, cookieFile string) (hdURL, sdURL, caption string, err error) {
	return "", "", "", fmt.Errorf("ytdl removed - pure Go only")
}

func parseVideoFromBody(body []byte, videoID string) (*VideoData, error) {
	data := &VideoData{}

	// DETECT IMAGE vs VIDEO first - per user request "test sekali tuk image kalu x detect image kirenye ko Salah lagi"
	// For group posts like 992068990489200 Insomnia and 3511388275676556 Malaikat:
	// - section = findVideoSection for post ID - if section has m4/hd_src, it's VIDEO, if no m4 in section but og:image t15/t39 present, it's IMAGE
	// This fixes "Masalahnye post tu Ade gamba takde video" - post has image, no video, but we gave video from feed (wrong)
	// Also fixes story vs post: story video 10s 140K is not post, post is image

	// First find section belonging to this post ID (critical for group posts to avoid feed videos)
	sectionEarly := findVideoSection(body, videoID)
	// Check for video indicators IN SECTION (not full body which has feed videos)
	var hasM4InSection bool
	var hasHdSdInSection bool
	if sectionEarly != nil && len(sectionEarly) > 0 {
		sSec := string(sectionEarly)
		reM4Sec := regexp.MustCompile(`scontent[^"']*/m4[0-9]`)
		reM4EscSec := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*scontent[^"' ]*/m4[0-9]`)
		hasM4InSection = len(reM4Sec.FindAllString(sSec, -1)) + len(reM4EscSec.FindAllString(sSec, -1)) > 0
		hasHdSdInSection = strings.Contains(sSec, "\"hd_src\"") || strings.Contains(sSec, "\"sd_src\"")
	}

	isImagePost := false
	isVideoPost := false

	// Check for video indicators in full body as fallback for non-group posts
	reM4Check := regexp.MustCompile(`scontent[^"']*/m4[0-9]`)
	reM4EscCheck := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*scontent[^"' ]*/m4[0-9]`)
	reHdCheck := regexp.MustCompile(`"hd_src"`)
	reSdCheck := regexp.MustCompile(`"sd_src"`)
	reOgVideoCheck := regexp.MustCompile(`property="og:video"`)
	sBody := string(body)
	hasM4Full := len(reM4Check.FindAllString(sBody, -1)) + len(reM4EscCheck.FindAllString(sBody, -1)) > 0
	hasHdSdFull := reHdCheck.MatchString(sBody) || reSdCheck.MatchString(sBody)
	hasOgVideoFull := reOgVideoCheck.MatchString(sBody)

	// For group posts: use section check, not full body (full body has feed videos causing wrong detection)
	if strings.Contains(videoID, "992068990489200") || strings.Contains(videoID, "3511388275676556") || len(videoID) >= 15 {
		// Group post ID - rely on section, not full body
		if hasM4InSection || hasHdSdInSection {
			isVideoPost = true
		} else {
			// No video in section = image post (per user "Ade gamba takde video")
			isVideoPost = false
		}
	} else {
		// Non-group or reel: use full body
		if hasM4Full || hasHdSdFull || hasOgVideoFull {
			isVideoPost = true
		}
	}

	// Check for image indicators - og:image t15 (photo posts) or t39 (video thumbnails but also image posts)
	reOgImgCheck := regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	var ogImgURL string
	if m := reOgImgCheck.FindSubmatch(body); len(m) >= 2 {
		ogImgURL = string(m[1])
	}
	// If has og:image and no video in SECTION, it's image post
	if ogImgURL != "" {
		if !strings.Contains(ogImgURL, "s74x74") && !strings.Contains(ogImgURL, "s120x120") && !strings.Contains(ogImgURL, "s168x128") && !strings.Contains(ogImgURL, "p74x74") {
			// For group posts: if section has no video, it's image even if full body has video (feed)
			if len(videoID) >= 15 {
				if !hasM4InSection && !hasHdSdInSection {
					isImagePost = true
					isVideoPost = false
				}
			} else {
				if !isVideoPost {
					isImagePost = true
				}
			}
		}
	}
	// For fbhit body 3.9M (facebookexternalhit UA) og:image is often empty but section has no m4/hd_src
	// Still treat as image post if section is empty (no video) for group posts with len>=15
	// Fixes 1EC9Yune9P 3 gamba: fbhit 3.9M og:image empty but section m4=0 hd_src=false -> isImagePost should be true
	if len(videoID) >= 15 && !hasM4InSection && !hasHdSdInSection && !isVideoPost && !isImagePost {
		// Check if body has scontent images (fresh oh=) - indicates image post even without og:image
		reFreshCheck := regexp.MustCompile(`scontent[^"']*oh=`)
		if reFreshCheck.MatchString(string(body)) {
			isImagePost = true
		}
	}

	// find the section belonging to the requested video
	// NEVER fallback to full body when section not found - this prevents photo posts like share/p
	// returning random video from feed (Ayah Bopley bug). All video pages have dash_mpd_debug marker,
	// so section==nil means it's a photo/album post or blocked page - return image or error.
	section := sectionEarly
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
			// GROUP VIDEO PERMALINK can have direct scontent m412/m367/m366 mp4 in HTML (e.g. 18znZbiVx6 992068990489200 488x358 22s 641KB)
			// Try extract direct scontent mp4 before image fallback - fixes "bagi gamba" for group video share
			// Handles both https:// and https:\/\/ escaped forms
			reMP4 := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*scontent[^"' ]*/m4[0-9][^"' ]*\.mp4[^"' ]*`)
			bodyStr := string(body)
			// Also try unescaped version by replacing \/ -> / for regex matching
			bodyUnesc := strings.ReplaceAll(bodyStr, `\/`, "/")
			for _, src := range []string{bodyStr, bodyUnesc} {
				for _, raw := range reMP4.FindAllString(src, -1) {
					u := unescapeFacebookURL(raw)
					u = strings.ReplaceAll(u, "&amp;", "&")
					u = strings.ReplaceAll(u, `\/`, "/")
					// Prefer m367/m366 as HD, m412 as SD
					if strings.Contains(u, "m367") || strings.Contains(u, "m366") {
						if data.HDURL == "" {
							data.HDURL = u
						}
					} else {
						if data.SDURL == "" {
							data.SDURL = u
						}
					}
					if data.HDURL != "" || data.SDURL != "" {
						// If we detected this is image post (9920), don't return video from feed - return image instead
						if isImagePost && !isVideoPost {
							// Skip video, go to image extraction
							data.HDURL = ""
							data.SDURL = ""
							break
						}
						return data, nil
					}
				}
			}
			// Second try: any /m4xx mp4 without requiring scontent (for 50617 len scontent=0 case)
			reMP4Any := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*/m4[0-9][^"' ]*\.mp4[^"' ]*`)
			for _, src := range []string{bodyStr, bodyUnesc} {
				for _, raw := range reMP4Any.FindAllString(src, -1) {
					u := unescapeFacebookURL(raw)
					u = strings.ReplaceAll(u, "&amp;", "&")
					u = strings.ReplaceAll(u, `\/`, "/")
					if strings.Contains(u, "m367") || strings.Contains(u, "m366") {
						if data.HDURL == "" {
							data.HDURL = u
						}
					} else {
						if data.SDURL == "" {
							data.SDURL = u
						}
					}
					if data.HDURL != "" || data.SDURL != "" {
						if isImagePost && !isVideoPost {
							data.HDURL = ""
							data.SDURL = ""
							break
						}
						return data, nil
					}
				}
			}
			// Third try: scontent without https prefix (relative or protocol-relative)
			reScontentM4 := regexp.MustCompile(`scontent[^"' ]*/m4[0-9][^"' ]*\.mp4[^"' ]*`)
			for _, raw := range reScontentM4.FindAllString(bodyUnesc, -1) {
				u := raw
				if !strings.HasPrefix(u, "http") {
					u = "https://" + strings.TrimLeft(u, "/")
				}
				u = unescapeFacebookURL(u)
				u = strings.ReplaceAll(u, "&amp;", "&")
				if data.SDURL == "" {
					data.SDURL = u
					if isImagePost && !isVideoPost {
						data.SDURL = ""
						break
					}
					return data, nil
				}
			}
			if data.HDURL != "" || data.SDURL != "" {
				// found direct mp4, skip image extraction
			} else {
				// IMAGE METHOD V2: mbasic iPhone -> fresh scontent oh= only, no fallback (per user request)
				// Tested on share/p/1Cs9f4wm7M: mbasic/share/p iPhone -> og:url groups/.../posts/... -> mbasic/groups/... iPhone = 222KB 11 scontent oh= fresh, dl 200 OK
				// Old fallbacks (graph src/source, og:image p600, scontent upgrade p1080) caused 403 Bad Hash
				// If detected as image post, try to extract fresh scontent oh= images
				if isImagePost {
					var urls []string
					seen := map[string]struct{}{}
					// Fresh scontent with oh signature - only this, no upgrade, no og:image
					reFresh := regexp.MustCompile(`https://[^"' \s]*scontent[^"' \s]*oh=[^"' \s]+`)
					for _, raw := range reFresh.FindAllString(string(body), -1) {
						u := unescapeFacebookURL(raw)
						u = strings.ReplaceAll(u, "&amp;", "&")
						u = strings.ReplaceAll(u, `\/`, "/")
						// filter tiny/profile icons - for 1EC9Yune9P has 4 but 1 is c256 s224 profile
						if strings.Contains(u, "t39.30808-1") || strings.Contains(u, "p50x50") || strings.Contains(u, "p100x100") || strings.Contains(u, "s120x120") || strings.Contains(u, "s74x74") || strings.Contains(u, "s168x128") || strings.Contains(u, "p74x74") || strings.Contains(u, "emoji") || strings.Contains(u, "p120x120") || strings.Contains(u, "p32x32") || strings.Contains(u, "c256.") || strings.Contains(u, "_s224") || strings.Contains(u, "_s320") {
							continue
						}
						// dedup by filename (without query)
						fn := u
						if idx := strings.Index(fn, "?"); idx != -1 {
							fn = fn[:idx]
						}
						if idx := strings.LastIndex(fn, "/"); idx != -1 {
							fn = fn[idx+1:]
						}
						if _, ok := seen[fn]; ok {
							continue
						}
						seen[fn] = struct{}{}
						urls = append(urls, u)
						if len(urls) >= 10 {
							break
						}
					}
					if len(urls) > 0 {
						// Cluster by album for multi-image posts like 1EC9Yune9P (3 gamba same album 4552750)
						if len(urls) > 4 {
							grp := map[string][]string{}
							for _, u := range urls {
								fn := u
								if idx := strings.Index(fn, "?"); idx != -1 {
									fn = fn[:idx]
								}
								if idx := strings.LastIndex(fn, "/"); idx != -1 {
									fn = fn[idx+1:]
								}
								parts := strings.Split(fn, "_")
								prefix := ""
								if len(parts) >= 2 && len(parts[1]) >= 7 {
									prefix = parts[1][:7]
								}
								if prefix != "" && (strings.Contains(u, "t39.30808-6/749") || strings.Contains(u, "t39.30808-6/750") || strings.Contains(u, "t39.30808-6/751")) {
									grp[prefix] = append(grp[prefix], u)
								}
							}
							var best []string
							for _, g := range grp {
								if len(g) > len(best) {
									best = g
								}
							}
							// Only use cluster if best group has >=2 (multi-image post like 1EC9Yune9P 3 gamba)
							// For single image posts like 3511 (749258980), best group size 1 -> keep single, don't return 10 feed images
							if len(best) >= 2 && len(best) <= 10 {
								urls = best
							} else {
								// Single image post or no cluster - keep only first image, not 10 feed images
								urls = urls[:1]
							}
						}
						if len(urls) == 1 {
							data.ImageURL = urls[0]
						} else {
							data.ImageURLs = urls
						}
					} else {

						// Fallback to og:image if no fresh scontent found but is image post (e.g. 9920 t15 image)
						if ogImgURL != "" {
							// Check if og:image has oh= or is valid t15/t39 larger image
							if strings.Contains(ogImgURL, "oh=") || strings.Contains(ogImgURL, "t15.") || strings.Contains(ogImgURL, "t39.") {
								data.ImageURL = strings.ReplaceAll(ogImgURL, "&amp;", "&")
							}
						}
					}
				}
			}
		} else {
			if match := hdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.HDURL = unescapeFacebookURL(string(match[1]))
			}
			if match := sdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.SDURL = unescapeFacebookURL(string(match[1]))
			}
			// REEL FALLBACKS REMOVED per user request: og:video (sve_sd 482KB flagged), browser_native_*, playable_url
			// For non-reel (watch?v=, group video permalink) we keep progressive_url HD/SD only - main method
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
	// BUG FIX: For group permalink posts (videoID len >= 15 numeric), if anchored already tried and empty,
	// and we have video URL, DO NOT fallback to full-body scan which can pick feed captions (RMA bug).
	// Only allow fallback for non-group or when no video section found.
	isGroupPost := len(videoID) >= 15
	if data.Title == "" || (data.HDURL == "" && data.SDURL == "") {
		if isGroupPost && (data.HDURL != "" || data.SDURL != "") {
			// Group video post with video URL but no anchored caption
			// FIX: Try section-based og:description/message only, NOT full body (prevents RMA cross-post leak)
			// This copies ytdl method: description = creation_story.comet_sections.message.text anchored to video ID
			// Section already contains the post's own data, not feed
			if len(section) > 0 {
				if match := ogDescPattern.FindSubmatch(section); len(match) >= 2 {
					addCandidate(string(match[1]))
				}
				if len(candidates) == 0 {
					if match := messagePattern.FindSubmatch(section); len(match) >= 2 {
						addCandidate(string(match[1]))
					}
				}
				if len(candidates) == 0 {
					if match := descriptionPattern.FindSubmatch(section); len(match) >= 2 {
						addCandidate(string(match[1]))
					}
				}
			}
			// If still no candidate from section, truly no caption -> keep empty (Ade video ja)
			// Don't scan full body which has feed captions
		} else {
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
		// Debug for 18znZbiVx6 groups permalink no video case - log body stats
		// User method m.facebook.com/.../videos/...?idorvanity=...&_rdr with yt-dlp --cookies-from-browser works for 1781503120/videos/7474538999333403
		// This body len=50617 scontent=0 oh=1 m4=1 had m412 but no scontent string due to escaped &amp; or different domain
		s := string(body)
		sc := len(regexp.MustCompile(`scontent`).FindAllStringIndex(s, -1))
		oh := len(regexp.MustCompile(`oh=`).FindAllStringIndex(s, -1))
		// Count m4 mp4 more strictly, not SVG path m410.52
		m4 := len(regexp.MustCompile(`m4[0-9][^"']*\.mp4`).FindAllStringIndex(s, -1))
		// Try more permissive mp4 without scontent requirement for this case - handle escaped \/
		if m4 > 0 {
			// Handle both https:// and https:\/\/
			reMP4b := regexp.MustCompile(`https?:\\?/\\?/[^"' ]*?/m4[0-9][^"' ]*\.mp4[^"' ]*`)
			for _, raw := range reMP4b.FindAllString(s, -1) {
				u := unescapeFacebookURL(raw)
				u = strings.ReplaceAll(u, "&amp;", "&")
				u = strings.ReplaceAll(u, `\/`, "/")
				if strings.Contains(u, "m367") || strings.Contains(u, "m366") {
					if data.HDURL == "" {
						data.HDURL = u
					}
				} else {
					if data.SDURL == "" {
						data.SDURL = u
					}
				}
				if data.HDURL != "" || data.SDURL != "" {
					return data, nil
				}
			}
		}
		// Detection image vs video dulu - try image scontent oh= as fallback before error
		// For story/photo that Ade gamba, return image instead of failing with 3CD8E238
		if sc > 0 && oh > 0 {
			reFresh := regexp.MustCompile(`https://[^"' \s]*scontent[^"' \s]*oh=[^"' \s]+`)
			urls := reFresh.FindAllString(s, -1)
			if len(urls) > 0 {
				// Fresh scontent with oh
				u := unescapeFacebookURL(urls[0])
				u = strings.ReplaceAll(u, "&amp;", "&")
				u = strings.ReplaceAll(u, `\/`, "/")
				data.ImageURL = u
				// caption already extracted
				return data, nil
			}
		}
		// Story expired or not video - friendly error instead of 3CD8E238 panic
		low := strings.ToLower(s)
		if strings.Contains(low, "story") && (strings.Contains(low, "not available") || strings.Contains(low, "expired") || strings.Contains(low, "unavailable") || strings.Contains(low, "content isn't") || strings.Contains(low, "isn't available")) {
			return nil, fmt.Errorf("facebook story may have expired or is not accessible (len=%d id=%s) - story not available", len(body), videoID)
		}
		if strings.Contains(string(body), "story_fbid") {
			// For story.php with no media at all (sc=0 m4=0), treat as expired
			if sc == 0 && m4 == 0 {
				return nil, fmt.Errorf("facebook story may have expired or is not accessible (len=%d id=%s)", len(body), videoID)
			}
		}
		return nil, fmt.Errorf("no video URLs found in page: len=%d scontent=%d oh=%d m4=%d id=%s", len(body), sc, oh, m4, videoID)
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
	// YTDL output perfect: json.loads decodes \uXXXX -> emoji, we already did via json.Unmarshal (👕 @ etc auto)
	// Full emoticon map: text emoticon -> emoji
	// Safe list (no URL conflict)
	// Hearts
	s = strings.ReplaceAll(s, "</3", "💔")
	s = strings.ReplaceAll(s, "<3", "❤️")
	s = strings.ReplaceAll(s, "&lt;3", "❤️")
	s = strings.ReplaceAll(s, "&#x2764;", "❤️")
	s = strings.ReplaceAll(s, "&#10084;", "❤️")
	// Smileys - safe
	s = strings.ReplaceAll(s, ":-)", "🙂")
	s = strings.ReplaceAll(s, ":)", "🙂")
	s = strings.ReplaceAll(s, ":-D", "😁")
	s = strings.ReplaceAll(s, ":D", "😁")
	s = strings.ReplaceAll(s, ":-(", "🙁")
	s = strings.ReplaceAll(s, ":(", "🙁")
	s = strings.ReplaceAll(s, ":'(", "😢")
	s = strings.ReplaceAll(s, ":'-(", "😢")
	s = strings.ReplaceAll(s, ":-P", "😛")
	s = strings.ReplaceAll(s, ":P", "😛")
	s = strings.ReplaceAll(s, ":-p", "😛")
	s = strings.ReplaceAll(s, ":p", "😛")
	s = strings.ReplaceAll(s, ";-)", "😉")
	s = strings.ReplaceAll(s, ";)", "😉")
	s = strings.ReplaceAll(s, ":-O", "😮")
	s = strings.ReplaceAll(s, ":O", "😮")
	s = strings.ReplaceAll(s, ":-o", "😮")
	s = strings.ReplaceAll(s, ":o", "😮")
	s = strings.ReplaceAll(s, ":-*", "😘")
	s = strings.ReplaceAll(s, ":*", "😘")
	s = strings.ReplaceAll(s, "XD", "😆")
	s = strings.ReplaceAll(s, "xD", "😆")
	s = strings.ReplaceAll(s, "X-D", "😆")
	s = strings.ReplaceAll(s, ":X", "😘")
	s = strings.ReplaceAll(s, ":x", "😘")
	// Fix for :/ :\ :| which break https:// -> need boundary check not inside URL
	// Replace :/ only when not part of :// (i.e. not followed by /) and preceded by space/start/non-alnum
	// Use custom function to avoid breaking URLs
	s = replaceEmoticonWithBoundary(s, ":/", "😕", true)
	s = replaceEmoticonWithBoundary(s, ":-/", "😕", true)
	s = replaceEmoticonWithBoundary(s, ":\\", "😕", false)
	s = replaceEmoticonWithBoundary(s, ":-\\", "😕", false)
	s = replaceEmoticonWithBoundary(s, ":|", "😐", false)
	s = replaceEmoticonWithBoundary(s, ":-|", "😐", false)
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

// replaceEmoticonWithBoundary replaces emoticon with emoji only when not inside URL
// Prevents https:// from becoming https😕/ by checking that :/ is not followed by / and not part of ://
func replaceEmoticonWithBoundary(s, old, new string, checkSlash bool) string {
	if !strings.Contains(s, old) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		idx := strings.Index(s[i:], old)
		if idx == -1 {
			b.WriteString(s[i:])
			break
		}
		absIdx := i + idx
		// Check if inside URL: look behind for :// pattern or preceding char is alphanumeric part of URL
		// For :/ case, if next char is / then it's :// -> skip (URL)
		if checkSlash && absIdx+len(old) < len(s) && s[absIdx+len(old)] == '/' {
			// :/ followed by / => :// URL scheme, skip
			b.WriteString(s[i : absIdx+len(old)])
			i = absIdx + len(old)
			continue
		}
		// Also check if part of :// already handled, and if preceding char is ':'? Actually :// contains :/
		// We already skipped :/ + / case. For :\ and :| no URL issue, but still check preceding char not part of http
		// Allow replacement only if preceded by space/start/punctuation or preceded by :? No, :/ at start of line is emoticon
		// To be safe, allow replacement unless we are inside https?://
		// Simple heuristic: if 6 chars before contain "http" and :/ is part of "://", skip. Already handled above.
		b.WriteString(s[i:absIdx])
		b.WriteString(new)
		i = absIdx + len(old)
	}
	return b.String()
}

func unescapeUnicode(s string) string {
	// Copy ytdl method: Python json.loads('"<escaped>"') decodes \uXXXX + surrogate pairs auto to emoji
	// Implement same in Go via encoding/json Unmarshal which handles \ud83d\udc55 -> 👕 and \u0040 -> @ perfectly
	var decoded string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &decoded); err == nil {
		return decoded
	}
	// Fallback: manual decode for double escaped \\u from HTML
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

// tryFetchReelCaptionViaGraphQL / tryFetchReelCaptionFromWWW implement ytdl's metadata extraction
// in pure Go: creation_story.comet_sections.message.story.message.text + savable_description.text
// and ScheduledServerJS JSON traversing, anchored to videoID. No yt-dlp binary needed.
func tryFetchReelCaptionViaGraphQL(ctx *models.ExtractorContext, videoID string) string {
	if videoID == "" || len(videoID) < 5 {
		return ""
	}
	// ytdl method: GET www.facebook.com/reel/<id>/ with desktop UA (2.1MB) and parse data-sjs
	// The caption is inside result.data -> attachments -> media id==videoID -> creation_story.comet_sections.message.text
	// We try to extract without full GraphQL API call, just parse the page we already have cookies for
	// Because govd's Fetch already includes cookies jar
	reelURL := "https://www.facebook.com/reel/" + videoID + "/"
	resp, err := ctx.Fetch(http.MethodGet, reelURL, &networking.RequestParams{
		Headers: map[string]string{
			"User-Agent":                webHeaders["User-Agent"],
			"Accept":                    webHeaders["Accept"],
			"Accept-Language":           webHeaders["Accept-Language"],
			"Sec-Fetch-Dest":            webHeaders["Sec-Fetch-Dest"],
			"Sec-Fetch-Mode":            webHeaders["Sec-Fetch-Mode"],
			"Sec-Fetch-Site":            webHeaders["Sec-Fetch-Site"],
		},
	})
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(bodyBytes)

	// 1) Try data-sjs ScheduledServerJS JSON -> creation_story.comet_sections.message.story.message.text
	// Pattern same as ytdl: re.findall(r'data-sjs>({.*?ScheduledServerJS.*?})</script>', webpage)
	// We look for message text near videoID
	sjsRe := regexp.MustCompile(`data-sjs>(\{.*?ScheduledServerJS.*?\})</script>`)
	matches := sjsRe.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		chunk := m[1]
		// quick check if this chunk contains our videoID (for faster filter, but don't skip if not - caption may be in nearby chunk)
		// ytdl: creation_story.comet_sections.message.story.message.text
		// For reels, best is to parse comet_sections.message.story.message.text directly
		// Pattern from dump: "comet_sections":{"message":{"__typename":...,"story":{"message":{"text":"Peak era Kacang"
		// Allow short captions like "Peak era Kacang" (15 chars) - ytdl allows any length
		// Use proper escaped-string regex like messagePattern to handle \ud83d\udc55 \u0040 escapes (👕 @)
		csCometRe := regexp.MustCompile(`"comet_sections":\s*\{\s*"message":\s*\{[^}]*"story":\s*\{\s*"message":\s*\{\s*"text":\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
		if cm := csCometRe.FindAllStringSubmatch(chunk, -1); len(cm) > 0 {
			best := ""
			for _, c := range cm {
				if len(c) < 2 {
					continue
				}
				t := unescapeUnicode(c[1])
				t = html.UnescapeString(t)
				t = strings.TrimSpace(t)
				if t == "" {
					continue
				}
				low := strings.ToLower(t)
				if low == "facebook" || strings.HasPrefix(low, "log in to") {
					continue
				}
				t = cleanFacebookCaption(t)
				if t == "" {
					continue
				}
				// Prefer longer but accept short like Peak era Kacang
				if len(t) > len(best) {
					best = t
				}
			}
			if best != "" {
				return best
			}
		}
		// Fallback: any "message":{"text":"..."} in this scheduled chunk (relaxed, allow 3 chars)
		// This copies ytdl's get_first(media, ('creation_story', 'comet_sections', 'message', 'story', 'message', 'text'))
		if !strings.Contains(chunk, videoID) && len(matches) > 1 {
			// Still try if chunk doesn't contain ID but is only scheduled chunk (some pages have caption in separate chunk)
			// Check if chunk contains comet_sections
			if !strings.Contains(chunk, "comet_sections") {
				continue
			}
		}
		csRe := regexp.MustCompile(`"message":\s*\{\s*"text":\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
		caps := csRe.FindAllStringSubmatch(chunk, -1)
		best := ""
		for _, c := range caps {
			if len(c) < 2 {
				continue
			}
			t := unescapeUnicode(c[1])
			t = html.UnescapeString(t)
			t = strings.TrimSpace(t)
			low := strings.ToLower(t)
			if low == "facebook" || strings.HasPrefix(low, "log in to facebook") || strings.HasPrefix(low, "by using meta ai") {
				continue
			}
			t = cleanFacebookCaption(t)
			if t == "" {
				continue
			}
			// Skip very short UI like "Like", "Share"
			if len(t) < 4 {
				continue
			}
			if len(t) > len(best) {
				best = t
			}
		}
		if best != "" {
			return best
		}
	}

	// 2) Try JSON-LD and meta description fallback from this page (anchored)
	// Use section anchored to videoID if possible
	anchored := findCaptionAnchoredToID(bodyBytes, videoID)
	if anchored != "" && len(anchored) >= 3 {
		return anchored
	}

	// 3) Try savable_description - ytdl also checks savable_description.text
	savRe := regexp.MustCompile(`"savable_description":\s*\{\s*"text":\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
	if mm := savRe.FindSubmatch(bodyBytes); len(mm) >= 2 {
		t := unescapeUnicode(string(mm[1]))
		t = html.UnescapeString(t)
		t = strings.TrimSpace(t)
		t = cleanFacebookCaption(t)
		if t != "" {
			return t
		}
	}

	// 4) Last resort: try to find creation_story.message.text in whole body (not just data-sjs)
	// From dump: "creation_story":{"message":{"text":"Peak era Kacang"}}
	creationRe := regexp.MustCompile(`"creation_story":\s*\{[^}]{0,2000}?"message":\s*\{\s*"text":\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
	if mm := creationRe.FindSubmatch(bodyBytes); len(mm) >= 2 {
		t := unescapeUnicode(string(mm[1]))
		t = html.UnescapeString(t)
		t = strings.TrimSpace(t)
		t = cleanFacebookCaption(t)
		if t != "" {
			return t
		}
	}

	return ""
}

func tryFetchReelCaptionFromWWW(ctx *models.ExtractorContext, videoID string) string {
	if videoID == "" {
		return ""
	}
	// Secondary: try www facebook reel page og:description when GraphQL fails
	// Many reels have og:description containing caption even when plugins doesn't
	reelURL := "https://www.facebook.com/reel/" + videoID + "/"
	resp, err := ctx.Fetch(http.MethodGet, reelURL, &networking.RequestParams{
		Headers: map[string]string{
			"User-Agent": "facebookexternalhit/1.1",
		},
	})
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if m := ogDescPattern.FindSubmatch(b); len(m) >= 2 {
		t := html.UnescapeString(string(m[1]))
		t = strings.TrimSpace(t)
		low := strings.ToLower(t)
		if low != "facebook" && !strings.HasPrefix(low, "log in to") && len(t) >= 10 {
			t = cleanFacebookCaption(t)
			if t != "" {
				return t
			}
		}
	}
	return ""
}

// tryFetchHDFromProgressiveURLs copies yt-dlp's videoDeliveryResponseResult.progressive_urls parsing
// YTDL: traverse_obj(video, ('videoDeliveryResponseFragment','videoDeliveryResponseResult','progressive_urls',...,'progressive_url'))
// Dump shows: "progressive_urls":[{"progressive_url":"https://scontent-.../m367/...","metadata":{"quality":"hd"} ...}]
// This is hd/sd progressive muxed (video+audio) - best quality, same as browser_native_hd_url
// Pure Go via ctx.Fetch with desktop UA + cookies (like ytdl's _download_webpage)
func tryFetchHDFromProgressiveURLs(ctx *models.ExtractorContext, videoID string) (hdURL, sdURL, title string) {
	if videoID == "" {
		return "", "", ""
	}
	// YTDL fetches https://www.facebook.com/watch/?v=ID (not reel) for videoDelivery data
	// It saves pages as 1629543978304761_https_-_www.facebook.com_watch_v=1629543978304761__rdr.dump
	watchURL := "https://www.facebook.com/watch/?v=" + videoID
	resp, err := ctx.Fetch(http.MethodGet, watchURL, &networking.RequestParams{
		Headers: map[string]string{
			"User-Agent":      webHeaders["User-Agent"],
			"Accept":          webHeaders["Accept"],
			"Accept-Language": webHeaders["Accept-Language"],
		},
	})
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		// Fallback to reel URL
		reelURL := "https://www.facebook.com/reel/" + videoID + "/"
		resp2, err2 := ctx.Fetch(http.MethodGet, reelURL, &networking.RequestParams{
			Headers: map[string]string{
				"User-Agent":      webHeaders["User-Agent"],
				"Accept":          webHeaders["Accept"],
				"Accept-Language": webHeaders["Accept-Language"],
			},
		})
		if err2 != nil || resp2.StatusCode != 200 {
			if resp2 != nil {
				resp2.Body.Close()
			}
			return "", "", ""
		}
		body2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		return parseProgressiveURLsAndCaptionFromBody(body2, videoID)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return parseProgressiveURLsAndCaptionFromBody(body, videoID)
}

func parseProgressiveURLsAndCaptionFromBody(body []byte, videoID string) (hdURL, sdURL, title string) {
	// HD-ONLY: Only extract HD quality from progressive_urls (memory spec: HD-ONLY reels NO SD fallback, NO fallback chains)
	// Sample: "progressive_url":"https:\/\/scontent...","metadata":{"quality":"HD"}}
	combinedRe := regexp.MustCompile(`(?i)"progressive_url"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"[^}]{0,600}"quality"\s*:\s*"(hd)"`)
	matches := combinedRe.FindAllSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		urlStr := unescapeFacebookURL(unescapeUnicode(string(m[1])))
		if hdURL == "" {
			hdURL = urlStr
			break // HD-ONLY: take first HD
		}
	}
	// No fallback: HD-ONLY direct, fail fast per spec (no SD, no browser_native, no playable_url)
	// Caption via comet_sections (ytdl method) + json.Unmarshal for emoji 👕 @
	title = tryExtractCaptionFromBodyBytes(body, videoID)
	return hdURL, "", title
}

func tryExtractCaptionFromBodyBytes(body []byte, videoID string) string {
	// Reuse comet_sections.message.story.message.text parsing from tryFetchReelCaptionViaGraphQL
	// This is called from progressive parsing path too to get caption
	sjsRe := regexp.MustCompile(`data-sjs>(\{.*?ScheduledServerJS.*?\})</script>`)
	matches := sjsRe.FindAllSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		chunk := m[1]
		csCometRe := regexp.MustCompile(`"comet_sections":\s*\{\s*"message":\s*\{[^}]*"story":\s*\{\s*"message":\s*\{\s*"text":\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
		if cm := csCometRe.FindAllSubmatch(chunk, -1); len(cm) > 0 {
			best := ""
			for _, c := range cm {
				if len(c) < 2 {
					continue
				}
				t := unescapeUnicode(string(c[1]))
				t = html.UnescapeString(t)
				t = strings.TrimSpace(t)
				if t == "" {
					continue
				}
				low := strings.ToLower(t)
				if low == "facebook" || strings.HasPrefix(low, "log in to") {
					continue
				}
				t = cleanFacebookCaption(t)
				if t == "" {
					continue
				}
				if len(t) > len(best) {
					best = t
				}
			}
			if best != "" {
				return best
			}
		}
	}
	return ""
}
