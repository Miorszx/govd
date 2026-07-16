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
	scontentPattern = regexp.MustCompile(
		`https://[^"]*scontent[^"]*\.(?:jpg|png)`,
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

		reqHeaders := webHeaders
		if isReel {
			reqHeaders = map[string]string{
				"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
				"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"Accept-Language": "en-US,en;q=0.5",
			}
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
		// Even if video URLs found but caption is empty, do NOT fail —
		// video itself is valid, caption is best-effort.
		_ = body
		return data, nil
	}

	// Photo image fetching removed per user request - dont try mbasic/photo.php to avoid single-image for 4 Gambar albums
	// This prevents random Ayah Bopley video and also avoids returning 1 gambar for 4 gambar posts
	if lastErr == nil {
		lastErr = fmt.Errorf("no video URLs found in page")
	}
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
		if len(section) != 0 {
			if match := hdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.HDURL = unescapeFacebookURL(string(match[1]))
			}
			if match := sdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.SDURL = unescapeFacebookURL(string(match[1]))
			}
		}
		if data.HDURL == "" && data.SDURL == "" {
			if match := hdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.HDURL = unescapeFacebookURL(string(match[1]))
			}
			if match := sdURLPattern.FindSubmatch(body); len(match) >= 2 {
				data.SDURL = unescapeFacebookURL(string(match[1]))
			}
		}
		// m.facebook.com direct: og:video contains mp4 with flagged cookies (reel/1056675483359870 gives sve_sd)
		if data.HDURL == "" && data.SDURL == "" {
			if m := regexp.MustCompile(`<meta[^>]+property="og:video"[^>]+content="([^"]+)"`).FindSubmatch(body); len(m) >= 2 {
				u := unescapeFacebookURL(string(m[1]))
				u = strings.ReplaceAll(u, "&amp;", "&")
				if strings.Contains(u, ".mp4") {
					data.SDURL = u
				}
			}
		}
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
	}
	// title can be anywhere in the page
	if match := titlePattern.FindSubmatch(body); len(match) >= 2 {
		data.Title = unescapeUnicode(string(match[1]))
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
