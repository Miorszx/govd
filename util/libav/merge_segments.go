package libav

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func MergeSegments(
	segmentPaths []string,
	outputPath string,
) (string, error) {
	if len(segmentPaths) == 0 {
		return "", errors.New("no segments to merge")
	}
	listFilePath := outputPath + ".segments.txt"
	listFile, err := os.Create(listFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create segment list file: %w", err)
	}
	defer listFile.Close()
	defer os.Remove(listFilePath)
	for _, segmentPath := range segmentPaths {
		fmt.Fprintf(listFile, "file '%s'\n", segmentPath)
	}

	err = ffmpeg.
		Input(listFilePath, ffmpeg.KwArgs{
			"f":                  "concat",
			"safe":               "0",
			"protocol_whitelist": "file,pipe",
		}).
		Output(outputPath, ffmpeg.KwArgs{
			"c":        "copy",
			"movflags": "+faststart",
		}).
		Silent(true).
		OverWriteOutput().
		Run()
	if err != nil {
		os.Remove(outputPath)
		return "", fmt.Errorf("failed to merge segments: %w", err)
	}
	return outputPath, nil
}
