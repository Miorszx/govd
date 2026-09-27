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
	testReplyOne  = "BBBBREPLYONE1"
	testReplyTwo  = "BBBBREPLYTWO2"
	postVideoURL  = "https://scontent.example/post-video.mp4"
	postPosterURL = "https://scontent.example/post-poster.jpg"
	replyOneURL   = "https://scontent.example/reply-one-image.jpg"
	replyOneVid   = "https://scontent.example/reply-one-video.mp4"
	replyTwoURL   = "https://scontent.example/reply-two-image.jpg"
)

// buildPage mimics the shape of a real Threads post page: the root post object
// (prefix with its own "code") followed by the reply stream, where every reply
// carries its own "code" + image_versions2 / video_versions arrays.
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

// A video post must yield EXACTLY the original video (with poster thumbnail)
// and nothing from the reply stream. Regression test for the "extra media from
// comments" bug.
func TestVideoPostExcludesReplies(t *testing.T) {
	media, err := ParsePostMedia(newTestContext(testPostCode), buildPage(true))
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	if len(media.Items) != 1 {
		t.Fatalf("expected 1 media item (original post only), got %d", len(media.Items))
	}
	formats := media.Items[0].Formats
	if len(formats) != 1 {
		t.Fatalf("expected 1 format (single distinct video), got %d: %+v", len(formats), formats)
	}
	f := formats[0]
	if f.Type != database.MediaTypeVideo {
		t.Fatalf("expected video format, got type=%v", f.Type)
	}
	if len(f.URL) == 0 || f.URL[0] != postVideoURL {
		t.Fatalf("expected post video URL %q, got %v", postVideoURL, f.URL)
	}
	if len(f.ThumbnailURL) == 0 || f.ThumbnailURL[0] != postPosterURL {
		t.Fatalf("expected poster thumbnail %q, got %v", postPosterURL, f.ThumbnailURL)
	}
	if got := media.Caption; got != "root caption" {
		t.Fatalf("expected root caption, got %q", got)
	}
}

// A photo post must yield only the original image, never reply media.
func TestPhotoPostExcludesReplies(t *testing.T) {
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
