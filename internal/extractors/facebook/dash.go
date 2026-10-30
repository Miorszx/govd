package facebook

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"strconv"
	"strings"
)

// dashRepresentation is one <Representation> entry of a Facebook DASH manifest.
type dashRepresentation struct {
	ID        string `xml:"id,attr"`
	Bandwidth int64  `xml:"bandwidth,attr"`
	Codecs    string `xml:"codecs,attr"`
	Width     int32  `xml:"width,attr"`
	Height    int32  `xml:"height,attr"`
	BaseURL   string `xml:"BaseURL"`
}

type dashAdaptationSet struct {
	ContentType     string               `xml:"contentType,attr"`
	Representations []dashRepresentation `xml:"Representation"`
}

type dashPeriod struct {
	AdaptationSets []dashAdaptationSet `xml:"AdaptationSet"`
}

type dashManifest struct {
	MediaPresentationDuration string       `xml:"mediaPresentationDuration,attr"`
	Periods                   []dashPeriod `xml:"Period"`
}

// dashTracks is the pair of single-file renditions the reel DASH manifest
// exposes: one video-only stream and one audio-only stream. Facebook only
// serves these as AV1/VP9, never H.264, so the caller has to re-encode.
type dashTracks struct {
	VideoURL   string
	AudioURL   string
	Width      int32
	Height     int32
	VideoCodec string
	AudioCodec string
	VideoSize  int64
	Duration   int32
}

// extractManifestXML pulls the inline DASH manifest out of a Facebook page
// body. The manifest is embedded as a JSON string field, so the value is
// escaped and cannot be matched with a plain regex capture.
func extractManifestXML(body []byte) string {
	const key = `"manifest_xml":"`
	s := string(body)
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key):]

	// Walk the JSON string literal until the closing quote that is not
	// preceded by a backslash, then let encoding/json do the unescaping.
	end := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\\' {
			i++
			continue
		}
		if rest[i] == '"' {
			end = i
			break
		}
	}
	if end < 0 {
		return ""
	}

	var out string
	if err := json.Unmarshal([]byte(`"`+rest[:end]+`"`), &out); err != nil {
		return ""
	}
	return out
}

// parseDASHManifest selects the highest-resolution video rendition and the
// audio rendition from a Facebook DASH manifest.
func parseDASHManifest(manifestXML string) *dashTracks {
	if manifestXML == "" {
		return nil
	}
	var m dashManifest
	if err := xml.Unmarshal([]byte(manifestXML), &m); err != nil {
		return nil
	}

	tracks := &dashTracks{Duration: parseDASHDuration(m.MediaPresentationDuration)}

	for _, period := range m.Periods {
		for _, set := range period.AdaptationSets {
			for _, rep := range set.Representations {
				// BaseURL is XML character data, so a signed CDN URL comes back
				// with its query separators escaped as &amp;. Sending that
				// literally makes the CDN reject the request with 403, so the
				// entity has to be decoded before the URL is used.
				url := html.UnescapeString(strings.TrimSpace(rep.BaseURL))
				if url == "" {
					continue
				}
				codec := codecFromDASHAttr(rep.Codecs)
				if codec == "" {
					continue
				}
				if set.ContentType == "audio" || rep.Width == 0 {
					if tracks.AudioURL == "" && isAudioCodecName(codec) {
						tracks.AudioURL = url
						tracks.AudioCodec = codec
					}
					continue
				}
				if tracks.VideoURL == "" || rep.Height > tracks.Height {
					tracks.VideoURL = url
					tracks.VideoCodec = codec
					tracks.Width = rep.Width
					tracks.Height = rep.Height
					tracks.VideoSize = estimateTrackSize(rep.Bandwidth, tracks.Duration)
				}
			}
		}
	}

	if tracks.VideoURL == "" {
		return nil
	}
	return tracks
}

// codecFromDASHAttr maps a DASH codecs attribute (RFC 6381) onto the internal
// codec names. The attribute can list several codecs separated by commas.
func codecFromDASHAttr(attr string) string {
	for _, part := range strings.Split(attr, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "av01"):
			return "av1"
		case strings.HasPrefix(part, "avc1"), strings.HasPrefix(part, "avc3"):
			return "avc"
		case strings.HasPrefix(part, "vp09"):
			return "vp9"
		case strings.HasPrefix(part, "hvc1"), strings.HasPrefix(part, "hev1"):
			return "hevc"
		case strings.HasPrefix(part, "mp4a"):
			return "aac"
		case strings.HasPrefix(part, "opus"):
			return "opus"
		}
	}
	return ""
}

func isAudioCodecName(codec string) bool {
	switch codec {
	case "aac", "opus", "vorbis", "mp3", "flac":
		return true
	}
	return false
}

// parseDASHDuration converts an ISO 8601 duration such as PT58.517334S into
// whole seconds.
func parseDASHDuration(v string) int32 {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "PT") {
		return 0
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "PT"), "S")
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int32(f + 0.5)
}

// estimateTrackSize turns a DASH bandwidth (bits per second) plus duration
// into an approximate byte size.
func estimateTrackSize(bandwidth int64, duration int32) int64 {
	if bandwidth <= 0 || duration <= 0 {
		return 0
	}
	return bandwidth * int64(duration) / 8
}
