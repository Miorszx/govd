package ext

import (
	"govd/ext/instagram"
	"govd/ext/ninegag"
	"govd/ext/pinterest"
	"govd/ext/reddit"
	"govd/ext/redgifs"
	"govd/ext/tiktok"
	"govd/ext/twitter"
	"govd/models"
)

var List = []*models.Extractor{
	tiktok.Extractor,
	tiktok.VMExtractor,
	instagram.Extractor,
	instagram.StoriesExtractor,
	instagram.ShareURLExtractor,
	twitter.Extractor,
	twitter.ShortExtractor,
	pinterest.Extractor,
	pinterest.ShortExtractor,
	reddit.Extractor,
	reddit.ShortExtractor,
	ninegag.Extractor,
	redgifs.Extractor,
	// todo: add every ext lol
}
