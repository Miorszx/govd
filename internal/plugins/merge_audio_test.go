package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util/libav"
)

// fakeCtx is the minimum ExtractorContext the merge plugin touches: the debug
// and info loggers, and a FilesTracker it appends the temp output path to.
func fakeCtx(t *testing.T) *models.ExtractorContext {
	t.Helper()
	// The context loggers dereference logger.L, which is only set once the bot
	// process calls logger.Init(). Tests have to do it themselves or the
	// Debugf/Infof calls inside the plugin segfault.
	logger.Init()
	return &models.ExtractorContext{
		ContentURL:   "https://example.invalid/reel/1",
		Extractor:    &models.Extractor{ID: "test"},
		FilesTracker: models.NewFilesTracker(),
	}
}

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
}

// makeAudio writes an audio-only file with the requested profile, or skips
// when this ffmpeg build cannot produce it.
//
// The native aac encoder on most Linux builds has no HE-AAC (SBR) mode -
// `-profile:a aac_he` fails with "Invalid argument" unless the binary was
// linked against libfdk_aac. The LC case is the one that matters for the
// stream-copy decision, and that one always encodes.
func makeAudio(t *testing.T, profile, path string) {
	t.Helper()
	args := []string{
		"-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100",
		"-c:a", "aac", "-b:a", "64k", "-ac", "2",
	}
	if profile != "" {
		args = append(args, "-profile:a", profile)
	}
	args = append(args, path)

	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot encode profile %q on this build: %v %s", profile, err, out)
	}
}

// probeProfile returns the profile ffprobe reports for the first audio
// stream, so the test asserts against the measured value rather than the
// encoder's intent.
func probeProfile(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,profile",
		"-of", "csv=p=0", path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return string(out)
}

// probeAllStreams returns codec/profile for every stream in the file, used to
// confirm a merge left the video track alone.
func probeAllStreams(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-show_entries", "stream=codec_type,codec_name,profile",
		"-of", "csv=p=0", path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return string(out)
}

// An AAC-LC source must not be re-encoded. This is Instagram's shape, and the
// audio survives on both platforms, so a needless pass would only cost
// quality.
func TestNeedsAACLCReencodeStreamCopiesLC(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	audio := filepath.Join(dir, "lc.m4a")
	makeAudio(t, "aac_low", audio)

	t.Logf("measured: %s", probeProfile(t, audio))

	if needsAACLCReencode(fakeCtx(t), audio) {
		t.Error("AAC-LC source was flagged for re-encode, want stream copy")
	}
}

// A source that is not LC must be re-encoded. The HE-AAC fixture usually
// cannot be produced on this ffmpeg build, so the test degrades to a skip
// rather than pretending to have measured it.
func TestNeedsAACLCReencodeReencodesNonLC(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	audio := filepath.Join(dir, "he.m4a")
	makeAudio(t, "aac_he", audio)

	t.Logf("measured: %s", probeProfile(t, audio))

	if !needsAACLCReencode(fakeCtx(t), audio) {
		t.Error("HE-AAC source was not flagged for re-encode, want aac-lc")
	}
}

// A path that does not exist makes ffprobe fail. The plugin must fall back to
// the stream copy it always did rather than refuse the merge - the file was
// already downloaded, and a probe failure is not a reason to drop the audio.
func TestNeedsAACLCReencodeProbeFailureStreamCopies(t *testing.T) {
	needFFmpeg(t)

	missing := filepath.Join(t.TempDir(), "nope.m4a")
	if needsAACLCReencode(fakeCtx(t), missing) {
		t.Error("probe failure flagged for re-encode, want stream copy fallback")
	}
}

// The merge itself must leave the video track untouched. Re-encoding AV1 costs
// minutes per reel, so the output video codec has to match the input.
func TestMergeVideoWithAudioAACLCPreservesVideoTrack(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()

	video := filepath.Join(dir, "v.mp4")
	if out, err := exec.Command(
		"ffmpeg", "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x284:rate=15:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", video,
	).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot encode h264 here: %v %s", err, out)
	}

	audio := filepath.Join(dir, "a.m4a")
	makeAudio(t, "aac_low", audio)

	outPath := filepath.Join(dir, "out.mp4")
	if err := libav.MergeVideoWithAudioAACLC(video, audio, outPath); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output missing: %v", err)
	}

	got := probeAllStreams(t, outPath)
	t.Logf("merged streams: %s", got)

	if !containsAll(got, "h264", "video", "aac", "LC") {
		t.Errorf("merged file streams = %q, want h264 video and aac LC audio", got)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		found := false
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
