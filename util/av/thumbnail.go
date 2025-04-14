package av

import (
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func ExtractVideoThumbnail(
	videoPath string,
	thumbnailPath string,
) error {
	err := ffmpeg.
		Input(videoPath).
		Output(thumbnailPath, ffmpeg.KwArgs{
			"vframes": 1,
			"f":       "image2",
			"ss":      "00:00:01",
			"c:v":     "mjpeg",
			"q:v":     10, // not sure
		}).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		return err
	}
	return nil
}
