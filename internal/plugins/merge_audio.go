package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util/libav"
)

// MergeAudio pulls the item's audio-only format and muxes it into the
// video-only one that was just downloaded.
//
// The audio is re-encoded to AAC-LC when the source is not already LC. This is
// the difference that decides whether a WhatsApp status on iPhone keeps its
// sound:
//
//   - Facebook's DASH audio rendition is `mp4a.40.5`, HE-AAC with SBR (the
//     manifest's own encoding tag reads dash_ln_heaac_vbr3_audio). Stream
//     copied into the MP4 it plays on Android and lands silent on iOS, because
//     the iOS status transcoder drops an SBR track instead of passing it
//     through.
//   - Instagram's progressive MP4 is plain AAC-LC and keeps its audio on both
//     platforms.
//
// So the target profile is AAC-LC and the check is done on the bytes that
// actually arrived, not on the manifest's claim. An LC source is still stream
// copied - the re-encode is only paid when the profile is something else.
//
// The video track is never touched: MergeVideoWithAudio copies it.
var MergeAudio = &models.Plugin{
	ID: "merge_audio",
	RunFunc: func(ctx *models.ExtractorContext, item *models.MediaItem, format *models.DownloadedFormat) error {
		filePath := format.FilePath

		audioFormat := item.GetDefaultAudioFormat()
		if audioFormat == nil {
			return fmt.Errorf("no audio format found")
		}

		if ctx.DownloadFunc == nil {
			return fmt.Errorf("download function not available in context")
		}

		downloadedAudioFormat, err := ctx.DownloadFunc(ctx, 0, audioFormat)
		if err != nil {
			return fmt.Errorf("failed to download audio format: %w", err)
		}

		outputPath := strings.TrimSuffix(
			filePath,
			filepath.Ext(filePath),
		) + "_remuxed" + filepath.Ext(filePath)
		ctx.FilesTracker.Add(outputPath)

		if needsAACLCReencode(ctx, downloadedAudioFormat.FilePath) {
			err = libav.MergeVideoWithAudioAACLC(
				format.FilePath,
				downloadedAudioFormat.FilePath,
				outputPath,
			)
		} else {
			err = libav.MergeVideoWithAudio(
				format.FilePath,
				downloadedAudioFormat.FilePath,
				outputPath,
			)
		}
		if err != nil {
			return fmt.Errorf("failed to merge video with audio: %w", err)
		}

		os.Rename(outputPath, filePath)

		return nil
	},
}

// needsAACLCReencode reports whether the downloaded audio track has to be
// re-encoded to land on AAC-LC.
//
// A failed probe answers false: the file was downloaded and playable enough to
// get this far, and re-encoding on a guess would cost quality for nothing. The
// post-download probe in core still records what was actually delivered.
func needsAACLCReencode(ctx *models.ExtractorContext, audioPath string) bool {
	probe, found, err := libav.ProbeAudioStream(audioPath)
	if err != nil {
		ctx.Debugf("merge audio: probe failed, stream copying: %v", err)
		return false
	}
	if !found {
		ctx.Debugf("merge audio: no audio stream in %s, stream copying", audioPath)
		return false
	}

	// ffprobe names the profile "LC" for plain AAC-LC and "HE-AAC" /
	// "HE-AACv2" for the SBR variants. Anything that is not LC is re-encoded.
	if strings.EqualFold(probe.Profile, "LC") {
		ctx.Debugf(
			"merge audio: source is %s profile=%q - stream copy",
			probe.Codec, probe.Profile,
		)
		return false
	}

	ctx.Infof(
		"merge audio: source is %s profile=%q (%dch %dHz) - re-encoding to aac-lc",
		probe.Codec, probe.Profile, probe.Channels, probe.SampleRate,
	)
	return true
}
