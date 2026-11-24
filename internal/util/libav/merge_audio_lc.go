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
	// HE-AAC -> AAC-LC has to be re-encoded: the two are different profiles and
	// no container remux converts one to the other. What must NOT happen is a
	// fixed bitrate, which is what this function used to do.
	//
	// Measured 2026-11-24 on a Facebook share post: the source track is HE-AAC
	// stereo 44100 Hz at 48 kbps. Forcing b:a=128k spent 2.7x the bits and still
	// sounded worse, because the encoder had to invent detail the source never
	// carried. A copy of the source bitrate is the honest ceiling.
	//
	// The probe result is passed in so the target follows the source.
	bitrate := probeBitrate
	if bitrate <= 0 || bitrate > 256000 {
		bitrate = 96000 // HE-AAC -> LC needs headroom; 48k would be too thin
	}
	err := ffmpeg.Input(inputPath).
		Output(outputPath, ffmpeg.KwArgs{
			"map":       "0",
			"c:v":       "copy",
			"c:a":       "aac",
			"profile:a": "aac_low",
			"b:a":       fmt.Sprintf("%dk", bitrate/1000),
			"ac":        "2",
			"movflags":  "+faststart",
		}).
		Silent(true).
		OverWriteOutput().
		Run()

	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to normalise audio to aac-lc: %w", err)
	}

	return nil
}
