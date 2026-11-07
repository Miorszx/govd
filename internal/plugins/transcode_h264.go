package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
	"github.com/govdbot/govd/internal/util/libav"
)

// transcodeCRF is the single quality setting used for the H.264 conversion.
// It is 23 because that is the x264 default and measured close to the AV1
// source on real reels; there is no ladder above it. A reel that does not fit
// at this quality is not squeezed - the progressive rendition is served.
const transcodeCRF = 23

// TranscodeH264 turns a downloaded video-only rendition into an H.264 file with
// the audio muxed in, then swaps it in as the format's file.
//
// Facebook publishes its 1080p reel renditions only as AV1 inside the DASH
// manifest, so the video track has to be re-encoded before Telegram will play
// it. This plugin is attached to a DASH format only after the extractor has
// confirmed the manifest size already fits under the limit - it is a format
// conversion, not a size reduction. There is exactly one encode pass at
// crf 23; if that somehow lands over the cap the reel is abandoned and the
// progressive URL is served instead, because re-encoding down a ladder costs
// minutes per reel and still delivers something worse.
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
			// The video track is AV1 without an audio track to mux, so the
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

		basePath := strings.TrimSuffix(
			format.FilePath,
			filepath.Ext(format.FilePath),
		)
		outputPath := basePath + "_h264.mp4"
		ctx.FilesTracker.Add(outputPath)

		ctx.Infof("transcoding dash rendition to h264")
		if err := libav.TranscodeToH264(
			format.FilePath,
			downloadedAudio.FilePath,
			outputPath,
			transcodeCRF,
		); err != nil {
			return fmt.Errorf("failed to transcode to h264: %w", err)
		}

		info, statErr := os.Stat(outputPath)
		if statErr != nil {
			return fmt.Errorf("failed to stat transcoded file: %w", statErr)
		}
		if info.Size() > util.TelegramBotAPIFileLimit()*95/100 {
			// The pick was made on the manifest size and the encode still
			// grew past the cap. Do not re-encode: returning the error makes
			// core fall back to the progressive rendition, which is
			// playable and costs no further minutes.
			return fmt.Errorf(
				"transcoded output is %d bytes, over the telegram limit",
				info.Size(),
			)
		}

		// The source track is AV1 and useless once the H.264 file exists;
		// leaving it behind doubles the disk cost of every reel.
		if err := os.Remove(format.FilePath); err != nil {
			ctx.Infof("could not remove dash source: %v", err)
		}

		ctx.Infof("transcoded dash rendition: %d bytes", info.Size())

		format.FilePath = outputPath
		return nil
	},
}
