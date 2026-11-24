package libav

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needFFmpegAndProbe(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

func ffmpegRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg failed: %v %s", err, out)
	}
}

func probeStreams(t *testing.T, path string) string {
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

// NormalizeAudioToAACLC must leave the video track alone and land the audio on
// LC. This is the progressive Facebook repair path: a single already-muxed
// MP4 that no merge plugin touches.
func TestNormalizeAudioToAACLCPreservesVideoAndLandsLC(t *testing.T) {
	needFFmpegAndProbe(t)
	dir := t.TempDir()

	input := filepath.Join(dir, "in.mp4")
	ffmpegRun(t,
		"-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x284:rate=15:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "64k",
		input,
	)

	before := probeStreams(t, input)
	t.Logf("input streams: %s", before)

	outPath := filepath.Join(dir, "out.mp4")
	if err := NormalizeAudioToAACLC(input, outPath, 48000); err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	after := probeStreams(t, outPath)
	t.Logf("output streams: %s", after)

	if !strings.Contains(after, "h264") {
		t.Errorf("video track lost or re-encoded: %q", after)
	}
	if !strings.Contains(after, "aac") || !strings.Contains(after, "LC") {
		t.Errorf("audio is not aac LC: %q", after)
	}
	if strings.Count(strings.TrimSpace(after), "\n") != strings.Count(strings.TrimSpace(before), "\n") {
		t.Errorf("stream count changed: before=%q after=%q", before, after)
	}
}
