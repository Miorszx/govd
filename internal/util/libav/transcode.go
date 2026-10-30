package libav

import (
	"os"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// TranscodeToH264 re-encodes a video-only stream to H.264 while copying the
// audio stream from a separate file. Facebook's high-resolution DASH
// renditions are AV1 or VP9 only, and neither plays reliably on every phone,
// so the 1080p path has to be re-encoded before it is handed to Telegram.
//
// veryfast keeps the encode close to real time on a small VPS; crf 23 is the
// point where the 1080p output stays visibly better than the 720p progressive
// rendition Facebook serves muxed.
//
// When audioPath is empty the video is encoded on its own. Otherwise the audio
// stream is copied, not re-encoded: the DASH audio rendition is already AAC, so
// re-encoding it would only lose quality.
func TranscodeToH264(
	videoPath string,
	audioPath string,
	outputPath string,
) error {
	streams := []*ffmpeg.Stream{ffmpeg.Input(videoPath)}
	kwargs := ffmpeg.KwArgs{
		"c:v":      "libx264",
		"preset":   "veryfast",
		"crf":      "23",
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
