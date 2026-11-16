package instagram

import (
	"testing"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
)

// Instagram serves one progressive MP4 with the audio already muxed. The
// has_audio flag is the only signal that separates a reel from a silent
// screen recording, and declaring AAC on a silent file is what makes
// Telegram reject it.
func TestStoryAudioCodec(t *testing.T) {
	if got := storyAudioCodec(true); got != database.MediaCodecAac {
		t.Errorf("storyAudioCodec(true) = %q, want %q", got, database.MediaCodecAac)
	}
	if got := storyAudioCodec(false); got != "" {
		t.Errorf("storyAudioCodec(false) = %q, want empty", got)
	}
}

// A silent GraphVideo must carry no declared audio, and no MergeAudio plugin:
// mergeFormats skips a video whose AudioCodec is non-empty, so getting this
// wrong would also try to mux an audio track that does not exist.
func TestParseGQLMediaSilentVideoHasNoAudioCodec(t *testing.T) {
	ctx := newTestMediaContext(t)

	media, err := ParseGQLMedia(ctx, &Media{
		Typename: "GraphVideo",
		VideoURL: "https://cdn.example.test/v.mp4",
		HasAudio: false,
	})
	if err != nil {
		t.Fatalf("ParseGQLMedia: %v", err)
	}
	if len(media.Items) != 1 || len(media.Items[0].Formats) != 1 {
		t.Fatalf("unexpected shape: %d items", len(media.Items))
	}

	f := media.Items[0].Formats[0]
	if f.AudioCodec != "" {
		t.Errorf("silent video declared AudioCodec %q, want empty", f.AudioCodec)
	}
	if len(f.Plugins) != 0 {
		t.Errorf("silent video got %d plugins, want 0", len(f.Plugins))
	}
}

func TestParseGQLMediaAudibleVideoDeclaresAac(t *testing.T) {
	ctx := newTestMediaContext(t)

	media, err := ParseGQLMedia(ctx, &Media{
		Typename: "XDTGraphVideo",
		VideoURL: "https://cdn.example.test/v.mp4",
		HasAudio: true,
	})
	if err != nil {
		t.Fatalf("ParseGQLMedia: %v", err)
	}

	f := media.Items[0].Formats[0]
	if f.AudioCodec != database.MediaCodecAac {
		t.Errorf("audible video declared AudioCodec %q, want %q", f.AudioCodec, database.MediaCodecAac)
	}
}

// newTestMediaContext builds the minimum context NewMedia needs. It does not
// touch the network: ParseGQLMedia works entirely off the passed struct.
func newTestMediaContext(t *testing.T) *models.ExtractorContext {
	t.Helper()
	return &models.ExtractorContext{
		ContentURL: "https://www.instagram.com/reel/0000000000000/",
		ContentID:  "0000000000000",
		Extractor:  &models.Extractor{ID: "instagram"},
		Context:    t.Context(),
	}
}
