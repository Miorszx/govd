package models

import (
	"govd/enums"
	"regexp"
)

type Extractor struct {
	Name       string
	CodeName   string
	Type       enums.ExtractorType
	Category   enums.ExtractorCategory
	URLPattern *regexp.Regexp
	IsDRM      bool
	IsRedirect bool

	Run func(*DownloadContext) (*ExtractorResponse, error)
}

type ExtractorResponse struct {
	MediaList []*Media
	URL       string // redirected URL
}

func (extractor *Extractor) NewMedia(
	contentID string,
	contentURL string,
) *Media {
	return &Media{
		ContentID:         contentID,
		ContentURL:        contentURL,
		ExtractorCodeName: extractor.CodeName,
	}
}
