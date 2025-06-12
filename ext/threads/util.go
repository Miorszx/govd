package threads

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/enums"
	"github.com/govdbot/govd/models"

	"github.com/PuerkitoBio/goquery"
)

var (
	pageBase  = "https://www.threads.net/@_/post/%s"
	embedBase = pageBase + "/embed"

	headers = map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language":           "en-GB,en;q=0.9",
		"Cache-Control":             "max-age=0",
		"Dnt":                       "1",
		"Priority":                  "u=0, i",
		"Sec-Ch-Ua":                 `Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99`,
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        "macOS",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}

	relayDataPattern = regexp.MustCompile(`adp_BarcelonaPostPageDirectQueryRelayPreloader_[a-z0-9]+",({"__bbox":.*?})\]\],\["CometResourceScheduler"`)
)

func FindPostRelayData(
	ctx *models.DownloadContext,
	body []byte,
) (*Post, error) {
	threadID := ctx.MatchedContentID
	match := relayDataPattern.FindSubmatch(body)
	if len(match) < 2 {
		return nil, ErrRelayDataNotFound
	}
	relayData := match[1]
	if len(relayData) == 0 {
		return nil, ErrRelayDataInvalid
	}
	var data RelayData
	err := sonic.ConfigFastest.Unmarshal(relayData, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse relay data: %w", err)
	}
	if data.Bbox == nil {
		return nil, ErrRelayDataBboxNotFound
	}
	if data.Bbox.Result == nil {
		return nil, ErrRelayDataResultNotFound
	}
	if data.Bbox.Result.Data == nil {
		return nil, ErrRelayDataDataNotFound
	}
	if data.Bbox.Result.Data.Data == nil {
		return nil, ErrRelayDataDataNotFound
	}
	edges := data.Bbox.Result.Data.Data.Edges
	if len(edges) == 0 {
		return nil, ErrRelayDataEdgesNotFound
	}
	var post *Post
	for _, edge := range edges {
		if edge.Node == nil {
			continue
		}
		if len(edge.Node.ThreadItems) == 0 {
			continue
		}
		if edge.Node.ThreadItems[0] == nil {
			continue
		}
		if edge.Node.ThreadItems[0].Post == nil {
			continue
		}
		if edge.Node.ThreadItems[0].Post.Code != threadID {
			continue
		}
		post = edge.Node.ThreadItems[0].Post
		break
	}
	if post == nil {
		return nil, ErrRelayDataPostNotFound
	}
	return post, nil
}

func ParsePostRelayData(
	ctx *models.DownloadContext,
	post *Post,
) ([]*models.Media, error) {
	contentID := ctx.MatchedContentID
	contentURL := ctx.MatchedContentURL

	if post.CarouselMedia != nil {
		mediaList := make([]*models.Media, 0, len(post.CarouselMedia))
		for _, carouselItem := range post.CarouselMedia {
			media := ctx.Extractor.NewMedia(
				contentID,
				contentURL,
			)
			media.SetCaption(post.Caption.Text)
			format, err := ParsePostMedia(media, carouselItem)
			if err != nil {
				return nil, err
			}
			media.AddFormat(format)
			mediaList = append(mediaList, media)
		}
		return mediaList, nil
	}
	media := ctx.Extractor.NewMedia(
		contentID,
		contentURL,
	)
	media.SetCaption(post.Caption.Text)
	format, err := ParsePostMedia(media, post)
	if err != nil {
		return nil, err
	}
	media.AddFormat(format)
	return []*models.Media{media}, nil
}

func ParsePostMedia(
	media *models.Media,
	post *Post,
) (*models.MediaFormat, error) {
	if post.VideoVersions != nil {
		return &models.MediaFormat{
			Type:       enums.MediaTypeVideo,
			FormatID:   "video",
			URL:        []string{post.VideoVersions[0].URL},
			VideoCodec: enums.MediaCodecAVC,
			AudioCodec: enums.MediaCodecAAC,
		}, nil
	}
	if post.ImageVersions2 != nil {
		best := FindBestCandidate(post.ImageVersions2.Candidates)
		if best != nil {
			return &models.MediaFormat{
				Type:     enums.MediaTypePhoto,
				FormatID: "image",
				URL:      []string{best.URL},
			}, nil
		}
	}
	return nil, errors.New("no suitable media format found")
}

func ParseEmbedMedia(
	ctx *models.DownloadContext,
	body []byte,
) ([]*models.Media, error) {
	var mediaList []*models.Media

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed parsing HTML: %w", err)
	}

	contentID := ctx.MatchedContentID
	contentURL := ctx.MatchedContentURL

	var caption string
	doc.Find(".BodyTextContainer").Each(func(i int, c *goquery.Selection) {
		caption = c.Text()
	})

	doc.Find(".MediaContainer, .SoloMediaContainer").Each(func(i int, container *goquery.Selection) {
		container.Find("video").Each(func(j int, vid *goquery.Selection) {
			sourceEl := vid.Find("source")
			src, exists := sourceEl.Attr("src")
			if exists {
				media := ctx.Extractor.NewMedia(
					contentID,
					contentURL,
				)
				media.SetCaption(caption)
				media.AddFormat(&models.MediaFormat{
					Type:       enums.MediaTypeVideo,
					FormatID:   "video",
					URL:        []string{src},
					VideoCodec: enums.MediaCodecAVC,
					AudioCodec: enums.MediaCodecAAC,
				})
				mediaList = append(mediaList, media)
			}
		})
		container.Find("img").Each(func(j int, img *goquery.Selection) {
			src, exists := img.Attr("src")
			if exists {
				media := ctx.Extractor.NewMedia(
					contentID,
					contentURL,
				)
				media.SetCaption(caption)
				media.AddFormat(&models.MediaFormat{
					Type:     enums.MediaTypePhoto,
					FormatID: "image",
					URL:      []string{src},
				})
				mediaList = append(mediaList, media)
			}
		})
	})

	return mediaList, nil
}

func FindBestCandidate(candidates []*Candidates) *Candidates {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Width > best.Width {
			best = candidate
		}
	}
	return best
}
