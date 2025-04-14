package av

import (
	"fmt"
	"os"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func MergeVideoWithAudio(
	videoFile string,
	audioFile string,
) error {
	tempFileName := videoFile + ".temp"
	outputFile := videoFile

	err := os.Rename(videoFile, tempFileName)
	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	defer os.Remove(tempFileName)
	defer os.Remove(audioFile)

	videoStream := ffmpeg.Input(tempFileName)
	audioStream := ffmpeg.Input(audioFile)

	err = ffmpeg.Output(
		[]*ffmpeg.Stream{videoStream, audioStream},
		outputFile,
		ffmpeg.KwArgs{
			"map":      []string{"0:v:0", "1:a:0"},
			"movflags": "+faststart",
			"c:v":      "copy",
			"c:a":      "copy",
		}).
		Silent(true).
		OverWriteOutput().
		Run()

	if err != nil {
		return fmt.Errorf("failed to merge files: %w", err)
	}

	return nil
}
