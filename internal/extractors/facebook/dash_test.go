package facebook

import (
	"strings"
	"testing"
)

// Synthetic manifest shaped like the ones Facebook embeds for reels: one
// video-only adaptation set whose ladder tops out at 1080p, plus one
// audio-only set. Codec identifiers and URLs are invented.
const stubManifestXML = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011" type="static" mediaPresentationDuration="PT12.500000S">
<Period id="0" duration="PT12.500000S">
<AdaptationSet id="0" contentType="video" par="9:16">
<Representation id="100v" bandwidth="80000" codecs="av01.0.05M.08.0.111" mimeType="video/mp4" width="480" height="854" FBQualityLabel="360p"><BaseURL>https://cdn.example.test/v-360</BaseURL></Representation>
<Representation id="200v" bandwidth="450000" codecs="av01.0.05M.08.0.111" mimeType="video/mp4" width="720" height="1280" FBQualityLabel="720p"><BaseURL>https://cdn.example.test/v-720</BaseURL></Representation>
<Representation id="300v" bandwidth="900000" codecs="av01.0.08M.08.0.111" mimeType="video/mp4" width="1080" height="1920" FBQualityLabel="1080p"><BaseURL>https://cdn.example.test/v-1080</BaseURL></Representation>
</AdaptationSet>
<AdaptationSet id="1" contentType="audio">
<Representation id="400a" bandwidth="64000" codecs="mp4a.40.5" mimeType="audio/mp4"><BaseURL>https://cdn.example.test/a-main</BaseURL></Representation>
</AdaptationSet>
</Period>
</MPD>`

// The manifest ships inside the page as a JSON string, so the value is
// escaped. A plain regex capture returns a mangled document, which is why the
// parser reads the literal through encoding/json.
const stubBodyWithManifest = `<html><body><script type="application/json">{"require":[["ScheduledServerJS","handle",null,[{"video":{"videoDeliveryResponseFragment":{"videoDeliveryResponseResult":{"manifest_xml":"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\u003cMPD xmlns=\"urn:mpeg:dash:schema:mpd:2011\" mediaPresentationDuration=\"PT12.500000S\"\u003e\u003cPeriod\u003e\u003cAdaptationSet contentType=\"video\"\u003e\u003cRepresentation id=\"300v\" bandwidth=\"900000\" codecs=\"av01.0.08M.08.0.111\" width=\"1080\" height=\"1920\"\u003e\u003cBaseURL\u003ehttps://cdn.example.test/v-1080\u003c/BaseURL\u003e\u003c/Representation\u003e\u003c/AdaptationSet\u003e\u003c/Period\u003e\u003c/MPD\u003e","progressive_urls":[]}}}]]]}</script></body></html>`

func TestExtractManifestXMLUnescapesInlineField(t *testing.T) {
	got := extractManifestXML([]byte(stubBodyWithManifest))
	if got == "" {
		t.Fatal("expected the inline manifest to be extracted, got empty string")
	}
	if !strings.Contains(got, "<MPD") {
		t.Fatalf("expected a raw XML document, got %q", got)
	}
	if strings.Contains(got, `\"`) {
		t.Fatalf("expected the JSON escaping to be resolved, got %q", got)
	}
	if !strings.Contains(got, "https://cdn.example.test/v-1080") {
		t.Fatalf("expected the representation URL in the manifest, got %q", got)
	}
}

func TestExtractManifestXMLMissingField(t *testing.T) {
	if got := extractManifestXML([]byte(`{"progressive_urls":[]}`)); got != "" {
		t.Fatalf("expected empty result when no manifest is present, got %q", got)
	}
}

func TestParseDASHManifestPicksHighestVideoAndAudio(t *testing.T) {
	tracks := parseDASHManifest(stubManifestXML)
	if tracks == nil {
		t.Fatal("expected tracks from the manifest, got nil")
	}
	if tracks.VideoURL != "https://cdn.example.test/v-1080" {
		t.Fatalf("expected the 1080p rendition, got %q", tracks.VideoURL)
	}
	if tracks.Width != 1080 || tracks.Height != 1920 {
		t.Fatalf("expected 1080x1920, got %dx%d", tracks.Width, tracks.Height)
	}
	if tracks.VideoCodec != "av1" {
		t.Fatalf("expected the av1 codec, got %q", tracks.VideoCodec)
	}
	if tracks.AudioURL != "https://cdn.example.test/a-main" {
		t.Fatalf("expected the audio rendition, got %q", tracks.AudioURL)
	}
	if tracks.AudioCodec != "aac" {
		t.Fatalf("expected the aac codec, got %q", tracks.AudioCodec)
	}
	if tracks.Duration != 13 {
		t.Fatalf("expected the 12.5s duration to round to 13, got %d", tracks.Duration)
	}
	if tracks.VideoSize != 1_462_500 {
		t.Fatalf("expected the estimated size 1462500 bytes, got %d", tracks.VideoSize)
	}
}

// A manifest without audio must still yield the video track: the caller falls
// back to the progressive rendition for sound.
func TestParseDASHManifestWithoutAudio(t *testing.T) {
	const videoOnly = `<MPD mediaPresentationDuration="PT5S"><Period><AdaptationSet contentType="video"><Representation id="1v" bandwidth="100000" codecs="avc1.640028" width="720" height="1280"><BaseURL>https://cdn.example.test/only-video</BaseURL></Representation></AdaptationSet></Period></MPD>`
	tracks := parseDASHManifest(videoOnly)
	if tracks == nil {
		t.Fatal("expected tracks from a video-only manifest, got nil")
	}
	if tracks.VideoURL != "https://cdn.example.test/only-video" {
		t.Fatalf("expected the video rendition, got %q", tracks.VideoURL)
	}
	if tracks.AudioURL != "" {
		t.Fatalf("expected no audio rendition, got %q", tracks.AudioURL)
	}
	if tracks.VideoCodec != "avc" {
		t.Fatalf("expected the avc codec, got %q", tracks.VideoCodec)
	}
}

func TestParseDASHManifestRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":    "",
		"not xml":  "hello world",
		"no video": `<MPD><Period><AdaptationSet contentType="audio"><Representation id="1a" codecs="mp4a.40.2"><BaseURL>https://cdn.example.test/a</BaseURL></Representation></AdaptationSet></Period></MPD>`,
	} {
		if tracks := parseDASHManifest(in); tracks != nil {
			t.Fatalf("%s: expected nil tracks, got %+v", name, tracks)
		}
	}
}

func TestCodecFromDASHAttr(t *testing.T) {
	cases := map[string]string{
		"av01.0.08M.08.0.111": "av1",
		"avc1.640028":         "avc",
		"vp09.00.10.08":       "vp9",
		"hvc1.1.6.L93.B0":     "hevc",
		"mp4a.40.5":           "aac",
		"opus":                "opus",
		"":                    "",
		"unknown.1":           "",
	}
	for in, want := range cases {
		if got := codecFromDASHAttr(in); got != want {
			t.Fatalf("codecFromDASHAttr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDASHDuration(t *testing.T) {
	cases := map[string]int32{
		"PT58.517334S": 59,
		"PT12S":        12,
		"PT0.4S":       0,
		"":             0,
		"nonsense":     0,
	}
	for in, want := range cases {
		if got := parseDASHDuration(in); got != want {
			t.Fatalf("parseDASHDuration(%q) = %d, want %d", in, got, want)
		}
	}
}
