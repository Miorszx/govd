package parser

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"

	"govd/enums"
	"govd/models"

	"github.com/grafov/m3u8"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func ParseM3U8Content(
	content []byte,
	baseURL string,
) ([]*models.MediaFormat, error) {
	baseURLObj, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}

	buf := bytes.NewBuffer(content)
	playlist, listType, err := m3u8.DecodeFrom(buf, true)
	if err != nil {
		return nil, fmt.Errorf("failed parsing m3u8: %w", err)
	}

	var formats []*models.MediaFormat

	if listType == m3u8.MASTER {
		masterpl := playlist.(*m3u8.MasterPlaylist)

		for _, variant := range masterpl.Variants {
			if variant == nil || variant.URI == "" {
				continue
			}

			width, height := int64(0), int64(0)
			if variant.Resolution != "" {
				var w, h int
				if _, err := fmt.Sscanf(variant.Resolution, "%dx%d", &w, &h); err == nil {
					width, height = int64(w), int64(h)
				}
			}

			format := &models.MediaFormat{
				Type:       enums.MediaTypeVideo,
				FormatID:   fmt.Sprintf("hls-%d", variant.Bandwidth/1000),
				VideoCodec: getCodecFromCodecs(variant.Codecs),
				AudioCodec: getAudioCodecFromCodecs(variant.Codecs),
				Bitrate:    int64(variant.Bandwidth),
				Width:      width,
				Height:     height,
			}

			variantURL := resolveURL(baseURLObj, variant.URI)
			format.URL = []string{variantURL}

			variantContent, err := fetchContent(variantURL)
			if err == nil {
				variantFormats, err := ParseM3U8Content(variantContent, variantURL)
				if err == nil && len(variantFormats) > 0 {
					format.Segments = variantFormats[0].Segments
					if variantFormats[0].Duration > 0 {
						format.Duration = variantFormats[0].Duration
					}
				}
			}

			formats = append(formats, format)
		}

		return formats, nil
	}

	if listType == m3u8.MEDIA {
		mediapl := playlist.(*m3u8.MediaPlaylist)

		var segments []string
		var totalDuration float64

		for _, segment := range mediapl.Segments {
			if segment != nil && segment.URI != "" {
				segmentURL := segment.URI
				if !strings.HasPrefix(segmentURL, "http://") && !strings.HasPrefix(segmentURL, "https://") {
					segmentURL = resolveURL(baseURLObj, segmentURL)
				}

				segments = append(segments, segmentURL)
				totalDuration += segment.Duration
			}
		}

		format := &models.MediaFormat{
			Type:       enums.MediaTypeVideo,
			FormatID:   "hls",
			VideoCodec: enums.MediaCodecAVC,
			AudioCodec: enums.MediaCodecAAC,
			Duration:   int64(totalDuration),
			URL:        []string{baseURL},
			Segments:   segments,
		}

		return []*models.MediaFormat{format}, nil
	}

	return nil, errors.New("unsupported m3u8 playlist type")
}

func ParseM3U8FromURL(url string) ([]*models.MediaFormat, error) {
	body, err := fetchContent(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch m3u8 content: %w", err)
	}
	return ParseM3U8Content(body, url)
}

func fetchContent(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func getCodecFromCodecs(codecs string) enums.MediaCodec {
	if strings.Contains(codecs, "avc") || strings.Contains(codecs, "h264") {
		return enums.MediaCodecAVC
	} else if strings.Contains(codecs, "hvc") || strings.Contains(codecs, "h265") {
		return enums.MediaCodecHEVC
	} else if strings.Contains(codecs, "av01") {
		return enums.MediaCodecAV1
	} else if strings.Contains(codecs, "vp9") {
		return enums.MediaCodecVP9
	} else if strings.Contains(codecs, "vp8") {
		return enums.MediaCodecVP8
	}
	return enums.MediaCodecAVC
}

func getAudioCodecFromCodecs(codecs string) enums.MediaCodec {
	if strings.Contains(codecs, "mp4a") {
		return enums.MediaCodecAAC
	} else if strings.Contains(codecs, "opus") {
		return enums.MediaCodecOpus
	} else if strings.Contains(codecs, "mp3") {
		return enums.MediaCodecMP3
	} else if strings.Contains(codecs, "flac") {
		return enums.MediaCodecFLAC
	} else if strings.Contains(codecs, "vorbis") {
		return enums.MediaCodecVorbis
	}
	return enums.MediaCodecAAC
}

func resolveURL(base *url.URL, uri string) string {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return uri
	}
	ref, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return base.ResolveReference(ref).String()
}
