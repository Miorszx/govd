package libav

import (
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func AudioFromVideo(videoPath string, audioPath string) error {
	err := ffmpeg.
		Input(videoPath).
		Output(audioPath, ffmpeg.KwArgs{
			"map": "a",
			"vn":  nil,
			"f":   "mp3",
			"ab":  "128k",
		}).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		return err
	}
	return nil
}
