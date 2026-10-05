package threads

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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

// ---------------------------------------------------------------------------
// JSON model
//
// A Threads post page embeds the post (and its replies) both as inline JSON
// inside <script type="application/json"> blobs and as escaped strings in the
// HTML body. The authoritative shape is the JSON blob: the post lives at
// `...result.data.media`, with `carousel_media` carrying album children.
//
// The previous implementation scanned the WHOLE body with regexes, which
// (a) harvested media from replies/comments and (b) when bounded to the
// "code" marker, dropped carousel children because those sit BEFORE the
// marker. Parsing the JSON node is exact and fixes both.
// ---------------------------------------------------------------------------

type mediaNode struct {
	Code           string          `json:"code"`
	MediaType      int             `json:"media_type"`
	Caption        *captionNode    `json:"caption"`
	ImageVersions2 *candidatesWrap `json:"image_versions2"`
	VideoVersions  []videoVersion  `json:"video_versions"`
	CarouselMedia  []mediaNode     `json:"carousel_media"`
	TextPostInfo   *textPostInfo   `json:"text_post_app_info"`
}

// textPostInfo carries the share info for text posts. A Threads "text post"
// (media_type 19) has no media of its own: the picture/video lives on the post
// it quotes or reposts, exposed under share_info.
type textPostInfo struct {
	ShareInfo *shareInfo `json:"share_info"`
}

type shareInfo struct {
	QuotedAttachmentPost *mediaNode `json:"quoted_attachment_post"`
	QuotedPost           *mediaNode `json:"quoted_post"`
	RepostedPost         *mediaNode `json:"reposted_post"`
}

type captionNode struct {
	Text string `json:"text"`
}

type candidatesWrap struct {
	Candidates []imageCandidate `json:"candidates"`
}

type videoVersion struct {
	Type int    `json:"type"`
	URL  string `json:"url"`
}

type imageCandidate struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

var (
	jsonScriptRe = regexp.MustCompile(`(?s)<script[^>]*type="application/json"[^>]*>(.*?)</script>`)

	// Fallback regexes (used only when no JSON blob carries the media node).
	postCodeRe      = regexp.MustCompile(`"code":"[a-zA-Z0-9_-]{5,}"`)
	videoVersionsRe = regexp.MustCompile(`"video_versions":\[([^\]]+)\]`)
	imageVersionsRe = regexp.MustCompile(`"image_versions2":\{"candidates":\[([^\]]+)\]`)
)

func ParsePostMedia(ctx *models.ExtractorContext, body []byte) (*models.Media, error) {
	s := string(body)
	if strings.Contains(s, "Thread not available") {
		return nil, util.ErrUnavailable
	}

	media := ctx.NewMedia()

	// Preferred path: parse the embedded JSON post node exactly.
	node, ok := findMediaNode(s, ctx.ContentID)
	if ok {
		appendNodeMedia(media, node)

		// A Threads text post (media_type 19) often carries no media of its
		// own: the picture/video lives on the post it quotes or reposts,
		// exposed under text_post_app_info.share_info. Borrow the caption and
		// media from there so share links to quote posts resolve.
		if len(media.Items) == 0 {
			if shared := firstSharedPost(node); shared != nil {
				if shared.Caption != nil {
					media.SetCaption(shared.Caption.Text)
				}
				appendNodeMedia(media, shared)
				if len(media.Items) > 0 {
					return media, nil
				}
			}
		} else if node.Caption != nil {
			media.SetCaption(node.Caption.Text)
		}

		if len(media.Items) > 0 {
			return media, nil
		}
	}

	// Fallback: bounded regex scan of the original post only.
	post := extractPostSection(s, ctx.ContentID)
	media.SetCaption(extractCaption(post))

	videoURLs := extractVideoURLs(post)
	imageURLs := extractImageURLs(post)
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
		if len(imageURLs) > 0 && imageURLs[0] != "" {
			formats[0].ThumbnailURL = []string{imageURLs[0]}
		}
		item.AddFormats(formats...)
	} else {
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

// appendNodeMedia emits a node's media: one item per carousel child when the
// node is an album, otherwise a single item for the node itself.
func appendNodeMedia(media *models.Media, n *mediaNode) {
	if n == nil {
		return
	}
	if len(n.CarouselMedia) > 0 {
		for i := range n.CarouselMedia {
			appendMediaNode(media, &n.CarouselMedia[i])
		}
		return
	}
	appendMediaNode(media, n)
}

// firstSharedPost returns the post a text post quotes or reposts, if any. The
// media for a media-less text post lives on that shared post.
func firstSharedPost(n *mediaNode) *mediaNode {
	if n == nil || n.TextPostInfo == nil || n.TextPostInfo.ShareInfo == nil {
		return nil
	}
	si := n.TextPostInfo.ShareInfo
	for _, cand := range []*mediaNode{si.QuotedAttachmentPost, si.QuotedPost, si.RepostedPost} {
		if cand != nil {
			return cand
		}
	}
	return nil
}

// appendMediaNode emits one album item for a post/carousel child: a video item
// (formats + poster frame) when video_versions is present, else a photo item.
func appendMediaNode(media *models.Media, n *mediaNode) {
	poster := bestImageURL(n.ImageVersions2)

	if vids := videoURLsFrom(n.VideoVersions); len(vids) > 0 {
		item := media.NewItem()
		formats := make([]*models.MediaFormat, 0, len(vids))
		for i, u := range vids {
			fmtID := fmt.Sprintf("video_%d", i)
			if i == 0 {
				fmtID = "video"
			}
			formats = append(formats, &models.MediaFormat{
				Type:       database.MediaTypeVideo,
				FormatID:   fmtID,
				URL:        []string{u},
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
			})
		}
		if poster != "" {
			formats[0].ThumbnailURL = []string{poster}
		}
		item.AddFormats(formats...)
		return
	}

	if poster != "" {
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			Type:     database.MediaTypePhoto,
			FormatID: "image",
			URL:      []string{poster},
		})
	}
}

func bestImageURL(w *candidatesWrap) string {
	if w == nil {
		return ""
	}
	best := imageCandidate{}
	for _, c := range w.Candidates {
		if c.URL != "" && c.Width >= best.Width {
			best = c
		}
	}
	return best.URL
}

func videoURLsFrom(vs []videoVersion) []string {
	if len(vs) == 0 {
		return nil
	}
	sorted := make([]videoVersion, len(vs))
	copy(sorted, vs)
	// Higher type = better quality; keep best first, dedupe (types 101/102/103
	// of the same video often share one URL).
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Type > sorted[j].Type })
	seen := map[string]bool{}
	var out []string
	for _, v := range sorted {
		if v.URL != "" && !seen[v.URL] {
			seen[v.URL] = true
			out = append(out, v.URL)
		}
	}
	return out
}

// findMediaNode walks every application/json script blob looking for the post
// object whose code matches, exposed under a `media` key (Threads'
// `...result.data.media`). Returns the decoded node.
func findMediaNode(body, code string) (*mediaNode, bool) {
	if len(code) < 5 {
		return nil, false
	}
	for _, m := range jsonScriptRe.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 || !strings.Contains(m[1], code) {
			continue
		}
		var root interface{}
		if err := json.Unmarshal([]byte(m[1]), &root); err != nil {
			continue
		}
		raw, ok := walkPrimary(root, code)
		if !ok {
			raw, ok = walkForMediaFallback(root, code)
		}
		if !ok {
			continue
		}
		buf, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var node mediaNode
		if err := json.Unmarshal(buf, &node); err != nil {
			continue
		}
		if node.Code == code {
			return &node, true
		}
	}
	return nil, false
}

// walkPrimary returns the object exposed under a `media` key whose code matches
// (Threads' `...result.data.media` shape).
func walkPrimary(v interface{}, code string) (map[string]interface{}, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		if m, ok := t["media"].(map[string]interface{}); ok {
			if c, _ := m["code"].(string); c == code {
				return m, true
			}
		}
		for _, val := range t {
			if r, ok := walkPrimary(val, code); ok {
				return r, true
			}
		}
	case []interface{}:
		for _, val := range t {
			if r, ok := walkPrimary(val, code); ok {
				return r, true
			}
		}
	}
	return nil, false
}

func walkForMediaFallback(v interface{}, code string) (map[string]interface{}, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		if c, _ := t["code"].(string); c == code {
			if _, ok := t["image_versions2"]; ok {
				return t, true
			}
			if _, ok := t["video_versions"]; ok {
				return t, true
			}
			if _, ok := t["carousel_media"]; ok {
				return t, true
			}
		}
		for _, val := range t {
			if r, ok := walkForMediaFallback(val, code); ok {
				return r, true
			}
		}
	case []interface{}:
		for _, val := range t {
			if r, ok := walkForMediaFallback(val, code); ok {
				return r, true
			}
		}
	}
	return nil, false
}

// extractPostSection bounds the parsed region to the ORIGINAL post only (used
// by the regex fallback). A Threads page embeds the root post followed by every
// reply; each reply carries its own "code" + media arrays. Scanning the whole
// body therefore harvests media from replies/comments too.
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

func extractCaption(s string) string {
	re := regexp.MustCompile(`"caption":\{"text":"((?:[^"\\]|\\.)*)"`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	var caption string
	if err := json.Unmarshal([]byte(`"`+m[1]+`"`), &caption); err != nil {
		return m[1]
	}
	return caption
}

func extractVideoURLs(s string) []string {
	var urls []string
	seen := map[string]bool{}
	for _, m := range videoVersionsRe.FindAllStringSubmatch(s, -1) {
		var versions []videoVersion
		if err := json.Unmarshal([]byte("["+m[1]+"]"), &versions); err != nil {
			continue
		}
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
	var urls []string
	seen := map[string]bool{}
	for _, m := range imageVersionsRe.FindAllStringSubmatch(s, -1) {
		var candidates []imageCandidate
		if err := json.Unmarshal([]byte("["+m[1]+"]"), &candidates); err != nil {
			continue
		}
		if best := bestImageURL(&candidatesWrap{Candidates: candidates}); best != "" && !seen[best] {
			seen[best] = true
			urls = append(urls, best)
		}
	}
	return urls
}
