package libav

import (
	"fmt"
	"io"
	"os"

	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func MergeSegments(
	initSegmentPath string,
	segmentPaths []string,
	outputPath string,
) (string, error) {
	if len(segmentPaths) == 0 {
		return "", errors.New("no segments to merge")
	}
	if initSegmentPath != "" && fileExists(initSegmentPath) {
		return mergeFragmentedMP4(initSegmentPath, segmentPaths, outputPath)
	}
	return mergeRegularSegments(segmentPaths, outputPath)
}

func mergeFragmentedMP4(
	initSegmentPath string,
	segmentPaths []string,
	outputPath string,
) (string, error) {
	output, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer output.Close()

	// copy init segment once at the beginning
	initFile, err := os.Open(initSegmentPath)
	if err != nil {
		return "", fmt.Errorf("failed to open init segment: %w", err)
	}
	defer initFile.Close()

	bytesWritten, err := io.Copy(output, initFile)
	if err != nil {
		return "", fmt.Errorf("failed to copy init segment: %w", err)
	}
	zap.S().Debugf("copied init segment: %s", humanize.Bytes(uint64(bytesWritten)))

	// copy all segments sequentially
	totalBytes := bytesWritten
	for i, segmentPath := range segmentPaths {
		if !fileExists(segmentPath) {
			zap.S().Warnf("segment %d does not exist: %s", i, segmentPath)
			continue
		}

		segmentFile, err := os.Open(segmentPath)
		if err != nil {
			return "", fmt.Errorf("failed to open segment %d (%s): %w", i, segmentPath, err)
		}

		segmentBytes, err := io.Copy(output, segmentFile)
		segmentFile.Close()

		if err != nil {
			return "", fmt.Errorf("failed to copy segment %d (%s): %w", i, segmentPath, err)
		}

		totalBytes += segmentBytes
	}

	zap.S().Debugf("merged fragmented MP4: written to %s (%s)", outputPath, humanize.Bytes(uint64(totalBytes)))
	return outputPath, nil
}

func mergeRegularSegments(segmentPaths []string, outputPath string) (string, error) {
	output, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer output.Close()

	var totalBytes int64
	for i, segmentPath := range segmentPaths {
		if !fileExists(segmentPath) {
			zap.S().Warnf("segment %d does not exist: %s", i, segmentPath)
			continue
		}

		segmentFile, err := os.Open(segmentPath)
		if err != nil {
			return "", fmt.Errorf("failed to open segment %d (%s): %w", i, segmentPath, err)
		}

		segmentBytes, err := io.Copy(output, segmentFile)
		segmentFile.Close()

		if err != nil {
			return "", fmt.Errorf("failed to copy segment %d (%s): %w", i, segmentPath, err)
		}

		totalBytes += segmentBytes
	}

	if totalBytes == 0 {
		os.Remove(outputPath)
		return "", errors.New("no valid segments found to merge")
	}

	zap.S().Debugf("merged regular segments: written to %s (%s)", outputPath, humanize.Bytes(uint64(totalBytes)))
	return outputPath, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
