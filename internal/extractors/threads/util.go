package threads

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

var headers = map[string]string{
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
	"Accept-Language": "en-GB,en;q=0.9",
	"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
}

// videoVersion represents a single video quality entry from Threads JSON
type videoVersion struct {
	Type int    `json:"type"`
	URL  string `json:"url"`
}

// imageCandidate represents a single image candidate from Threads JSON
type imageCandidate struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// postCodeRe matches the `"code":"<shortcode>"` marker that prefixes every
// post/reply object embedded in a Threads page (root post first, then replies).
var postCodeRe = regexp.MustCompile(`"code":"[a-zA-Z0-9_-]{5,}"`)

// extractPostSection bounds the parsed region to the ORIGINAL post only.
//
// A Threads post page embeds the root post followed by EVERY reply, and each
// reply object carries its own "code" + image_versions2/video_versions arrays.
// Scanning the whole page therefore harvests media from replies/comments too,
// which is the "extra media" bug: a post that is a single video came back with
// the video plus photos/videos taken from the comment thread.
//
// We cut the body at the post's own "code" marker and stop before the next
// (different) "code" marker, which is exactly where the reply stream starts.
// If the marker cannot be located we fall back to the whole body so nothing
// regresses on unusual page shapes.
func extractPostSection(s, code string) string {
	if len(code) < 5 {
		return s
	}
	marker := `"code":"` + code + `"`
	start := strings.Index(s, marker)
	if start < 0 {
		return s
	}
	rest := s[start+len(marker):]
	if loc := postCodeRe.FindStringIndex(rest); loc != nil {
		return s[start : start+len(marker)+loc[0]]
	}
	return s[start:]
}

func ParsePostMedia(ctx *models.ExtractorContext, body []byte) (*models.Media, error) {
	s := string(body)
	if strings.Contains(s, "Thread not available") || strings.Contains(s, "not available") {
		return nil, util.ErrUnavailable
	}

	// Only consider the original post; skip media belonging to replies.
	post := extractPostSection(s, ctx.ContentID)

	media := ctx.NewMedia()

	// Extract caption from JSON: "caption":{"text":"..."}
	caption := extractCaption(post)
	media.SetCaption(caption)

	videoURLs := extractVideoURLs(post)
	imageURLs := extractImageURLs(post)

	if len(videoURLs) > 0 {
		// Video post: emit the video formats and use the post's poster frame
		// (image_versions2) as the thumbnail, matching the Instagram extractor.
		item := media.NewItem()
		formats := make([]*models.MediaFormat, 0, len(videoURLs))
		for i, u := range videoURLs {
			fmtID := fmt.Sprintf("video_%d", i)
			if i == 0 {
				fmtID = "video" // best quality first
			}
			formats = append(formats, &models.MediaFormat{
				Type:       database.MediaTypeVideo,
				FormatID:   fmtID,
				URL:        []string{u},
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
			})
		}
		if len(imageURLs) > 0 && imageURLs[0] != "" {
			formats[0].ThumbnailURL = []string{imageURLs[0]}
		}
		item.AddFormats(formats...)
	} else {
		// Photo post (single image or carousel): one item per image.
		for _, u := range imageURLs {
			item := media.NewItem()
			item.AddFormats(&models.MediaFormat{
				Type:     database.MediaTypePhoto,
				FormatID: "image",
				URL:      []string{u},
			})
		}
	}

	if len(media.Items) == 0 {
		return nil, fmt.Errorf("no media found in post page")
	}

	return media, nil
}

func extractCaption(s string) string {
	// "caption":{"text":"..."} — handle unicode escapes
	re := regexp.MustCompile(`"caption":\{"text":"((?:[^"\\]|\\.)*)"`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	// Unescape JSON string
	var caption string
	if err := json.Unmarshal([]byte(`"`+m[1]+`"`), &caption); err != nil {
		return m[1] // fallback raw
	}
	return caption
}

func extractVideoURLs(s string) []string {
	// Find "video_versions":[{"type":101,"url":"..."},{"type":102,...}]
	re := regexp.MustCompile(`"video_versions":\[([^\]]+)\]`)
	var urls []string
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		var versions []videoVersion
		if err := json.Unmarshal([]byte("["+m[1]+"]"), &versions); err != nil {
			continue
		}
		// Sort by type descending (higher type = better quality usually)
		// type 103 > 102 > 101
		for i := len(versions) - 1; i >= 0; i-- {
			u := versions[i].URL
			if u != "" && !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}
	return urls
}

func extractImageURLs(s string) []string {
	// Find "image_versions2":{"candidates":[{"width":640,"height":360,"url":"..."},...]}
	re := regexp.MustCompile(`"image_versions2":\{"candidates":\[([^\]]+)\]`)
	var urls []string
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		var candidates []imageCandidate
		if err := json.Unmarshal([]byte("["+m[1]+"]"), &candidates); err != nil {
			continue
		}
		// Get the largest candidate
		if len(candidates) > 0 {
			best := candidates[0]
			for _, c := range candidates {
				if c.Width > best.Width {
					best = c
				}
			}
			if best.URL != "" && !seen[best.URL] {
				seen[best.URL] = true
				urls = append(urls, best.URL)
			}
		}
	}
	return urls
}
