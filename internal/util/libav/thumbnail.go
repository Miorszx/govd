package libav

import (
	"os"

	"github.com/govdbot/govd/internal/logger"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func ExtractVideoThumbnail(
	videoPath string,
	outputPath string,
) (string, error) {
	logger.L.Debugf("extracting thumbnail from video: %s", videoPath)

	// Extract from 0.5s to avoid black first frame (FB reels often start dark)
	// Scale to 320 width for Telegram thumbnail limit (Bot API recommends 320x320 max)
	tmpPath := outputPath + ".full.jpeg"

	err := ffmpeg.Input(videoPath, ffmpeg.KwArgs{"ss": "0.5"}).
		Output(tmpPath, ffmpeg.KwArgs{
			"vframes": 1,
			"vcodec":  "mjpeg",
			"vf":      "scale=320:trunc(ow/dar/2)*2",
			"q:v":     "2",
		}).
		Silent(true).
		OverWriteOutput().
		Run()

	if err != nil {
		// fallback: select frame 15+ without ss
		err = ffmpeg.Input(videoPath).
			Filter("select", ffmpeg.Args{"gte(n,15)"}).
			Output(tmpPath, ffmpeg.KwArgs{
				"vframes": 1,
				"vcodec":  "mjpeg",
				"vf":      "scale=320:trunc(ow/dar/2)*2",
				"q:v":     "2",
			}).
			Silent(true).
			OverWriteOutput().
			Run()
	}

	if err != nil {
		os.Remove(tmpPath)
		os.Remove(outputPath)
		return "", err
	}

	// Move tmp to final
	err = os.Rename(tmpPath, outputPath)
	if err != nil {
		// try copy if rename cross-fs
		data, rErr := os.ReadFile(tmpPath)
		if rErr != nil {
			os.Remove(tmpPath)
			return "", rErr
		}
		wErr := os.WriteFile(outputPath, data, 0644)
		os.Remove(tmpPath)
		if wErr != nil {
			return "", wErr
		}
	}

	return outputPath, nil
}
