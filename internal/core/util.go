package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/plugins"
	"github.com/govdbot/govd/internal/util"
	"github.com/govdbot/govd/internal/util/download"
	"github.com/govdbot/govd/internal/util/libav"
)

var ErrNoMedia = errors.New("no media found")

func parseFormatFromDB(row *database.GetMediaFormatRow) *models.MediaFormat {
	return &models.MediaFormat{
		FormatID:   row.FormatID,
		FileID:     row.FileID,
		Type:       row.Type,
		AudioCodec: row.AudioCodec.MediaCodec,
		VideoCodec: row.VideoCodec.MediaCodec,
		FileSize:   row.FileSize.Int64,
		Duration:   row.Duration.Int32,
		Title:      row.Title.String,
		Artist:     row.Artist.String,
		Width:      row.Width.Int32,
		Height:     row.Height.Int32,
		Bitrate:    row.Bitrate.Int64,
	}
}

func getThumbnail(
	ctx *models.ExtractorContext,
	format *models.MediaFormat,
	filePath string,
) (string, error) {
	fileDir := filepath.Dir(filePath)
	fileName := filepath.Base(filePath)
	fileExt := filepath.Ext(fileName)
	fileBaseName := fileName[:len(fileName)-len(fileExt)]
	thumbnailFilePath := filepath.Join(fileDir, fileBaseName+".jpeg")
	ctx.FilesTracker.Add(thumbnailFilePath)

	if len(format.ThumbnailURL) > 0 {
		file, err := download.DownloadFileInMemory(
			ctx, format.ThumbnailURL,
			format.DownloadSettings,
		)
		if err == nil && file != nil {
			var size int
			if format.Type == database.MediaTypeAudio {
				// for audio, use a smaller thumbnail
				size = 320
			}
			bounds, cerr := util.ImgToJPEG(file, thumbnailFilePath, size)
			if cerr == nil {
				format.Width = bounds.W
				format.Height = bounds.H
				return thumbnailFilePath, nil
			}
			err = cerr
		}
		// The remote poster can expire or be geo-blocked while the media
		// itself is still reachable. Treat it as non-fatal: fall back to a
		// frame extracted from the downloaded video below.
		if format.Type != database.MediaTypeVideo {
			if err != nil {
				return "", fmt.Errorf("failed to get thumbnail: %w", err)
			}
			return thumbnailFilePath, nil
		}
		ctx.Warnf("poster thumbnail unavailable (%v), extracting frame from video", err)
	}

	if format.Type == database.MediaTypeVideo {
		return libav.ExtractVideoThumbnail(filePath, thumbnailFilePath)
	}

	return thumbnailFilePath, nil
}

func insertVideoInfo(format *models.MediaFormat, filePath string) {
	duration, width, height := util.ExtractMP4Metadata(filePath)
	if duration == 0 && width == 0 && height == 0 {
		duration, width, height = libav.ExtractVideoMetadata(filePath)
	}
	format.Duration = duration
	format.Width = width
	format.Height = height
}

func formatCaption(media *models.Media, username string, isEnabled bool) string {
	// Trim and early return if captions disabled or empty
	caption := strings.TrimSpace(media.Caption)
	if !isEnabled {
		return ""
	}
	// Fix rosak: truncate by runes not bytes (emoji/multibyte safe).
	// Reserve for header+wrapper. Telegram limit 1024 quoted text but we keep 1024 total safe.
	// Old code did caption[:600] byte slice -> broken unicode.
	const maxRunes = 900
	if caption != "" {
		r := []rune(caption)
		if len(r) > maxRunes {
			caption = string(r[:maxRunes-3]) + "..."
		}
	}
	formatText := func(s string) string {
		s = strings.ReplaceAll(s, "{{username}}", username)
		s = strings.ReplaceAll(s, "{{url}}", media.ContentURL)
		s = strings.ReplaceAll(s, "{{text}}", util.Unquote(caption))
		return s
	}
	// Always include source header: <a href='{{url}}'>source</a> - @{{username}}
	// This gives clickable source button + bot mention like original upstream
	header := formatText(config.Env.CaptionsHeader)
	var description string
	if caption != "" {
		description = formatText(config.Env.CaptionsDescription)
	}
	if caption == "" {
		// If no caption, still return header so source+bot visible
		return header
	}
	return header + "\n" + description
}

// utility function to merge audio into video formats with no audio
func mergeFormats(item *models.MediaItem, format *models.DownloadedFormat) {
	if format.Format.Type != database.MediaTypeVideo {
		return
	}
	if format.Format.AudioCodec != "" {
		return
	}
	audioFormat := item.GetDefaultAudioFormat()
	if audioFormat == nil {
		return
	}
	format.Format.AudioCodec = audioFormat.AudioCodec
	format.Format.Plugins = append(
		format.Format.Plugins,
		plugins.MergeAudio,
	)
}

// mediaCodecFromProbe maps an ffprobe codec_name onto the MediaCodec enum.
// Only the names that appear in this project's extractors are handled; an
// unknown name returns "" so the caller keeps whatever it declared.
func mediaCodecFromProbe(name string) database.MediaCodec {
	switch name {
	case "aac":
		return database.MediaCodecAac
	case "opus":
		return database.MediaCodecOpus
	case "vorbis":
		return database.MediaCodecVorbis
	case "mp3":
		return database.MediaCodecMp3
	case "flac":
		return database.MediaCodecFlac
	}
	return ""
}

// verifyAudioCodec replaces the declared audio codec with the one ffprobe
// actually finds in the delivered file, and logs the profile alongside it.
//
// The declarations upstream are hardcoded: the DASH, progressive and
// Instagram builders all set AAC without asking what the stream is, and an
// AAC stream can be LC, HE-AAC (SBR) or HE-AACv2 (SBR+PS). Telegram does not
// accept the three equally, so when a send is rejected the log has to say
// which one it was.
//
// Three cases, and they are not the same case:
//   - probe failed        → change nothing, the codec is still unknown
//   - no audio stream     → clear the declared codec, it was a guess and wrong
//   - stream found        → adopt the real codec name
//
// The second case is what keeps a silent screen recording from being sent as
// AAC. It only clears the field; FileExtension keeps naming the container.
func verifyAudioCodec(ctx *models.ExtractorContext, format *models.DownloadedFormat) {
	if format == nil || format.Format == nil || format.FilePath == "" {
		return
	}

	probe, found, err := libav.ProbeAudioStream(format.FilePath)
	if err != nil {
		ctx.Debugf("audio probe: skipped, ffprobe failed: %v", err)
		return
	}

	declared := format.Format.AudioCodec

	if !found {
		if declared != "" {
			format.Format.AudioCodec = ""
			ctx.Infof(
				"audio probe: declared %q but file has no audio stream - cleared",
				declared,
			)
		}
		return
	}

	actual := mediaCodecFromProbe(probe.Codec)
	if actual != "" && actual != declared {
		format.Format.AudioCodec = actual
		ctx.Infof(
			"audio probe: declared %q but stream is %q (profile=%q %dch %dHz) - corrected",
			declared, probe.Codec, probe.Profile, probe.Channels, probe.SampleRate,
		)
		return
	}

	ctx.Debugf(
		"audio probe: %s profile=%q %dch %dHz %dbps",
		probe.Codec, probe.Profile, probe.Channels, probe.SampleRate, probe.BitRate,
	)
}
