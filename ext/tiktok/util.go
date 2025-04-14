package tiktok

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"govd/enums"
	"govd/models"
	"govd/util"

	"github.com/google/uuid"
)

func BuildAPIQuery() (url.Values, error) {
	requestTicket := strconv.Itoa(int(time.Now().Unix()) * 1000)
	clientDeviceID := uuid.New().String()
	versionCode, err := GetAppVersionCode(appVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get app version code: %w", err)
	}
	return url.Values{
		"device_platform":       []string{"android"},
		"os":                    []string{"android"},
		"ssmix":                 []string{"0"}, // what is this?
		"_rticket":              []string{requestTicket},
		"cdid":                  []string{clientDeviceID},
		"channel":               []string{"googleplay"},
		"aid":                   []string{appID},
		"app_name":              []string{appName},
		"version_code":          []string{versionCode},
		"version_name":          []string{appVersion},
		"manifest_version_code": []string{manifestAppVersion},
		"update_version_code":   []string{manifestAppVersion},
		"ab_version":            []string{appVersion},
		"resolution":            []string{"1080*2400"},
		"dpi":                   []string{"420"},
		"device_type":           []string{"Pixel 7"},
		"device_brand":          []string{"Google"},
		"language":              []string{"en"},
		"os_api":                []string{"29"},
		"os_version":            []string{"13"},
		"ac":                    []string{"wifi"},
		"is_pad":                []string{"0"},
		"current_region":        []string{"US"},
		"app_type":              []string{"normal"},
		"last_install_time":     []string{GetRandomInstallTime()},
		"timezone_name":         []string{"America/New_York"},
		"residence":             []string{"US"},
		"app_language":          []string{"en"},
		"timezone_offset":       []string{"-14400"},
		"host_abi":              []string{"armeabi-v7a"},
		"locale":                []string{"en"},
		"ac2":                   []string{"wifi5g"},
		"uoo":                   []string{"1"}, // what is this?
		"carrier_region":        []string{"US"},
		"build_number":          []string{appVersion},
		"region":                []string{"US"},
		"ts":                    []string{strconv.Itoa(int(time.Now().Unix()))},
		"iid":                   []string{installationID},
		"device_id":             []string{GetRandomDeviceID()},
		"openudid":              []string{GetRandomUdid()},
	}, nil
}

func ParsePlayAddr(
	video *Video,
	playAddr *PlayAddr,
) (*models.MediaFormat, error) {
	formatID := playAddr.URLKey
	if formatID == "" {
		return nil, errors.New("url_key not found")
	}
	videoCodec := enums.MediaCodecHEVC
	if strings.Contains(formatID, "h264") {
		videoCodec = enums.MediaCodecAVC
	}
	videoURL := playAddr.URLList
	videoDuration := video.Duration / 1000
	videoWidth := playAddr.Width
	videoHeight := playAddr.Height
	videoCover := &video.Cover
	videoThumbnailURLs := videoCover.URLList

	return &models.MediaFormat{
		Type:       enums.MediaTypeVideo,
		FormatID:   formatID,
		URL:        videoURL,
		VideoCodec: videoCodec,
		AudioCodec: enums.MediaCodecAAC,
		Duration:   videoDuration,
		Thumbnail:  videoThumbnailURLs,
		Width:      videoWidth,
		Height:     videoHeight,
	}, nil
}

func GetRandomInstallTime() string {
	currentTime := int(time.Now().Unix())
	minOffset := big.NewInt(86400)
	maxOffset := big.NewInt(1123200)
	diff := new(big.Int).Sub(maxOffset, minOffset)
	randomOffset, _ := rand.Int(rand.Reader, diff)
	randomOffset.Add(randomOffset, minOffset)
	result := currentTime - int(randomOffset.Int64())
	return strconv.Itoa(result)
}

func GetRandomUdid() string {
	const charset = "0123456789abcdef"
	result := make([]byte, 16)

	for i := range result {
		index, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[index.Int64()]
	}
	return string(result)
}

func GetRandomDeviceID() string {
	minNum := big.NewInt(7250000000000000000)
	maxNum := big.NewInt(7351147085025500000)
	diff := new(big.Int).Sub(maxNum, minNum)
	randNum, _ := rand.Int(rand.Reader, diff)
	result := new(big.Int).Add(randNum, minNum)
	return result.String()
}

func BuildPostData(awemeID string) *strings.Reader {
	data := url.Values{
		"aweme_ids":      []string{fmt.Sprintf("[%s]", awemeID)},
		"request_source": []string{"0"},
	}
	return strings.NewReader(data.Encode())

}

func GetAppVersionCode(version string) (string, error) {
	parts := strings.Split(version, ".")

	var result strings.Builder
	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil {
			return "", fmt.Errorf("failed to parse version part: %w", err)
		}
		_, err = fmt.Fprintf(&result, "%02d", num)
		if err != nil {
			return "", fmt.Errorf("failed to format version part: %w", err)
		}
	}
	return result.String(), nil
}

func FindVideoData(
	resp *Response,
	expectedAwemeID string,
) (*AwemeDetails, error) {
	if resp.StatusCode == 2053 {
		return nil, util.ErrUnavailable
	}
	if resp.AwemeDetails == nil {
		return nil, errors.New("aweme_details is nil")
	}
	for _, item := range resp.AwemeDetails {
		if item.AwemeID == expectedAwemeID {
			return &item, nil
		}
	}
	return nil, errors.New("matching aweme_id not found")
}
