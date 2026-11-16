package facebook

import (
	"testing"

	"github.com/govdbot/govd/internal/database"
)

// The manifest codec name decides the AudioCodec declared on the audio
// format. Facebook serves mp4a today, so AAC is the default; the other names
// exist so a manifest that switches to Opus is labelled as Opus rather than
// silently declared AAC and then rejected by Telegram after a stream copy.
func TestDASHAudioCodecMapping(t *testing.T) {
	cases := []struct {
		in   string
		want database.MediaCodec
	}{
		{"aac", database.MediaCodecAac},
		{"", database.MediaCodecAac}, // unknown defaults to what FB serves
		{"opus", database.MediaCodecOpus},
		{"vorbis", database.MediaCodecVorbis},
		{"mp3", database.MediaCodecMp3},
		{"flac", database.MediaCodecFlac},
		{"somethingelse", database.MediaCodecAac},
	}
	for _, c := range cases {
		if got := dashAudioCodec(c.in); got != c.want {
			t.Errorf("dashAudioCodec(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// buildDASHFormat must forward the manifest codec onto the audio format
// instead of hardcoding AAC.
func TestBuildDASHFormatCarriesManifestAudioCodec(t *testing.T) {
	ctx := newTestExtractorContext(t)

	data := &VideoData{
		DASHDuration: 30,
		DASH:         syntheticDASHTracks(),
	}
	data.DASH.AudioCodec = "opus"

	formats := buildDASHFormat(ctx, data, 1<<40)
	if len(formats) != 2 {
		t.Fatalf("got %d formats, want 2", len(formats))
	}

	found := false
	for _, f := range formats {
		if f.FormatID != "dash_audio" {
			continue
		}
		found = true
		if f.AudioCodec != database.MediaCodecOpus {
			t.Errorf("dash_audio codec = %q, want %q", f.AudioCodec, database.MediaCodecOpus)
		}
	}
	if !found {
		t.Fatal("dash_audio format missing")
	}
}
