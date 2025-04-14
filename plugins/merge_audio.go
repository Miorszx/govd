package plugins

import (
	"context"
	"fmt"
	"govd/models"
	"govd/util"
	"govd/util/av"

	"github.com/pkg/errors"
)

func MergeAudio(media *models.DownloadedMedia) error {
	audioFormat := media.Media.GetDefaultAudioFormat()
	if audioFormat == nil {
		return errors.New("no audio format found")
	}

	// download the audio file
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	audioFile, err := util.DownloadFile(
		ctx, audioFormat.URL,
		audioFormat.GetFileName(), nil,
	)
	if err != nil {
		return fmt.Errorf("failed to download audio file: %w", err)
	}

	err = av.MergeVideoWithAudio(
		media.FilePath,
		audioFile,
	)
	if err != nil {
		return fmt.Errorf("failed to merge video with audio: %w", err)
	}

	return nil
}
