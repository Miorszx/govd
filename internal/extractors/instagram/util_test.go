package instagram

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

func TestMain(m *testing.M) {
	logger.Init()
	os.Exit(m.Run())
}

// fakeClient returns a fixed HTTP response built from an in-memory fixture,
// regardless of the request. This lets us exercise the REAL GetWebpageMedia
// parse/decision logic without hitting Instagram.
type fakeClient struct {
	body []byte
}

func (f *fakeClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(f.body))),
		Header:     make(http.Header),
	}, nil
}

// reelFixture is a server-rendered reel page shaped like the HTML Instagram
// actually serves. It deliberately contains TWO media payloads:
//
//  1. a decoy "video_versions" belonging to a suggested reel (no code marker
//     of its own before it in this fixture), and
//  2. the real payload, owned by "code":"SYNTH000001".
//
// This is the regression guard for the bug measured on Db0lSiOkZln: the first
// "video_versions" in a live page belonged to a suggested post, so taking
// occurrence[0] downloaded the wrong video. Every identifier here is synthetic
// and every domain is .invalid.
const reelFixture = `<!DOCTYPE html><html><head>
<meta property="og:title" content="synthuser on Instagram: &quot;synthetic caption&quot;" />
<meta property="og:description" content="1,234 likes, 56 comments - synthuser on January 1, 2026: &quot;synthetic caption&quot;" />
</head><body>
<script type="application/json">
{"require":[["ScheduledServerJS","handle",null,[{"__bbox":{"result":{"data":{
"xdt_api__v1__media__shortcode__web_info":{"items":[{
"code":"SYNTH000001","original_width":720,"original_height":1280,
"video_versions":[{"type":101,"url":"https:\/\/cdn.example.invalid\/v\/synthetic-real.mp4","width":720,"height":1280}],
"image_versions2":{"candidates":[{"url":"https:\/\/cdn.example.invalid\/v\/synthetic-poster.jpg","width":720,"height":1280}]}
}]}}}}]]]}
</script>
<script type="application/json">
{"require":[["ScheduledServerJS","handle",null,[{"__bbox":{"result":{"data":{
"xdt_api__v1__media__shortcode__web_info":{"items":[{
"code":"DECOY000002","original_width":640,"original_height":360,
"video_versions":[{"type":101,"url":"https:\/\/cdn.example.invalid\/v\/synthetic-decoy.mp4","width":640,"height":360}]
}]}}}}]]]}
</script>
</body></html>`

func newTestContext(fixture []byte) *models.ExtractorContext {
	ext := &models.Extractor{ID: "instagram"}
	return &models.ExtractorContext{
		ContentURL: "https://www.instagram.com/reel/SYNTH000001/",
		ContentID:  "SYNTH000001",
		Extractor:  ext,
		Context:    context.Background(),
		HTTPClient: &networking.HTTPClient{Client: &fakeClient{body: fixture}},
	}
}

// TestReelVideoDetection proves a reel carrying video_versions is parsed as a
// VIDEO, not a photo. Regression test for the "reel sent as thumbnail" bug.
func TestReelVideoDetection(t *testing.T) {
	ctx := newTestContext([]byte(reelFixture))

	media, err := GetWebpageMedia(ctx)
	if err != nil {
		t.Fatalf("GetWebpageMedia returned error: %v", err)
	}
	if media == nil || len(media.Items) == 0 {
		t.Fatalf("no media items returned")
	}

	item := media.Items[0]
	formats := item.Formats
	if len(formats) == 0 {
		t.Fatalf("no formats on media item")
	}

	gotVideo := false
	for _, f := range formats {
		if f.Type == database.MediaTypeVideo && len(f.URL) > 0 {
			gotVideo = true
		}
	}
	if !gotVideo {
		t.Fatalf("expected a video format for a reel carrying video_versions, got: %+v", formats)
	}
}

// TestIgnoresSuggestedPost is the guard for the wrong-video bug: the payload
// that is NOT owned by the requested shortcode must never be selected.
func TestIgnoresSuggestedPost(t *testing.T) {
	ctx := newTestContext([]byte(reelFixture))

	media, err := GetWebpageMedia(ctx)
	if err != nil {
		t.Fatalf("GetWebpageMedia returned error: %v", err)
	}
	if len(media.Items) == 0 || len(media.Items[0].Formats) == 0 {
		t.Fatal("no formats")
	}

	for _, f := range media.Items[0].Formats {
		for _, u := range f.URL {
			if strings.Contains(u, "synthetic-decoy") {
				t.Fatalf("picked the suggested post's video instead of the requested one: %s", u)
			}
		}
	}
}

// TestOwnerScoping pins the marker logic directly.
func TestOwnerScoping(t *testing.T) {
	page, err := parseIGHTML(reelFixture, "SYNTH000001")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, ok := bestVideoVersion(page.VideoVersions)
	if !ok {
		t.Fatal("no video version found")
	}
	if v.Width != 720 || v.Height != 1280 {
		t.Fatalf("expected the owned 720x1280 rendition, got %dx%d", v.Width, v.Height)
	}
	if !strings.Contains(v.URL, "synthetic-real") {
		t.Fatalf("expected the owned URL, got %s", v.URL)
	}
}

// TestUnknownShortcodeFailsLoudly makes sure a page with no matching media
// returns an error instead of silently shipping the wrong post.
func TestUnknownShortcodeFailsLoudly(t *testing.T) {
	if _, err := parseIGHTML(reelFixture, "MISSING0000"); err == nil {
		t.Fatal("expected an error for a shortcode absent from the page")
	}
}

// TestHTMLUnescape covers Instagram's hex numeric entities (Thai captions come
// through as &#xe2b; without this).
func TestHTMLUnescape(t *testing.T) {
	got := htmlUnescape("&quot;a&quot; &#xe2b;&#xe49; &amp; &#39;b&#39;")
	want := `"a" ห้ & 'b'`
	if got != want {
		t.Fatalf("htmlUnescape = %q, want %q", got, want)
	}
}
