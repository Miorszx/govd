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

// synthetic DASH tracks mirroring what a reel manifest yields: 1080p video-only
// plus a separate audio rendition.
func syntheticDASHTracks() *dashTracks {
	return &dashTracks{
		VideoURL:   "https://cdn.example.test/v-1080",
		AudioURL:   "https://cdn.example.test/a-main",
		Width:      1080,
		Height:     1920,
		VideoCodec: "av1",
		AudioCodec: "aac",
		VideoSize:  5_089_536,
		Duration:   59,
	}
}

// buildDASHFormat must return the video rendition plus the audio rendition, and
// the video format must carry the transcode plugin. Without the plugin the
// AV1 track would be sent to Telegram untouched and fail to play; without the
// audio format the transcode plugin has nothing to mux.
func TestBuildDASHFormatEmitsVideoAndAudio(t *testing.T) {
	ctx := newTestExtractorContext(t)
	data := &VideoData{
		DASHURL:        "https://cdn.example.test/v-1080",
		DASHAudioURL:   "https://cdn.example.test/a-main",
		DASHWidth:      1080,
		DASHHeight:     1920,
		DASHSourceSize: 5_089_536,
		DASHDuration:   59,
	}

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

// The CDN rejects the signed DASH URLs without the session cookies the page was
// fetched with, so both formats have to carry them.
func TestBuildDASHFormatCarriesCookies(t *testing.T) {
	ctx := newTestExtractorContext(t)
	ctx.HTTPClient.Cookies = []*http.Cookie{{Name: "session", Value: "synthetic"}}

	data := &VideoData{
		DASHURL:        "https://cdn.example.test/v-1080",
		DASHAudioURL:   "https://cdn.example.test/a-main",
		DASHWidth:      1080,
		DASHHeight:     1920,
		DASHSourceSize: 5_089_536,
		DASHDuration:   59,
	}

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

// The estimate pads the AV1 source because the H.264 output is larger. A
// rendition that would blow past the safe limit must be dropped so the caller
// falls back to progressive instead of paying for an encode it cannot send.
func TestBuildDASHFormatDropsOversizedRendition(t *testing.T) {
	ctx := newTestExtractorContext(t)
	data := &VideoData{
		DASHURL:        "https://cdn.example.test/v-1080",
		DASHAudioURL:   "https://cdn.example.test/a-main",
		DASHWidth:      1080,
		DASHHeight:     1920,
		DASHSourceSize: 40 * 1024 * 1024,
		DASHDuration:   59,
	}

	const tgLimit = 50 * 1024 * 1024
	safeLimit := int64(tgLimit * 95 / 100)
	if formats := buildDASHFormat(ctx, data, safeLimit); formats != nil {
		t.Fatalf("expected the oversized rendition to be dropped, got %d formats", len(formats))
	}
}

// The size estimate must overshoot the source, otherwise a rendition that
// encodes past the Telegram cap would be accepted here and only fail after the
// encode finished.
func TestEstimateDASHOutputSizePadsSource(t *testing.T) {
	const source = 5_089_536
	got := estimateDASHOutputSize(&VideoData{DASHSourceSize: source})
	if got <= source {
		t.Fatalf("expected the estimate %d to exceed the source %d", got, source)
	}
	// A real 1080p AV1 reel of this size encodes to roughly 15 MB, so the
	// estimate has to land in that neighbourhood rather than be wildly high.
	if got < 12_000_000 || got > 20_000_000 {
		t.Fatalf("expected an estimate near the measured output, got %d", got)
	}
}

// With no bandwidth data the estimate is unknown, and an unknown size must not
// block the rendition: the progressive ladder is the worse outcome.
func TestEstimateDASHOutputSizeUnknown(t *testing.T) {
	if got := estimateDASHOutputSize(&VideoData{}); got != 0 {
		t.Fatalf("expected an unknown estimate to be zero, got %d", got)
	}
}
