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

// crfLadder is walked in order until the encoded file fits under the Telegram
// limit. The first pass starts at 23 because that is where a 1080p AV1 reel
// measures only 1.16x its source and usually lands far below the cap; the
// later passes exist for long low-bitrate reels, where the same encode grows
// the file toward 3x and needs real compression to come back under.
var crfLadder = []int{23, 28, 32, 35}

// TranscodeH264 turns a downloaded video-only rendition into an H.264 file with
// the audio muxed in, then swaps it in as the format's file.
//
// Facebook publishes its 1080p reel renditions only as AV1 or VP9 inside the
// DASH manifest; the muxed progressive file tops out at 360x640 for reels.
// Copying the streams is therefore not an option - the video track has to be
// re-encoded - and this plugin is what the extractor attaches to the DASH
// format when it picks that rendition.
//
// The audio is a second format in the same item, downloaded through the same
// DownloadFunc the merge plugin uses, so the codec/size validation and the
// file tracking stay identical to the rest of the pipeline. Audio is copied,
// never re-encoded: only the video stream is decoded, which keeps the work to
// one decode pass per attempt.
//
// The size check after each encode is what makes the budget honest. The
// extractor's projection is a guess from the manifest bandwidth; the file on
// disk is the measurement. When an attempt overflows, the next rung of the
// ladder re-encodes from the same source instead of delivering a file Telegram
// will reject.
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

		basePath := strings.TrimSuffix(
			format.FilePath,
			filepath.Ext(format.FilePath),
		)

		// Leave a margin under the hard cap. Telegram rejects at the limit, not
		// above it, and the multipart body adds a little on top of the file.
		limit := util.TelegramBotAPIFileLimit() * 95 / 100

		var outputPath string
		var lastSize int64

		for _, crf := range crfLadder {
			outputPath = fmt.Sprintf("%s_crf%d.mp4", basePath, crf)
			ctx.FilesTracker.Add(outputPath)

			ctx.Infof("transcoding dash rendition to h264 (crf %d)", crf)
			err = libav.TranscodeToH264(
				format.FilePath,
				downloadedAudio.FilePath,
				outputPath,
				crf,
			)
			if err != nil {
				return fmt.Errorf("failed to transcode to h264 at crf %d: %w", crf, err)
			}

			info, statErr := os.Stat(outputPath)
			if statErr != nil {
				return fmt.Errorf("failed to stat transcoded file: %w", statErr)
			}
			lastSize = info.Size()

			if limit <= 0 || lastSize <= limit {
				ctx.Infof(
					"transcoded dash rendition: crf %d -> %d bytes (limit %d)",
					crf, lastSize, limit,
				)
				break
			}

			// This rung overflowed; drop it before trying the next one so a
			// failed attempt cannot be mistaken for the deliverable.
			ctx.Infof(
				"transcoded dash rendition at crf %d is %d bytes, over the %d limit",
				crf, lastSize, limit,
			)
			if rmErr := os.Remove(outputPath); rmErr != nil {
				ctx.Infof("could not remove oversized encode: %v", rmErr)
			}
			outputPath = ""
		}

		// Every rung overflowed. Returning an error lets core fall back to the
		// progressive rendition the extractor already queued, which is worse
		// but playable - far better than sending a file Telegram will bounce.
		if outputPath == "" {
			return fmt.Errorf(
				"transcoded output still %d bytes after crf %d, over the %d limit",
				lastSize, crfLadder[len(crfLadder)-1], limit,
			)
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
