package instagram

// GetWebpageMedia — HTML-first extraction.
//
// The GraphQL `web_info` endpoint (doc_id 27128499623469141) was retired by
// Instagram. Dead doc_ids do not 404; IG redirects the request to
// https://www.instagram.com/ and returns 200 with the logged-in home page.
// GetWebpageMedia therefore used to fail with `400 Bad Request`, and even
// when the request happened to succeed it parsed HTML as JSON and found
// zero items.
//
// This file walks the server-rendered HTML instead. Instagram embeds the
// whole media node in the reel page as escaped JSON:
//
//	"video_versions":[{"type":101,"url":"https:\/\/scontent...mp4?...","width":720,...}]
//
// No browser, no doc_id, no third-party service. Verified working against
// https://www.instagram.com/reel/Db0lSiOkZln/ → HTTP 206, 5,128,739 B,
// magic ftypisom, 720x1280.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

// igHTMLHeaders mirror what a real navigation sends. Accept-Language and the
// Sec-Fetch family matter: Instagram serves a stripped page without them.
var igHTMLHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"Accept-Language":           "en-US,en;q=0.9",
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
	"Referer":                   "https://www.instagram.com/",
}

// extractJSONArray finds `key` in the HTML and returns the balanced [...] that
// follows it. Instagram escapes forward slashes (\/) inside these blobs, so the
// caller unescapes before json.Unmarshal.
func extractJSONArray(html, key string) (string, bool) {
	idx := strings.Index(html, key)
	if idx < 0 {
		return "", false
	}
	rest := html[idx+len(key):]
	// Skip whitespace and the colon separator.
	rest = strings.TrimLeft(rest, " \t\r\n:")
	rest = strings.TrimLeft(rest, " \t\r\n")

	start := strings.IndexByte(rest, '[')
	if start != 0 {
		return "", false
	}

	depth := 0
	inStr := false
	escaped := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return rest[:i+1], true
			}
		}
	}
	return "", false
}

// igVideoVersion is one rendition from `video_versions`.
type igVideoVersion struct {
	Type   int    `json:"type"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// igImageCandidate is one rendition from `image_versions2.candidates`.
type igImageCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// igHTMLPage holds everything we scrape out of one reel/p page.
type igHTMLPage struct {
	VideoVersions []igVideoVersion
	Images        []igImageCandidate
	Caption       string
	Width         int
	Height        int
	Username      string
}

// igCodeMarker is one `"code":"<shortcode>"` occurrence in the page.
type igCodeMarker struct {
	pos  int
	code string
}

// findCodeMarkers lists every shortcode marker in document order. Instagram
// embeds the reel, then suggested posts, so several different shortcodes can
// appear in the same HTML — each owning its own media block.
func findCodeMarkers(html string) []igCodeMarker {
	var out []igCodeMarker
	const key = `"code":"`
	for i := 0; ; {
		j := strings.Index(html[i:], key)
		if j < 0 {
			return out
		}
		start := i + j + len(key)
		end := start
		for end < len(html) && html[end] != '"' {
			end++
		}
		if end < len(html) {
			out = append(out, igCodeMarker{pos: i + j, code: html[start:end]})
		}
		i = end
	}
}

// ownerOf returns the shortcode that owns the media block starting at pos:
// the last marker appearing before it.
func ownerOf(markers []igCodeMarker, pos int) string {
	owner := ""
	for _, m := range markers {
		if m.pos < pos {
			owner = m.code
			continue
		}
		break
	}
	return owner
}

// findOwnedBlocks returns every occurrence of key that belongs to shortcode.
//
// Ownership is NOT simply "the last marker before pos". Measured on reel
// DfUDeEHzDQn: with Instagram's default mobile UA the page renders ONE flat
// item whose "video_versions" sits at offset 566447 while the item's
// `"code":"DfUDeEHzDQn"` sits at 570343 -- the marker comes AFTER the media,
// in the same JSON object. Backward-only ownership found no owner and the reel
// was served as an image. So we accept a block when the nearest marker either
// before OR after it (in document order) is the requested shortcode, and the
// forward window must still be inside the same JSON object (no `}` crossing).
func findOwnedBlocks(html, key, shortcode string) []string {
	markers := findCodeMarkers(html)
	var out []string
	for i := 0; ; {
		j := strings.Index(html[i:], key)
		if j < 0 {
			return out
		}
		pos := i + j
		if raw, ok := extractJSONArray(html[pos:], key); ok {
			if ownsBlockForward(html, markers, pos) == shortcode ||
				ownerOf(markers, pos) == shortcode {
				out = append(out, raw)
			}
		}
		i = pos + len(key)
	}
}

// ownsBlockForward returns the shortcode marker that follows pos, but only if
// it is still inside the same JSON object. The scan from pos aborts the moment
// the brace depth dips below zero — that is the enclosing object closing, and
// any marker past that point belongs to a different item. Final depth alone is
// not enough: `...}]},{` returns to zero after closing the previous item, which
// would let a neighbouring item's marker claim this block.
func ownsBlockForward(html string, markers []igCodeMarker, pos int) string {
	start := -1
	for i, m := range markers {
		if m.pos > pos {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := markers[start].pos
	if end-pos > 8192 {
		return "" // too far to be the same item
	}

	depth := 0
	inStr := false
	esc := false
	for i := pos; i < end; i++ {
		c := html[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return "" // left the enclosing object
			}
		}
	}
	if inStr {
		return "" // string never terminated — treat as malformed
	}
	return markers[start].code
}

// parseIGHTML pulls media out of the server-rendered page. shortcode is used to
// pick the block that belongs to the requested post and ignore suggested ones.
func parseIGHTML(html, shortcode string) (*igHTMLPage, error) {
	page := &igHTMLPage{}

	for _, raw := range findOwnedBlocks(html, `"video_versions"`, shortcode) {
		var versions []igVideoVersion
		if err := json.Unmarshal([]byte(unescapeIGJSON(raw)), &versions); err == nil && len(versions) > 0 {
			page.VideoVersions = append(page.VideoVersions, versions...)
			break
		}
	}

	for _, raw := range findOwnedBlocks(html, `"candidates"`, shortcode) {
		var candidates []igImageCandidate
		if err := json.Unmarshal([]byte(unescapeIGJSON(raw)), &candidates); err == nil && len(candidates) > 0 {
			page.Images = append(page.Images, candidates...)
			break
		}
	}

	if len(page.Images) == 0 {
		for _, raw := range findOwnedBlocks(html, `"display_resources"`, shortcode) {
			var res []igImageCandidate
			if err := json.Unmarshal([]byte(unescapeIGJSON(raw)), &res); err == nil && len(res) > 0 {
				page.Images = append(page.Images, res...)
				break
			}
		}
	}

	page.Caption = extractIGCaption(html)
	page.Username = extractIGUsername(html)
	page.Width, page.Height = extractIGDimensions(html)

	if len(page.VideoVersions) == 0 && len(page.Images) == 0 {
		return nil, fmt.Errorf("no media found in page for %s (blocked, login-walled, or suggested-only page)", shortcode)
	}
	return page, nil
}

// unescapeIGJSON turns Instagram's \/ escaping into plain JSON.
func unescapeIGJSON(s string) string {
	if !strings.Contains(s, `\/`) {
		return s
	}
	return strings.ReplaceAll(s, `\/`, `/`)
}

// extractIGCaption reads og:description and trims Instagram's suffix.
func extractIGCaption(html string) string {
	for _, marker := range []string{`property="og:description" content="`, `name="description" content="`} {
		i := strings.Index(html, marker)
		if i < 0 {
			continue
		}
		rest := html[i+len(marker):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			continue
		}
		cap := htmlUnescape(rest[:end])
		// Trim the "X likes, Y comments - user on date:" preamble Instagram
		// puts in front of the real caption.
		if idx := strings.LastIndex(cap, ": \""); idx >= 0 && idx+3 < len(cap) {
			cap = cap[idx+3:]
			cap = strings.TrimSuffix(cap, `"`)
		}
		cap = strings.TrimSpace(cap)
		if cap != "" {
			return cap
		}
	}
	return ""
}

// extractIGUsername reads the account name out of the title tag.
func extractIGUsername(html string) string {
	const marker = `property="og:title" content="`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	rest := html[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	title := htmlUnescape(rest[:end])
	// "Username on Instagram: "caption""
	if idx := strings.Index(title, " on Instagram"); idx > 0 {
		return strings.TrimSpace(title[:idx])
	}
	return ""
}

// extractIGDimensions reads original_width/original_height when present.
func extractIGDimensions(html string) (int, int) {
	w := extractIGInt(html, `"original_width":`)
	h := extractIGInt(html, `"original_height":`)
	if w == 0 || h == 0 {
		w = extractIGInt(html, `"width":`)
		h = extractIGInt(html, `"height":`)
	}
	return w, h
}

func extractIGInt(html, key string) int {
	i := strings.Index(html, key)
	if i < 0 {
		return 0
	}
	rest := html[i+len(key):]
	n := 0
	seen := false
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
			seen = true
			continue
		}
		if seen {
			break
		}
		if c == ' ' || c == '\t' {
			continue
		}
		return 0
	}
	return n
}

// htmlUnescape resolves the entities Instagram puts in meta tags. Instagram
// encodes non-Latin text as hex numeric entities (&#xe2b; for Thai, CJK, etc.),
// so a plain named-entity replacer leaves the caption unreadable.
func htmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		if end < 0 || end > 12 {
			b.WriteByte(s[i])
			i++
			continue
		}
		entity := s[i+1 : i+end]
		if r, ok := decodeIGEntity(entity); ok {
			b.WriteRune(r)
			i += end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// decodeIGEntity resolves one entity body (the part between & and ;).
func decodeIGEntity(entity string) (rune, bool) {
	switch entity {
	case "quot":
		return '"', true
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "nbsp":
		return ' ', true
	case "apos":
		return '\'', true
	}
	if len(entity) < 2 || entity[0] != '#' {
		return 0, false
	}
	num := entity[1:]
	base := 10
	if num[0] == 'x' || num[0] == 'X' {
		num = num[1:]
		base = 16
	}
	if num == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(num, base, 32)
	if err != nil || v <= 0 || v > 0x10FFFF {
		return 0, false
	}
	return rune(v), true
}

// bestVideoVersion returns the widest rendition, never version[0] — IG does not
// order video_versions by quality.
func bestVideoVersion(v []igVideoVersion) (igVideoVersion, bool) {
	if len(v) == 0 {
		return igVideoVersion{}, false
	}
	best := v[0]
	for _, c := range v[1:] {
		if c.Width > best.Width {
			best = c
		}
	}
	return best, true
}

// bestImageCandidate returns the widest still.
func bestImageCandidate(c []igImageCandidate) (igImageCandidate, bool) {
	if len(c) == 0 {
		return igImageCandidate{}, false
	}
	best := c[0]
	for _, x := range c[1:] {
		if x.Width > best.Width {
			best = x
		}
	}
	return best, true
}

// buildPageURL constructs the canonical reel URL for a shortcode.
func buildPageURL(shortcode string) string {
	return "https://www.instagram.com/reel/" + shortcode + "/"
}

// GetWebpageMedia fetches Instagram media from the server-rendered page.
// This replaces the retired GraphQL web_info path.
func GetWebpageMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	cookies := util.GetExtractorCookies(ctx.Extractor.ID)

	pageURL := buildPageURL(ctx.ContentID)

	resp, err := ctx.Fetch(
		http.MethodGet,
		pageURL,
		&networking.RequestParams{
			Headers: igHTMLHeaders,
			Cookies: cookies,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch page: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read page body: %w", err)
	}

	page, err := parseIGHTML(string(body), ctx.ContentID)
	if err != nil {
		return nil, err
	}

	w, h := page.Width, page.Height
	var dims *Dimensions
	if w > 0 || h > 0 {
		dims = &Dimensions{Width: int32(w), Height: int32(h)}
	}

	if v, ok := bestVideoVersion(page.VideoVersions); ok {
		if v.Width > 0 && v.Height > 0 {
			dims = &Dimensions{Width: int32(v.Width), Height: int32(v.Height)}
		}
		var thumb string
		if img, ok := bestImageCandidate(page.Images); ok {
			thumb = img.URL
		}
		media := &Media{
			Typename:   "XDTGraphVideo",
			IsVideo:    true,
			VideoURL:   v.URL,
			DisplayURL: thumb,
			Caption:    page.Caption,
			ID:         ctx.ContentID,
			Shortcode:  ctx.ContentID,
			Dimensions: dims,
		}
		return ParseGQLMedia(ctx, media)
	}

	if img, ok := bestImageCandidate(page.Images); ok {
		media := &Media{
			Typename:   "XDTGraphImage",
			IsVideo:    false,
			DisplayURL: img.URL,
			Caption:    page.Caption,
			ID:         ctx.ContentID,
			Shortcode:  ctx.ContentID,
		}
		return ParseGQLMedia(ctx, media)
	}

	return nil, fmt.Errorf("no formats found for media item")
}
