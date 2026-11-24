package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util/libav"
)

// NormalizeAudioLC guarantees the delivered file carries AAC-LC audio.
//
// It exists because not every video reaches the disk through MergeAudio.
// Facebook's progressive ladder serves a single muxed MP4 - H.264 plus audio
// in one file - so no merge runs and nothing ever inspects the audio profile.
// If that audio turns out to be HE-AAC, the status upload loses its sound on
// iOS exactly as the DASH path did before MergeAudio learned to re-encode.
//
// The plugin is cheap by design: when the audio is already LC, or when the
// probe fails, it returns nil and the file is left byte-identical. A remux
// only happens when the measured profile is something else - the video is
// always copied, so nothing is lost but the SBR layer.
//
// Attach it after any merge plugin: a merge decision made on the source
// rendition is more accurate than one made on the muxed result, and running
// second here means it usually finds LC and does nothing.
var NormalizeAudioLC = &models.Plugin{
	ID: "normalize_audio_lc",
	RunFunc: func(ctx *models.ExtractorContext, item *models.MediaItem, format *models.DownloadedFormat) error {
		if format == nil || format.FilePath == "" {
			return nil
		}

		probe, found, err := libav.ProbeAudioStream(format.FilePath)
		if err != nil {
			ctx.Debugf("normalize audio: probe failed, leaving file alone: %v", err)
			return nil
		}
		if !found {
			// Silent video: nothing to normalise, and inventing an audio
			// track here would be worse than sending silence.
			return nil
		}
		if strings.EqualFold(probe.Profile, "LC") {
			ctx.Debugf(
				"normalize audio: already %s profile=%q - no remux",
				probe.Codec, probe.Profile,
			)
			return nil
		}

		ctx.Infof(
			"normalize audio: %s profile=%q (%dch %dHz) - remuxing audio to aac-lc",
			probe.Codec, probe.Profile, probe.Channels, probe.SampleRate,
		)

		outputPath := strings.TrimSuffix(
			format.FilePath,
			filepath.Ext(format.FilePath),
		) + "_lc" + filepath.Ext(format.FilePath)
		ctx.FilesTracker.Add(outputPath)

		if err := libav.NormalizeAudioToAACLC(format.FilePath, outputPath, int(probe.BitRate)); err != nil {
			// A failed normalise must not cost the whole download: the file
			// still plays on Android, and Telegram still accepts it.
			ctx.Warnf("normalize audio: remux failed, keeping original: %v", err)
			return nil
		}

		if err := os.Rename(outputPath, format.FilePath); err != nil {
			return fmt.Errorf("failed to replace file with normalised audio: %w", err)
		}
		return nil
	},
}
