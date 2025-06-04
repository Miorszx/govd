package nicovideo

import (
	"regexp"

	"github.com/govdbot/govd/enums"
	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util"
	"github.com/govdbot/govd/util/networking"
)

var Extractor = &models.Extractor{
	Name:       "NicoVideo",
	CodeName:   "nicovideo",
	Type:       enums.ExtractorTypeSingle,
	Category:   enums.ExtractorCategorySocial,
	URLPattern: regexp.MustCompile(`https?://(?:(?:www\.|secure\.|sp\.)?nicovideo\.jp/watch|nico\.ms)/(?P<id>(?:[a-z]{2})?[0-9]+)`),
	Host:       []string{"nicovideo"},

	Run: func(ctx *models.DownloadContext) (*models.ExtractorResponse, error) {
		video, err := GetVideo(ctx)
		if err != nil {
			return nil, err
		}
		return &models.ExtractorResponse{
			MediaList: []*models.Media{video},
		}, nil
	},
}

func GetVideo(ctx *models.DownloadContext) (*models.Media, error) {
	client := networking.GetExtractorHTTPClient(ctx.Extractor)
	cookies := util.GetExtractorCookies(ctx.Extractor)

	videoID := ctx.MatchedContentID
	videoURL := ctx.MatchedContentURL
	media := ctx.Extractor.NewMedia(
		videoID,
		videoURL,
	)

	serverResponse, sessionID, err := GetServerResponse(client, cookies, videoID)
	if err != nil {
		return nil, err
	}
	if serverResponse.Meta.Code == "FORBIDDEN" {
		return nil, util.ErrUnavailable
	}
	if serverResponse.Data.Response.Video.IsPrivate {
		return nil, util.ErrUnavailable
	}
	cookies = append(cookies, sessionID)

	media.SetCaption(serverResponse.Data.Response.Video.Title)
	media.NSFW = serverResponse.Data.Response.Video.Rating.IsAdult
	duration := serverResponse.Data.Response.Video.Duration

	formats, err := GetFormats(client, cookies, serverResponse)
	if err != nil {
		return nil, err
	}
	for _, format := range formats {
		format.Duration = duration
		media.AddFormat(format)
	}

	return media, nil
}
