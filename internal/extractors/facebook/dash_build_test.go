package facebook

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

// newTestExtractorContext builds the minimal context buildDASHFormat needs. The
// Extractor field is set because Infof reads its ID when it logs, and the
// logger is initialised because Infof writes through the package-level one.
func newTestExtractorContext(t *testing.T) *models.ExtractorContext {
	t.Helper()
	if logger.L == nil {
		logger.Init()
	}
	return &models.ExtractorContext{
		ContentURL: "https://www.facebook.com/reel/000000000000000",
		ContentID:  "000000000000000",
		Extractor:  &models.Extractor{ID: "facebook"},
		HTTPClient: &networking.HTTPClient{},
		Context:    context.Background(),
	}
}

// syntheticDASHTracks mirrors a real reel manifest: a 1080p video-only entry
// and a separate audio rendition. Sizes are the ones measured on reel
// 1084425931240979 (3.56 Mbps, 80 s) so the budget arithmetic in the tests is
// anchored to a real file, not a guess.
func syntheticDASHTracks() *dashTracks {
	return &dashTracks{
		VideoURL:   "https://cdn.example.test/v-1080",
		AudioURL:   "https://cdn.example.test/a-main",
		Width:      1080,
		Height:     1920,
		VideoCodec: "av1",
		AudioCodec: "aac",
		VideoSize:  35_558_430,
		Duration:   80,
		Videos: []Representation{
			{Width: 1080, Height: 1920, URL: "https://cdn.example.test/v-1080", Codec: "av1", Size: 35_558_430},
		},
	}
}

// buildDASHFormat must return the video rendition plus the audio rendition, and
// the video format must carry the transcode plugin. Without the plugin the
// AV1 track would be sent to Telegram untouched and fail to play; without the
// audio format the transcode plugin has nothing to mux.
func TestBuildDASHFormatEmitsVideoAndAudio(t *testing.T) {
	ctx := newTestExtractorContext(t)
	data := &VideoData{DASH: syntheticDASHTracks(), DASHDuration: 80}

	formats := buildDASHFormat(ctx, data, 1900*1024*1024)
	if len(formats) != 2 {
		t.Fatalf("expected a video and an audio format, got %d", len(formats))
	}

	video := formats[0]
	if video.FormatID != "dash" {
		t.Fatalf("expected the video format first, got %q", video.FormatID)
	}
	if video.VideoCodec != database.MediaCodecAvc {
		t.Fatalf("expected the delivered codec to be avc, got %q", video.VideoCodec)
	}
	if video.AudioCodec != database.MediaCodecAac {
		t.Fatalf("expected the audio codec to be declared, got %q", video.AudioCodec)
	}
	if len(video.Plugins) != 1 || video.Plugins[0] == nil {
		t.Fatalf("expected the transcode plugin on the video format, got %d", len(video.Plugins))
	}
	if video.Plugins[0].ID != "transcode_h264" {
		t.Fatalf("expected the transcode plugin, got %q", video.Plugins[0].ID)
	}

	audio := formats[1]
	if audio.FormatID != "dash_audio" {
		t.Fatalf("expected the audio format second, got %q", audio.FormatID)
	}
	if audio.VideoCodec != "" {
		t.Fatalf("expected the audio format to have no video codec, got %q", audio.VideoCodec)
	}
	if len(audio.URL) != 1 || audio.URL[0] != "https://cdn.example.test/a-main" {
		t.Fatalf("expected the audio URL, got %v", audio.URL)
	}
}

// A signed CDN URL arrives with its query separators escaped as &amp; because
// BaseURL is XML character data. Forwarding that literal string makes the CDN
// answer 403, so the parser must decode the entity.
func TestParseDASHManifestDecodesXMLEntities(t *testing.T) {
	const manifest = `<?xml version="1.0"?>
<MPD mediaPresentationDuration="PT10S">
  <Period>
    <AdaptationSet contentType="video">
      <Representation id="1" bandwidth="900000" codecs="av01.0.08M.08" width="1080" height="1920">
        <BaseURL>https://cdn.example.test/v.mp4?_nc_cat=1&amp;_nc_sid=abc&amp;oh=xyz</BaseURL>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	tracks := parseDASHManifest(manifest)
	if tracks == nil {
		t.Fatal("expected the manifest to parse")
	}
	if strings.Contains(tracks.VideoURL, "&amp;") {
		t.Fatalf("expected the XML entity to be decoded, got %q", tracks.VideoURL)
	}
	if !strings.Contains(tracks.VideoURL, "&_nc_sid=abc&oh=xyz") {
		t.Fatalf("expected the query separators to survive decoding, got %q", tracks.VideoURL)
	}
}

// The whole ladder has to survive parsing. Pinning only the tallest entry is
// what forced long low-bitrate reels down to the 360p progressive URL: the
// 1080p entry overflows the Telegram cap while the 720p entry of the same
// manifest fits.
func TestParseDASHManifestKeepsFullLadder(t *testing.T) {
	const manifest = `<?xml version="1.0"?>
<MPD mediaPresentationDuration="PT80S">
  <Period>
    <AdaptationSet contentType="video">
      <Representation id="v720" bandwidth="800000" codecs="av01.0.05M.08" width="720" height="1280">
        <BaseURL>https://cdn.example.test/v-720</BaseURL>
      </Representation>
      <Representation id="v1080" bandwidth="3400000" codecs="av01.0.08M.08" width="1080" height="1920">
        <BaseURL>https://cdn.example.test/v-1080</BaseURL>
      </Representation>
    </AdaptationSet>
    <AdaptationSet contentType="audio">
      <Representation id="a1" bandwidth="44000" codecs="mp4a.40.2">
        <BaseURL>https://cdn.example.test/a-main</BaseURL>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	tracks := parseDASHManifest(manifest)
	if tracks == nil {
		t.Fatal("expected the manifest to parse")
	}
	if len(tracks.Videos) != 2 {
		t.Fatalf("expected both video renditions, got %d", len(tracks.Videos))
	}
	if tracks.Videos[0].Height != 1920 {
		t.Fatalf("expected the ladder tallest-first, got %d first", tracks.Videos[0].Height)
	}
	if tracks.Videos[1].Height != 1280 {
		t.Fatalf("expected the 720p entry second, got %d", tracks.Videos[1].Height)
	}
	if tracks.VideoURL != "https://cdn.example.test/v-1080" {
		t.Fatalf("expected the peak rendition mirrored, got %q", tracks.VideoURL)
	}
}

// The budget walk must fall to the next rendition down rather than give up.
// This is the whole point of keeping the ladder: 1080p projected at 3x blows
// the cap, 720p projected at 3x does not.
func TestPickVideoForBudgetWalksLadder(t *testing.T) {
	tracks := &dashTracks{
		Duration: 80,
		Videos: []Representation{
			{Width: 1080, Height: 1920, URL: "v-1080", Size: 35_558_430},
			{Width: 720, Height: 1280, URL: "v-720", Size: 8_000_000},
		},
	}

	const safeLimit = 49_807_360

	rep, ok := pickVideoForBudget(tracks, safeLimit)
	if !ok {
		t.Fatalf("expected a rendition to fit, got none")
	}
	if rep.Height != 1280 {
		t.Fatalf("expected the walk to land on 720p, got %d", rep.Height)
	}

	// Widen the budget and the same ladder must land on 1080p again.
	rep, ok = pickVideoForBudget(tracks, 1900*1024*1024)
	if !ok || rep.Height != 1920 {
		t.Fatalf("expected 1080p under a wide budget, got %dx%d ok=%v", rep.Width, rep.Height, ok)
	}
}

// Nothing fitting must be reported so the caller can fall back, and the
// shortest rendition is returned for the error message.
func TestPickVideoForBudgetReportsOverflow(t *testing.T) {
	tracks := &dashTracks{
		Duration: 80,
		Videos: []Representation{
			{Width: 1080, Height: 1920, Size: 40 * 1024 * 1024},
		},
	}

	rep, ok := pickVideoForBudget(tracks, 49_807_360)
	if ok {
		t.Fatal("expected the oversized rendition to be rejected")
	}
	if rep.Height != 1920 {
		t.Fatalf("expected the shortest rendition returned, got %d", rep.Height)
	}
}

// The CDN rejects the signed DASH URLs without the session cookies the page was
// fetched with, so both formats have to carry them.
func TestBuildDASHFormatCarriesCookies(t *testing.T) {
	ctx := newTestExtractorContext(t)
	ctx.HTTPClient.Cookies = []*http.Cookie{{Name: "session", Value: "synthetic"}}

	data := &VideoData{DASH: syntheticDASHTracks(), DASHDuration: 80}

	formats := buildDASHFormat(ctx, data, 1900*1024*1024)
	if len(formats) != 2 {
		t.Fatalf("expected a video and an audio format, got %d", len(formats))
	}
	for _, f := range formats {
		if f.DownloadSettings == nil || len(f.DownloadSettings.Cookies) == 0 {
			t.Fatalf("expected %s to carry the session cookies", f.FormatID)
		}
	}
}

// A reel without a manifest must leave the progressive ladder alone.
func TestBuildDASHFormatSkipsWhenNoManifest(t *testing.T) {
	ctx := newTestExtractorContext(t)

	formats := buildDASHFormat(ctx, &VideoData{}, 1900*1024*1024)
	if formats != nil {
		t.Fatalf("expected no dash formats, got %d", len(formats))
	}
}

// A ladder where no rendition fits must yield nothing, so the caller falls
// back to progressive rather than paying for an encode it cannot send.
func TestBuildDASHFormatDropsWhenNothingFits(t *testing.T) {
	ctx := newTestExtractorContext(t)
	data := &VideoData{
		DASH: &dashTracks{
			Duration: 80,
			Videos: []Representation{
				{Width: 1080, Height: 1920, URL: "v-1080", Size: 40 * 1024 * 1024},
			},
		},
		DASHDuration: 80,
	}

	const tgLimit = 50 * 1024 * 1024
	safeLimit := int64(tgLimit * 95 / 100)
	if formats := buildDASHFormat(ctx, data, safeLimit); formats != nil {
		t.Fatalf("expected the oversized rendition to be dropped, got %d formats", len(formats))
	}
}
