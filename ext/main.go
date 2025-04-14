package ext

import (
	"govd/ext/instagram"
	"govd/ext/pinterest"
	"govd/ext/reddit"
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
	// todo: add every ext lol
}
