package models

type DownloadContext struct {
	MatchedContentID  string
	MatchedContentURL string
	MatchedGroups     map[string]string
	GroupSettings     *GroupSettings
	Extractor         *Extractor
}
