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

func ParsePostMedia(ctx *models.ExtractorContext, body []byte) (*models.Media, error) {
	s := string(body)
	if strings.Contains(s, "Thread not available") || strings.Contains(s, "not available") {
		return nil, util.ErrUnavailable
	}

	media := ctx.NewMedia()

	// Extract caption from JSON: "caption":{"text":"..."}
	caption := extractCaption(s)
	media.SetCaption(caption)

	// Extract video URLs from "video_versions":[{"type":101,"url":"..."},...]
	videoURLs := extractVideoURLs(s)
	if len(videoURLs) > 0 {
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
		item.AddFormats(formats...)
	}

	// Extract image URLs from "image_versions2":{"candidates":[...]}
	imageURLs := extractImageURLs(s)
	for _, u := range imageURLs {
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			Type:     database.MediaTypePhoto,
			FormatID: "image",
			URL:      []string{u},
		})
	}

	if len(videoURLs) == 0 && len(imageURLs) == 0 {
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
