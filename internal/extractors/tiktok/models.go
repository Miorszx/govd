package tiktok

import (
	"strconv"
	"strings"
)

type Response struct {
	AwemeDetails []*AwemeDetail `json:"aweme_details"`
	StatusCode   int            `json:"status_code"`
	StatusMsg    string         `json:"status_msg"`
}

type PlayAddr struct {
	DataSize int64    `json:"data_size"`
	FileCs   string   `json:"file_cs"`
	FileHash string   `json:"file_hash"`
	Height   int64    `json:"height"`
	URI      string   `json:"uri"`
	URLKey   string   `json:"url_key"`
	URLList  []string `json:"url_list"`
	Width    int64    `json:"width"`
}

type Image struct {
	DisplayImage *DisplayImage `json:"display_image"`
}

type DisplayImage struct {
	Height  int32    `json:"height"`
	URI     string   `json:"uri"`
	URLList []string `json:"url_list"`
	Width   int32    `json:"width"`
}

type ImagePostInfo struct {
	Images []*Image `json:"images"`
}

type Video struct {
	Duration        int32     `json:"duration"`
	HasWatermark    bool      `json:"has_watermark"`
	Height          int32     `json:"height"`
	PlayAddr        *PlayAddr `json:"play_addr"`
	PlayAddrBytevc1 *PlayAddr `json:"play_addr_bytevc1"`
	PlayAddrH264    *PlayAddr `json:"play_addr_h264"`
	Width           int32     `json:"width"`
}

type AwemeDetail struct {
	AwemeID       string         `json:"aweme_id"`
	AwemeType     int            `json:"aweme_type"`
	Desc          string         `json:"desc"`
	Video         *Video         `json:"video"`
	ImagePostInfo *ImagePostInfo `json:"image_post_info"`
}

type WebItemStruct struct {
	ID        string        `json:"id"`
	Desc      string        `json:"desc"`
	Video     *WebVideo     `json:"video"`
	ImagePost *WebImagePost `json:"imagePost"`
}

type WebImagePost struct {
	Images []*WebImage `json:"images"`
	Title  string      `json:"title"`
}

type WebVideo struct {
	Duration    int32          `json:"duration"`
	Height      int32          `json:"height"`
	PlayAddr    *WebPlayAddr   `json:"PlayAddrStruct"`
	BitrateInfo []*BitrateInfo `json:"bitrateInfo"`
	Width       int32          `json:"width"`
}

// BitrateInfo is one rung of the quality ladder TikTok ships alongside the
// page. PlayAddrStruct only ever carries the default gear (usually 540p/720p),
// so the HD/1080p renditions are reachable exclusively through this array.
type BitrateInfo struct {
	Bitrate     int64        `json:"Bitrate"`
	GearName    string       `json:"GearName"`
	QualityType int          `json:"QualityType"`
	PlayAddr    *WebPlayAddr `json:"PlayAddr"`
}

type WebImageURL struct {
	URLList []string `json:"urlList"`
}

type WebImage struct {
	URL *WebImageURL `json:"imageURL"`
}

type WebPlayAddr struct {
	FileHash string   `json:"FileHash"`
	FileCs   string   `json:"FileCs"`
	DataSize string   `json:"DataSize"`
	Width    int32    `json:"Width"`
	Height   int32    `json:"Height"`
	URI      string   `json:"Uri"`
	URLList  []string `json:"UrlList"`
	URLKey   string   `json:"UrlKey"`
}

// SizeBytes parses DataSize, which TikTok ships as a decimal string on the web
// payload but as a plain integer on the app API. Returns 0 when unparseable so
// callers can treat it as "unknown" rather than as a zero-byte file.
func (p *WebPlayAddr) SizeBytes() int64 {
	if p == nil || p.DataSize == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(p.DataSize), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
