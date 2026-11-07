package libav

import (
	"os"
	"strconv"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// TranscodeToH264 re-encodes a video-only stream to H.264 while copying the
// audio stream from a separate file. Facebook's high-resolution DASH
// renditions are AV1 or VP9 only, and neither plays reliably on every phone,
// so the 1080p path has to be re-encoded before it is handed to Telegram.
//
// veryfast keeps the encode close to real time on a small VPS; crf is the
// caller's dial, because how far an AV1 reel can be squeezed before the
// output fits Telegram depends on its source bitrate. Measured growth over
// four real reels ranged from 1.16x at 3.56 Mbps to 2.98x at 0.48 Mbps, so
// one fixed value cannot serve both ends of the range.
//
// When audioPath is empty the video is encoded on its own. Otherwise the audio
// stream is copied, not re-encoded: the DASH audio rendition is already AAC, so
// re-encoding it would only lose quality.
func TranscodeToH264(
	videoPath string,
	audioPath string,
	outputPath string,
	crf int,
) error {
	streams := []*ffmpeg.Stream{ffmpeg.Input(videoPath)}
	kwargs := ffmpeg.KwArgs{
		"c:v":      "libx264",
		"preset":   "veryfast",
		"crf":      strconv.Itoa(crf),
		"pix_fmt":  "yuv420p",
		"movflags": "+faststart",
	}
	if audioPath != "" {
		streams = append(streams, ffmpeg.Input(audioPath))
		kwargs["c:a"] = "copy"
	}

	err := ffmpeg.Output(streams, outputPath, kwargs).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		os.Remove(outputPath)
		return err
	}
	return nil
}
