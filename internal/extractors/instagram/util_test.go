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

// reelFixture is the smallest web_info payload that still reproduces the shape
// under test: is_video is null, yet video_versions carries a rendition. The
// items array holds the node itself - that is the shape web_info returns. Every
// identifier here is synthetic.
const reelFixture = `{
  "data": {
    "xdt_api__v1__media__shortcode__web_info": {
      "items": [
        {
          "__typename": "XDTGraphVideo",
          "code": "SYNTH000001",
          "is_video": null,
          "original_width": 720,
          "original_height": 1280,
          "caption": { "text": "synthetic caption body" },
          "video_versions": [
            {
              "width": 720,
              "height": 1280,
              "type": 101,
              "url": "https://cdn.example.invalid/v/t16/f1/synthetic-reel.mp4"
            }
          ],
          "image_versions2": {
            "candidates": [
              {
                "width": 720,
                "height": 1280,
                "url": "https://cdn.example.invalid/v/t16/f1/synthetic-poster.jpg"
              }
            ]
          }
        }
      ]
    }
  }
}`

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

// TestReelVideoDetection proves the fix: a reel whose web_info response has
// is_video:null but a non-empty video_versions list MUST be parsed as a VIDEO,
// not a photo. Regression test for the "reel sent as thumbnail" bug. The
// fixture is in-memory so the test does not depend on scratch files under
// /tmp surviving a reboot.
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
