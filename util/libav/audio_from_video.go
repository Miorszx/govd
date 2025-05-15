package libav

import (
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"go.uber.org/zap"
)

func AudioFromVideo(videoPath string, audioPath string) error {
	silent := zap.S().Level() != zap.DebugLevel
	err := ffmpeg.
		Input(videoPath).
		Output(audioPath, ffmpeg.KwArgs{
			"map": "a",
			"vn":  nil,
			"f":   "mp3",
			"ab":  "128k",
		}).
		Silent(silent).
		OverWriteOutput().
		Run()
	if err != nil {
		return err
	}
	return nil
}
