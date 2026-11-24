package facebook

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

// A share link of the form share/p/<code> is not always a photo.
//
// Facebook labels every share code /p/ regardless of what the post holds, and
// the extractor used to trust that label: `/share/p/` was routed straight to the
// photo path and never looked for a video. Measured 2026-11-24 on
// share/p/1A5dDnp3j5 -- the post carried two videos (107 s and 44 s) plus a
// photo album, and every path in the bot answered "content isn't available".
//
// So the page is fetched once and asked what it actually contains:
//
//	video_id present  -> one item per video, each resolved through the ordinary
//	                     reel ladder
//	no video_id       -> fall through to the existing photo path unchanged
//
// The ids are collected from the JSON islands that appear once per attachment,
// not from the story_fbid in the URL: a post can carry several videos and
// story_fbid only names the post.
var reSharePVideoID = regexp.MustCompile(`"video_id"\s*:\s*"(\d+)"`)

// sharePVideoIDs returns the distinct video ids carried by a share/p document,
// in the order they appear.
func sharePVideoIDs(body []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reSharePVideoID.FindAllSubmatch(body, -1) {
		id := string(m[1])
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// getSharePMedia handles a share/p/<code> link that carries video attachments.
//
// It returns ok=false when the post holds no video, which tells the caller to
// run the ordinary photo extraction instead of duplicating that logic here.
func getSharePMedia(ctx *models.ExtractorContext) (media *models.Media, ok bool) {
	// The share code must be resolved first: the page it redirects to is where
	// the video ids live. fetchViaCurl already carries the cookie jar and the
	// image-capable Accept header that makes Facebook part with the real page.
	body, err := fetchViaCurl(ctx.Context, ctx.ContentURL)
	if err != nil || len(body) == 0 {
		return nil, false
	}
	// A bare album /p/ link legitimately has no video. Only the presence of
	// video_id distinguishes a mixed post from a photo post.
	ids := sharePVideoIDs(body)
	if len(ids) == 0 {
		return nil, false
	}

	media = &models.Media{}
	if m := reOGDescription.FindSubmatch(body); len(m) >= 2 {
		media.SetCaption(strings.TrimSpace(htmlUnescape(string(m[1]))))
	}

	tgLimit := util.TelegramBotAPIFileLimit()
	safeLimit := tgLimit * 95 / 100

	// Resolve each video through the shared reel ladder. One failure must not
	// kill the rest: a mixed post is common and the parts that did resolve are
	// still what the user asked for.
	var firstErr error
	for _, id := range ids {
		m := fetchReelMedia(ctx, id)
		url := m.HD
		if url == "" {
			url = m.SD
		}
		if url == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("video %s: no progressive_url", id)
			}
			continue
		}
		item := media.NewItem()
		format := &models.MediaFormat{
			FormatID:   "progressive",
			Type:       database.MediaTypeVideo,
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			URL:        []string{url},
			Title:      m.Title,
		}
		item.Formats = append(item.Formats, format)
		if m.DASH != nil && len(m.DASH.Videos) > 0 {
			// The DASH ladder sits above the muxed rendition on some posts.
			// buildDASHFormat owns the budget decision; append its result so
			// the ladder picker sees both.
			dashFormats := buildDASHFormat(
				ctx,
				&VideoData{DASH: m.DASH, DASHDuration: m.DASH.Duration},
				safeLimit,
			)
			item.Formats = append(item.Formats, dashFormats...)
		}
	}

	if len(media.Items) == 0 {
		if firstErr != nil {
			ctx.Infof("facebook share/p: all videos failed: %v", firstErr)
		}
		return nil, false
	}
	return media, true
}

// htmlUnescape covers the entities that appear in og:description text; pulling
// in the html package for five strings would be a heavier dependency than the
// job needs.
func htmlUnescape(s string) string {
	return strings.NewReplacer(
		"&amp;", "&",
		"&quot;", `"`,
		"&#039;", "'",
		"&apos;", "'",
		"&lt;", "<",
		"&gt;", ">",
	).Replace(s)
}
