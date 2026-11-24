package libav

import (
	"fmt"
	"os"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// MergeVideoWithAudioAACLC muxes a video-only rendition with an audio
// rendition, copying the video track and re-encoding the audio track to
// AAC-LC.
//
// The difference from MergeVideoWithAudio is the audio profile, and it is the
// whole reason this function exists. Facebook's DASH audio rendition is
// published as `mp4a.40.5`, which is HE-AAC (SBR) - the manifest even names
// the encoding tag `dash_ln_heaac_vbr3_audio`. A stream-copied HE-AAC track
// plays fine on Android and arrives silent in a WhatsApp status on iOS: the
// iOS status transcoder keeps the video track and drops an SBR audio track it
// cannot pass through, so the upload lands with picture and no sound.
//
// Instagram serves its progressive MP4 with plain AAC-LC and keeps its audio
// on both platforms, so AAC-LC is the profile to land on. Re-encoding the
// audio is cheap - the track is a couple of hundred kilobytes - and it costs
// nothing in video quality because the video is copied untouched.
//
// The video track is never re-encoded. The rendition is AV1 and a full
// AV1 -> H.264 pass costs minutes per reel plus a 1.16x-3.03x size blowup,
// which is the cost the DASH budget walk in the facebook extractor exists to
// avoid. Audio-only normalisation sidesteps that entirely.
func MergeVideoWithAudioAACLC(
	videoPath string,
	audioPath string,
	outputPath string,
) error {
	err := ffmpeg.Output(
		[]*ffmpeg.Stream{
			ffmpeg.Input(videoPath),
			ffmpeg.Input(audioPath),
		},
		outputPath,
		ffmpeg.KwArgs{
			"movflags":  "+faststart",
			"c:v":       "copy",
			"c:a":       "aac",
			"profile:a": "aac_low",
			"b:a":       "128k",
			"ac":        "2",
		}).
		Silent(true).
		OverWriteOutput().
		Run()

	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to merge files with aac-lc audio: %w", err)
	}

	return nil
}

// NormalizeAudioToAACLC rewrites a file's audio track to AAC-LC, copying every
// other stream untouched.
//
// This is the repair for a file that is already muxed - the progressive
// Facebook ladder hands over one MP4 with video and audio in the same
// container, so there is no second input to merge and nothing for
// MergeVideoWithAudioAACLC to join. Re-encoding the audio in place is the only
// way in.
//
// "map 0" keeps every stream and its order, so a file with subtitles or a
// thumbnail track keeps them. Both codecs are named explicitly rather than
// using a blanket "c": "copy" followed by "c:a": "aac": ffmpeg applies flags
// left to right, and the kwargs map has no order, so a blanket copy could
// land after the audio override and silently copy the SBR track through.
func NormalizeAudioToAACLC(inputPath string, outputPath string, probeBitrate int) error {
	// The target profile is not fixed here. AAC-LC decodes on everything, but
	// AAC-HE is what iOS uses for WhatsApp and what the sources themselves
	// carry; encoding HE down to LC strips the SBR extension and is audible as
	// a dull track on the receiver that mattered. TargetAACProfile reads
	// AAC_PROFILE so the operator decides, and defaults to HE.
	//
	// The source bitrate is passed in so the target follows it. Measured
	// 2026-11-24: a Facebook share post carries HE-AAC stereo 44100 Hz at
	// 48 kbps. Forcing b:a=128k at LC spent 2.7x the bits inventing detail the
	// source never had, which is what made the previous output sound worse.
	target := TargetAACProfile()
	bitrate := aacBitrateFor(target, probeBitrate)

	err := ffmpeg.Input(inputPath).
		Output(outputPath, ffmpeg.KwArgs{
			"map":       "0",
			"c:v":       "copy",
			"c:a":       "aac",
			"profile:a": target,
			"b:a":       fmt.Sprintf("%dk", bitrate/1000),
			"movflags":  "+faststart",
		}).
		Silent(true).
		OverWriteOutput().
		Run()

	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to normalise audio to %s: %w", target, err)
	}

	return nil
}
