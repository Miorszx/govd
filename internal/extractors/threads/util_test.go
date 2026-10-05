package threads

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
)

func TestMain(m *testing.M) {
	logger.Init()
	os.Exit(m.Run())
}

const (
	testPostCode  = "AAAAPOSTCODE1"
	testCarousel  = "CCCCAROUSEL1"
	testReplyOne  = "BBBBREPLYONE1"
	testReplyTwo  = "BBBBREPLYTWO2"
	postVideoURL  = "https://scontent.example/post-video.mp4"
	postPosterURL = "https://scontent.example/post-poster.jpg"
	carouselImg1  = "https://scontent.example/carousel-1.jpg"
	carouselImg2  = "https://scontent.example/carousel-2.jpg"
	carouselImg3  = "https://scontent.example/carousel-3.jpg"
	carouselImg4  = "https://scontent.example/carousel-4.jpg"
	replyOneURL   = "https://scontent.example/reply-one-image.jpg"
	replyOneVid   = "https://scontent.example/reply-one-video.mp4"
	replyTwoURL   = "https://scontent.example/reply-two-image.jpg"
)

// jsonPage wraps a JSON post object in the <script type="application/json">
// shape Threads uses, with the reply stream as sibling nodes that must never
// be parsed.
func jsonPage(rootJSON string) []byte {
	replyOne := `{"reply":{"code":"` + testReplyOne + `","caption":{"text":"reply one"},` +
		`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + replyOneURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + replyOneVid + `"}],"carousel_media":null}}`
	replyTwo := `{"reply":{"code":"` + testReplyTwo + `","caption":{"text":"reply two"},` +
		`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + replyTwoURL + `"}]},"carousel_media":null}}`
	inner := `{"data":{"media":{` + rootJSON + `},` +
		`"related":{"thread_items":[` +
		`{"post":` + replyOne + `,"line_type":"line"},` +
		`{"post":` + replyTwo + `,"line_type":"line"}` +
		`]}}}`
	// Real pages nest the payload inside require/__bbox wrappers; wrap the valid
	// inner object so the test exercises the deep recursive walk.
	blob := `{"require":[{"__bbox":{"require":[{"__bbox":{"result":` + inner + `}}]}}]}`
	return []byte(`<!doctype html><html><body><script type="application/json">` + blob + `</script></body></html>`)
}

// buildPage mimics the inline (escaped) body shape used by Threads fallback:
// the root post object followed by the reply stream, each carrying its own
// "code" + image_versions2 / video_versions arrays.
func buildPage(rootIsVideo bool) []byte {
	rootMedia := `"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},`
	if rootIsVideo {
		rootMedia += `"video_versions":[{"type":101,"url":"` + postVideoURL + `"},{"type":103,"url":"` + postVideoURL + `"}],`
	}
	page := `<!doctype html><html><body>{"p":{"` +
		`"code":"` + testPostCode + `","caption":{"text":"root caption"},` + rootMedia +
		`"carousel_media":null}},{"reply":{"` +
		`"code":"` + testReplyOne + `","caption":{"text":"reply one"},` +
		`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + replyOneURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + replyOneVid + `"}],"carousel_media":null}},{"reply":{"` +
		`"code":"` + testReplyTwo + `","caption":{"text":"reply two"},` +
		`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + replyTwoURL + `"}]},` +
		`"carousel_media":null}}</body></html>`
	return []byte(page)
}

func newTestContext(code string) *models.ExtractorContext {
	return &models.ExtractorContext{
		ContentID:  code,
		ContentURL: "https://www.threads.com/@tester/post/" + code,
		Extractor:  &models.Extractor{ID: "threads"},
		Context:    context.Background(),
	}
}

// --- JSON blob path (authoritative) -----------------------------------------

// A video post must yield EXACTLY the original video (with poster thumbnail)
// and nothing from the reply stream.
func TestJSONVideoPostExcludesReplies(t *testing.T) {
	root := `"code":"` + testPostCode + `","caption":{"text":"root caption"},` +
		`"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},` +
		`"video_versions":[{"type":101,"url":"` + postVideoURL + `"},{"type":103,"url":"` + postVideoURL + `"}],"carousel_media":null`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 media item (original post only), got %d", len(media.Items))
	}
	formats := media.Items[0].Formats
	if len(formats) != 1 {
		t.Fatalf("expected 1 format (single distinct video), got %d", len(formats))
	}
	f := formats[0]
	if f.Type != database.MediaTypeVideo || f.URL[0] != postVideoURL {
		t.Fatalf("unexpected video format: type=%v url=%v", f.Type, f.URL)
	}
	if len(f.ThumbnailURL) == 0 || f.ThumbnailURL[0] != postPosterURL {
		t.Fatalf("expected poster thumbnail %q, got %v", postPosterURL, f.ThumbnailURL)
	}
	if media.Caption != "root caption" {
		t.Fatalf("expected root caption, got %q", media.Caption)
	}
}

// A CAROUSEL post must yield EVERY album child — the regression this fix
// addresses: bounding the old regex to the "code" marker dropped album images
// because carousel_media sits BEFORE the marker.
func TestJSONCarouselPostYieldsAllImages(t *testing.T) {
	child := func(url string) string {
		return `{"code":"CHILDCODE` + url[len(url)-5:] + `","media_type":1,` +
			`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + url + `"}]},` +
			`"video_versions":null,"carousel_media":null}`
	}
	root := `"code":"` + testCarousel + `","caption":{"text":"album caption"},"media_type":8,` +
		`"carousel_media":[` + child(carouselImg1) + `,` + child(carouselImg2) + `,` +
		child(carouselImg3) + `,` + child(carouselImg4) + `],` +
		`"image_versions2":{"candidates":[{"width":1536,"height":2048,"url":"` + carouselImg1 + `"}]}`
	media, err := ParsePostMedia(newTestContext(testCarousel), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 4 {
		t.Fatalf("expected 4 album items, got %d", len(media.Items))
	}
	want := []string{carouselImg1, carouselImg2, carouselImg3, carouselImg4}
	for i, item := range media.Items {
		if len(item.Formats) == 0 || item.Formats[0].URL[0] != want[i] {
			t.Fatalf("item %d: expected %q, got %+v", i, want[i], item.Formats)
		}
		if item.Formats[0].Type != database.MediaTypePhoto {
			t.Fatalf("item %d: expected photo type, got %v", i, item.Formats[0].Type)
		}
	}
	if media.Caption != "album caption" {
		t.Fatalf("expected album caption, got %q", media.Caption)
	}
	for _, leaked := range []string{replyOneURL, replyOneVid, replyTwoURL} {
		for _, item := range media.Items {
			if len(item.Formats) > 0 && item.Formats[0].URL[0] == leaked {
				t.Fatalf("reply media leaked into album: %s", leaked)
			}
		}
	}
}

// A mixed carousel (photo + video child) must keep the video child's formats.
func TestJSONMixedCarousel(t *testing.T) {
	root := `"code":"` + testCarousel + `","caption":{"text":"mixed"},"media_type":8,` +
		`"carousel_media":[` +
		`{"code":"CHILDAAAAA1","media_type":1,"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + carouselImg1 + `"}]},"video_versions":null},` +
		`{"code":"CHILDBBBBB2","media_type":2,"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},"video_versions":[{"type":103,"url":"` + postVideoURL + `"}]}` +
		`]`
	media, err := ParsePostMedia(newTestContext(testCarousel), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(media.Items))
	}
	if media.Items[0].Formats[0].Type != database.MediaTypePhoto {
		t.Fatalf("child 0 should be photo, got %v", media.Items[0].Formats[0].Type)
	}
	vf := media.Items[1].Formats[0]
	if vf.Type != database.MediaTypeVideo || vf.URL[0] != postVideoURL {
		t.Fatalf("child 1 should be video, got %+v", vf)
	}
}

// --- regex fallback path ----------------------------------------------------

func TestFallbackVideoPostExcludesReplies(t *testing.T) {
	media, err := ParsePostMedia(newTestContext(testPostCode), buildPage(true))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(media.Items))
	}
	f := media.Items[0].Formats[0]
	if f.Type != database.MediaTypeVideo || f.URL[0] != postVideoURL {
		t.Fatalf("unexpected fallback video format: %+v", f)
	}
	if media.Caption != "root caption" {
		t.Fatalf("expected root caption, got %q", media.Caption)
	}
}

func TestFallbackPhotoPostExcludesReplies(t *testing.T) {
	media, err := ParsePostMedia(newTestContext(testPostCode), buildPage(false))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(media.Items))
	}
	f := media.Items[0].Formats[0]
	if f.Type != database.MediaTypePhoto || f.URL[0] != postPosterURL {
		t.Fatalf("expected photo %q, got type=%v url=%v", postPosterURL, f.Type, f.URL)
	}
}

// The post-section boundary must stop before the first reply.
func TestExtractPostSectionBounds(t *testing.T) {
	sec := extractPostSection(string(buildPage(true)), testPostCode)
	if !strings.Contains(sec, postVideoURL) || !strings.Contains(sec, postPosterURL) {
		t.Fatalf("post section missing original media")
	}
	for _, leaked := range []string{replyOneURL, replyOneVid, replyTwoURL, testReplyOne, testReplyTwo} {
		if strings.Contains(sec, leaked) {
			t.Fatalf("post section leaked reply data: %s", leaked)
		}
	}
}

// Unknown code -> fall back to the whole body (no regression on odd pages).
func TestExtractPostSectionFallback(t *testing.T) {
	body := string(buildPage(true))
	if got := extractPostSection(body, "NOMATCHCODE"); got != body {
		t.Fatalf("expected fallback to full body when code absent")
	}
}

// --- shared (quote / repost) text posts -------------------------------------

const (
	quotedPostCode = "QQQQQUOTED1"
	quotedPostVid  = "https://scontent.example/quoted-video.mp4"
	quotedImg1     = "https://scontent.example/quoted-1.jpg"
	quotedImg2     = "https://scontent.example/quoted-2.jpg"
)

// A text post (media_type 19) carries no media of its own; the media lives on
// the quoted post. The parser must borrow the quoted post's media and caption
// instead of failing with "no media found in post page".
func TestQuotedTextPostBorrowsMedia(t *testing.T) {
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"quote wrapper"},` +
		`"image_versions2":{"candidates":[]},"video_versions":null,"carousel_media":null,` +
		`"text_post_app_info":{"share_info":{"quoted_attachment_post":{` +
		`"code":"` + quotedPostCode + `","media_type":2,` +
		`"caption":{"text":"quoted caption"},` +
		`"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + quotedPostVid + `"}],"carousel_media":null` +
		`}}}`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 item from quoted post, got %d", len(media.Items))
	}
	f := media.Items[0].Formats[0]
	if f.Type != database.MediaTypeVideo || f.URL[0] != quotedPostVid {
		t.Fatalf("expected quoted video %q, got %+v", quotedPostVid, f)
	}
	if media.Caption != "quoted caption" {
		t.Fatalf("expected quoted caption, got %q", media.Caption)
	}
}

// A quoted ALBUM must yield every child image from the quoted post.
func TestQuotedTextPostAlbum(t *testing.T) {
	child := func(url string) string {
		return `{"code":"CHILD` + url[len(url)-6:] + `","media_type":1,` +
			`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + url + `"}]},` +
			`"video_versions":null,"carousel_media":null}`
	}
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"quote wrapper"},"image_versions2":{"candidates":[]},"video_versions":null,` +
		`"text_post_app_info":{"share_info":{"quoted_attachment_post":{` +
		`"code":"` + quotedPostCode + `","media_type":8,"caption":{"text":"quoted album"},` +
		`"carousel_media":[` + child(quotedImg1) + `,` + child(quotedImg2) + `],` +
		`"image_versions2":{"candidates":[]},"video_versions":null` +
		`}}}`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 2 {
		t.Fatalf("expected 2 quoted album items, got %d", len(media.Items))
	}
	want := []string{quotedImg1, quotedImg2}
	for i, item := range media.Items {
		if item.Formats[0].URL[0] != want[i] {
			t.Fatalf("item %d: expected %q, got %v", i, want[i], item.Formats[0].URL)
		}
	}
}

// A text post with neither own media nor a shared post stays empty -> error.
func TestTextPostWithoutSharedMediaErrors(t *testing.T) {
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"just text"},"image_versions2":{"candidates":[]},"video_versions":null,` +
		`"text_post_app_info":{"share_info":{"quoted_attachment_post":null,"quoted_post":null,"reposted_post":null}}`
	if _, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root)); err == nil {
		t.Fatalf("expected error for media-less text post, got nil")
	}
}

// --- linked inline media (pasted link cards) --------------------------------

const (
	inlinePostCode = "IIIIINLINE1"
	inlineVid      = "https://scontent.example/inline-video.mp4"
	inlineImg1     = "https://scontent.example/inline-1.jpg"
	inlineImg2     = "https://scontent.example/inline-2.jpg"
)

// A text post that pastes an Instagram/Threads URL renders the target as an
// inline media card under text_post_app_info.linked_inline_media. share_info is
// null in that case, so the parser must read the card instead of failing.
func TestLinkedInlineMediaBorrowsMedia(t *testing.T) {
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"link wrapper"},` +
		`"image_versions2":{"candidates":[]},"video_versions":null,"carousel_media":null,` +
		`"text_post_app_info":{"share_info":null,` +
		`"linked_inline_media":{` +
		`"code":"` + inlinePostCode + `","media_type":2,` +
		`"caption":{"text":"inline caption"},` +
		`"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + inlineVid + `"}],"carousel_media":null}}`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 item from inline card, got %d", len(media.Items))
	}
	f := media.Items[0].Formats[0]
	if f.Type != database.MediaTypeVideo || f.URL[0] != inlineVid {
		t.Fatalf("expected inline video %q, got %+v", inlineVid, f)
	}
	if len(f.ThumbnailURL) == 0 || f.ThumbnailURL[0] != postPosterURL {
		t.Fatalf("expected inline poster %q, got %v", postPosterURL, f.ThumbnailURL)
	}
	if media.Caption != "inline caption" {
		t.Fatalf("expected inline caption, got %q", media.Caption)
	}
}

// A linked inline ALBUM must yield every child image.
func TestLinkedInlineMediaAlbum(t *testing.T) {
	child := func(url string) string {
		return `{"code":"CHILD` + url[len(url)-5:] + `","media_type":1,` +
			`"image_versions2":{"candidates":[{"width":1080,"height":1080,"url":"` + url + `"}]},` +
			`"video_versions":null,"carousel_media":null}`
	}
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"link wrapper"},"image_versions2":{"candidates":[]},"video_versions":null,` +
		`"text_post_app_info":{"share_info":null,"linked_inline_media":{` +
		`"code":"` + inlinePostCode + `","media_type":8,"caption":{"text":"inline album"},` +
		`"carousel_media":[` + child(inlineImg1) + `,` + child(inlineImg2) + `],` +
		`"image_versions2":{"candidates":[]},"video_versions":null}}`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 2 {
		t.Fatalf("expected 2 inline album items, got %d", len(media.Items))
	}
	want := []string{inlineImg1, inlineImg2}
	for i, item := range media.Items {
		if item.Formats[0].URL[0] != want[i] {
			t.Fatalf("item %d: expected %q, got %v", i, want[i], item.Formats[0].URL)
		}
	}
}

// When both share_info and linked_inline_media are present, the quoted/reposted
// post wins — it is the media the author is actually sharing.
func TestShareInfoBeatsLinkedInlineMedia(t *testing.T) {
	root := `"code":"` + testPostCode + `","media_type":19,` +
		`"caption":{"text":"wrapper"},"image_versions2":{"candidates":[]},"video_versions":null,` +
		`"text_post_app_info":{"share_info":{"quoted_attachment_post":{` +
		`"code":"` + quotedPostCode + `","media_type":2,"caption":{"text":"quoted caption"},` +
		`"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + quotedPostVid + `"}]}},` +
		`"linked_inline_media":{"code":"` + inlinePostCode + `","media_type":2,` +
		`"caption":{"text":"inline caption"},` +
		`"image_versions2":{"candidates":[{"width":640,"height":360,"url":"` + postPosterURL + `"}]},` +
		`"video_versions":[{"type":103,"url":"` + inlineVid + `"}]}}`
	media, err := ParsePostMedia(newTestContext(testPostCode), jsonPage(root))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 || media.Items[0].Formats[0].URL[0] != quotedPostVid {
		t.Fatalf("expected quoted post to win, got %+v", media.Items)
	}
}
