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
			"ss":      "00:00:01",
		}).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		return err
	}
	return nil
}
