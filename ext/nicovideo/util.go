package nicovideo

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/logger"
	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util"
	"github.com/govdbot/govd/util/parser"
	"go.uber.org/zap"
)

var (
	urlBase    = "https://www.nicovideo.jp/watch/"
	playerBase = "https://nvapi.nicovideo.jp/v1/watch/%s/access-rights/hls?actionTrackId=%s"

	webHeaders = map[string]string{
		// bypass 403
		"User-Agent": "Twitterbot/1.0",
	}

	serverResponsePattern = regexp.MustCompile(`<meta\s+name="server-response"\s+content="([^"]+)"\s*/>`)
)

func GetServerResponse(
	client models.HTTPClient,
	cookies []*http.Cookie,
	videoID string,
) (*ServerResponse, *http.Cookie, error) {
	resp, err := util.FetchPage(
		client,
		http.MethodGet,
		urlBase+videoID,
		nil,
		webHeaders,
		cookies,
	)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// debugging
	logger.WriteFile("nv_webpage", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("bad response: %s", resp.Status)
	}

	sessionID := util.GetCookieByName(resp.Cookies(), "nicosid")
	if sessionID == nil {
		return nil, nil, ErrNoSessionIDFound
	}
	zap.S().Debugf("session ID: %s", sessionID.Value)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	matches := serverResponsePattern.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, nil, ErrServerResponseNotFound
	}

	data := html.UnescapeString(string(matches[1]))

	reader := strings.NewReader(data)

	var serverResponse *ServerResponse
	decoder := sonic.ConfigFastest.NewDecoder(reader)
	err = decoder.Decode(&serverResponse)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return serverResponse, sessionID, nil
}

func GetFormats(
	client models.HTTPClient,
	cookies []*http.Cookie,
	serverResponse *ServerResponse,
) ([]*models.MediaFormat, error) {
	domand := serverResponse.Data.Response.Media.Domand
	if domand == nil {
		return nil, ErrNoDomandDataFound
	}
	nicoClient := serverResponse.Data.Response.Client
	if nicoClient == nil {
		return nil, ErrNoClientDataFound
	}
	videos := domand.Videos
	if len(videos) == 0 {
		return nil, ErrNoVideoDataFound
	}
	audios := domand.Audios
	if len(audios) == 0 {
		return nil, ErrNoAudioDataFound
	}
	accessKey := domand.AccessRightKey
	if accessKey == "" {
		return nil, ErrNoAccessKeyFound
	}
	zap.S().Debugf("access key: %s", accessKey)

	trackID := nicoClient.WatchTrackID
	if trackID == "" {
		return nil, ErrNoTrackIDFound
	}
	zap.S().Debugf("track ID: %s", trackID)

	videoID := serverResponse.Data.Response.Video.ID

	postData := BuildPlayerPostData(videos, audios)
	zap.S().Debugf("building player post data: %s", postData)

	reqURL := fmt.Sprintf(playerBase, videoID, trackID)
	zap.S().Debugf("requesting player data from: %s", reqURL)

	headers := map[string]string{
		"X-Access-Right-Key": accessKey,
		"X-Frontend-Id":      "6",
		"X-Frontend-Version": "0",
		"X-Request-With":     "https://www.nicovideo.jp",
	}

	resp, err := util.FetchPage(
		client,
		http.MethodPost,
		reqURL,
		strings.NewReader(postData),
		headers,
		cookies,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch player data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("bad response: %s", resp.Status)
	}

	// debugging
	logger.WriteFile("nv_player_response", resp)

	domandID := util.GetCookieByName(resp.Cookies(), "domand_bid")
	if domandID == nil {
		return nil, ErrNoDomandDataFound
	}
	zap.S().Debugf("domand ID: %s", domandID.Value)
	cookies = append(cookies, domandID)

	var playerResponse *PlayerResponse
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	err = decoder.Decode(&playerResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode player response: %w", err)
	}
	formats, err := parser.ParseM3U8FromURL(
		playerResponse.Data.ContentURL,
		cookies,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse M3U8: %w", err)
	}

	return formats, nil
}

func BuildPlayerPostData(videos []*Videos, audios []*Audios) string {
	requestData := map[string]any{
		"outputs": CartesianFormats(videos, audios),
	}
	jsonData, _ := sonic.ConfigDefault.Marshal(requestData)
	return string(jsonData)
}

func CartesianFormats(videos []*Videos, audios []*Audios) [][]string {
	result := make([][]string, 0, len(videos)*len(audios))
	for _, video := range videos {
		if !video.IsAvailable {
			continue
		}
		for _, audio := range audios {
			result = append(result, []string{video.ID, audio.ID})
		}
	}
	return result
}
