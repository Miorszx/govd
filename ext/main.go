package ext

import (
	"govd/ext/instagram"
	"govd/ext/ninegag"
	"govd/ext/pinterest"
	"govd/ext/reddit"
	"govd/ext/redgifs"
	"govd/ext/soundcloud"
	"govd/ext/threads"
	"govd/ext/tiktok"
	"govd/ext/twitter"
	"govd/ext/youtube"
	"govd/models"
)

var List = []*models.Extractor{
	youtube.Extractor,
	tiktok.Extractor,
	tiktok.VMExtractor,
	instagram.Extractor,
	instagram.StoriesExtractor,
	instagram.ShareURLExtractor,
	threads.Extractor,
	twitter.Extractor,
	twitter.ShortExtractor,
	pinterest.Extractor,
	pinterest.ShortExtractor,
	reddit.Extractor,
	reddit.ShortExtractor,
	ninegag.Extractor,
	redgifs.Extractor,
	soundcloud.Extractor,
	soundcloud.ShortExtractor,
}
