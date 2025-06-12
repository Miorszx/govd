package reddit

import (
	"fmt"
	"regexp"

	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util/parser"
)

const (
	hlsURLFormat = "https://v.redd.it/%s/HLSPlaylist.m3u8"
)

var videoURLPattern = regexp.MustCompile(`https?://v\.redd\.it/([^/]+)`)

func GetHLSFormats(
	videoURL string,
	duration int64,
) ([]*models.MediaFormat, error) {
	matches := videoURLPattern.FindStringSubmatch(videoURL)
	if len(matches) < 2 {
		return nil, nil
	}

	videoID := matches[1]
	hlsURL := fmt.Sprintf(hlsURLFormat, videoID)

	formats, err := parser.ParseM3U8FromURL(hlsURL, nil)
	if err != nil {
		return nil, err
	}

	for _, format := range formats {
		format.Duration = duration
	}

	return formats, nil
}
