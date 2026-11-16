package libav

import (
	"encoding/json"
	"strconv"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// AudioProbe is what ffprobe reports about the first audio stream of a file.
//
// Profile is the field that matters for Telegram: an "aac" stream is either
// LC, HE-AAC (SBR) or HE-AACv2 (SBR+PS), and the three do not fail the same
// way. Declaring the codec without the profile is what hides that difference
// in the logs.
type AudioProbe struct {
	Codec      string
	Profile    string
	Channels   int32
	SampleRate int32
	BitRate    int64
}

// audioProbeData mirrors the subset of ffprobe JSON this needs. The video
// probe in probe.go already carries its own struct; this one is separate so
// adding audio fields cannot shift the existing width/height/duration parse.
type audioProbeData struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Profile    string `json:"profile"`
		Channels   int32  `json:"channels"`
		SampleRate string `json:"sample_rate"`
		BitRate    string `json:"bit_rate"`
	} `json:"streams"`
}

// ProbeAudioStream reads the first audio stream of inputPath.
//
// The three outcomes are distinct and callers must not collapse them:
//   - err != nil          → ffprobe did not run; nothing is known, change nothing
//   - found == false      → ffprobe ran, the file genuinely has no audio stream
//   - found == true       → AudioProbe describes the stream
//
// Treating "no audio stream" and "probe failed" as the same answer is how a
// silent video ends up labelled AAC forever.
//
// No logging here on purpose: logger.L is only initialised once the bot
// process starts, and a probe that cannot write a log line must still return
// its answer. The caller logs through ExtractorContext, which is always set.
func ProbeAudioStream(inputPath string) (AudioProbe, bool, error) {
	raw, err := ffmpeg.Probe(inputPath)
	if err != nil {
		return AudioProbe{}, false, err
	}

	data := &audioProbeData{}
	if err := json.Unmarshal([]byte(raw), data); err != nil {
		return AudioProbe{}, false, err
	}

	for _, s := range data.Streams {
		if s.CodecType != "audio" || s.CodecName == "" {
			continue
		}

		out := AudioProbe{
			Codec:    s.CodecName,
			Profile:  s.Profile,
			Channels: s.Channels,
		}
		// ffprobe emits these as JSON strings, not numbers.
		if v, err := strconv.ParseInt(s.SampleRate, 10, 32); err == nil {
			out.SampleRate = int32(v)
		}
		if v, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
			out.BitRate = v
		}
		return out, true, nil
	}

	return AudioProbe{}, false, nil
}
