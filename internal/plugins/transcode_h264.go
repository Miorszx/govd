package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util/libav"
)

// TranscodeH264 turns a downloaded video-only rendition into an H.264 file with
// the audio muxed in, then swaps it in as the format's file.
//
// Facebook publishes its 1080p reel renditions only as AV1 or VP9 inside the
// DASH manifest; the muxed progressive file tops out at 720x1280. Copying the
// streams is therefore not an option - the video track has to be re-encoded -
// and this plugin is what the extractor attaches to the DASH format when it
// picks that rendition.
//
// The audio is a second format in the same item, downloaded through the same
// DownloadFunc the merge plugin uses, so the codec/size validation and the
// file tracking stay identical to the rest of the pipeline. Audio is copied,
// never re-encoded: only the video stream is decoded, which keeps the work to
// one decode pass.
var TranscodeH264 = &models.Plugin{
	ID: "transcode_h264",
	RunFunc: func(ctx *models.ExtractorContext, item *models.MediaItem, format *models.DownloadedFormat) error {
		audioFormat := item.GetDefaultAudioFormat()
		if audioFormat == nil {
			// The video track is AV1/VP9 without an audio track to mux, so the
			// result would be a silent file Telegram cannot play. Fail loudly
			// rather than deliver it.
			return fmt.Errorf("no audio format found for the dash rendition")
		}
		if ctx.DownloadFunc == nil {
			return fmt.Errorf("download function not available in context")
		}

		downloadedAudio, err := ctx.DownloadFunc(ctx, 0, audioFormat)
		if err != nil {
			return fmt.Errorf("failed to download audio format: %w", err)
		}

		outputPath := strings.TrimSuffix(
			format.FilePath,
			filepath.Ext(format.FilePath),
		) + "_h264.mp4"
		ctx.FilesTracker.Add(outputPath)

		ctx.Infof("transcoding dash rendition to h264")
		err = libav.TranscodeToH264(
			format.FilePath,
			downloadedAudio.FilePath,
			outputPath,
		)
		if err != nil {
			return fmt.Errorf("failed to transcode to h264: %w", err)
		}

		// The source track is AV1/VP9 and useless once the H.264 file exists;
		// leaving it behind doubles the disk cost of every reel.
		if err := os.Remove(format.FilePath); err != nil {
			ctx.Infof("could not remove dash source: %v", err)
		}

		format.FilePath = outputPath
		return nil
	},
}
