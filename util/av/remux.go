package av

import (
	"fmt"
	"os"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func RemuxFile(
	inputFile string,
) error {
	tempFileName := inputFile + ".temp"
	outputFile := inputFile
	err := os.Rename(inputFile, tempFileName)
	if err != nil {
		return fmt.Errorf("failed to rename file: %v", err)
	}
	defer os.Remove(tempFileName)
	err = ffmpeg.
		Input(tempFileName).
		Output(outputFile, ffmpeg.KwArgs{
			"c": "copy",
		}).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		return fmt.Errorf("failed to remux file: %v", err)
	}
	return nil
}
