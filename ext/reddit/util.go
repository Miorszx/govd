package reddit

import (
	"fmt"
	"govd/models"
	"govd/util"
	"govd/util/parser"
	"regexp"
)

const (
	hlsURLFormat = "https://v.redd.it/%s/HLSPlaylist.m3u8"
)

var videoURLPattern = regexp.MustCompile(`https?://v\.redd\.it/([^/]+)`)

func GetHLSFormats(videoURL string, thumbnail string, duration int64) ([]*models.MediaFormat, error) {
	matches := videoURLPattern.FindStringSubmatch(videoURL)
	if len(matches) < 2 {
		return nil, nil
	}

	videoID := matches[1]
	hlsURL := fmt.Sprintf(hlsURLFormat, videoID)

	formats, err := parser.ParseM3U8FromURL(hlsURL)
	if err != nil {
		return nil, err
	}

	for _, format := range formats {
		format.Duration = duration
		if thumbnail != "" {
			format.Thumbnail = []string{util.FixURL(thumbnail)}
		}
	}

	return formats, nil
}
