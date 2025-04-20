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
	Host       []string
	IsDRM      bool
	IsRedirect bool
	Client     HTTPClient

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

type ExtractorConfig struct {
	HTTPProxy    string `yaml:"http_proxy"`
	HTTPSProxy   string `yaml:"https_proxy"`
	NoProxy      string `yaml:"no_proxy"`
	EdgeProxyURL string `yaml:"edge_proxy_url"`
}
