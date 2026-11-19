package tiktok

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/util"
)

func TestParseUniversalDataSupportsWebappVideoDetail(t *testing.T) {
	body := []byte(`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{"__DEFAULT_SCOPE__":{"webapp.video-detail":{"statusCode":0,"itemInfo":{"itemStruct":{"id":"123","desc":"caption","video":{"duration":12,"height":1280,"width":720,"PlayAddrStruct":{"Uri":"play","UrlList":["https://example.com/video.mp4"],"Width":720,"Height":1280}}}}}}}</script>`)

	item, status, err := ParseUniversalData(body)
	if err != nil {
		t.Fatalf("ParseUniversalData returned error: %v", err)
	}
	if status != 0 {
		t.Fatalf("expected status 0, got %d", status)
	}
	if item == nil || item.ID != "123" {
		t.Fatalf("expected parsed item 123, got %#v", item)
	}
	if item.Video == nil || item.Video.PlayAddr == nil || len(item.Video.PlayAddr.URLList) != 1 {
		t.Fatalf("expected parsed video play address, got %#v", item.Video)
	}
}

func TestGetVideoWebStatusErrorMapsCrossBorderViolation(t *testing.T) {
	// 10231 = "cross_border_violation": a region-locked (but existing) post
	// must be reported as geo-restricted, not as generically unavailable.
	if err := getVideoWebStatusError(10231); err != util.ErrGeoRestrictedContent {
		t.Fatalf("10231: expected ErrGeoRestrictedContent, got %v", err)
	}
	if err := getVideoWebStatusError(0); err != nil {
		t.Fatalf("0: expected nil, got %v", err)
	}
	if err := getVideoWebStatusError(10204); err != util.ErrTikTokIPBlocked {
		t.Fatalf("10204: expected ErrTikTokIPBlocked, got %v", err)
	}
	if err := getVideoWebStatusError(99999); err != util.ErrUnavailable {
		t.Fatalf("99999: expected ErrUnavailable, got %v", err)
	}
}

func TestSolveChallengeCookies(t *testing.T) {
	baseValue := []byte("seed")
	solution := "7"
	payload := append(append([]byte{}, baseValue...), []byte(solution)...)
	expectedDigest := sha256.Sum256(payload)

	challengePayload := `{"v":{"a":"` + base64.StdEncoding.EncodeToString(baseValue) + `","b":1777080052,"c":"` + base64.StdEncoding.EncodeToString(expectedDigest[:]) + `"},"s":"state"}`
	challengeEncoded := strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(challengePayload)), "=")

	body := []byte(`<body>Please wait...<p id="wci" class="_wafchallengeid"></p><p id="cs" class="` + challengeEncoded + `"></p><p id="rci" class="waforiginalreid"></p><p id="rs" class="ref-value"></p></body>`)

	cookies, err := solveChallengeCookies(body)
	if err != nil {
		t.Fatalf("solveChallengeCookies returned error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	var challengeValue string
	for _, cookie := range cookies {
		switch cookie.Name {
		case "_wafchallengeid":
			challengeValue = cookie.Value
		case "waforiginalreid":
			if cookie.Value != "ref-value" {
				t.Fatalf("expected referer cookie value ref-value, got %q", cookie.Value)
			}
		}
	}
	if challengeValue == "" {
		t.Fatal("expected _wafchallengeid cookie to be present")
	}

	decodedCookie, err := base64.StdEncoding.DecodeString(challengeValue)
	if err != nil {
		t.Fatalf("failed to decode challenge cookie value: %v", err)
	}

	var cookiePayload map[string]any
	if err := json.Unmarshal(decodedCookie, &cookiePayload); err != nil {
		t.Fatalf("failed to unmarshal challenge cookie payload: %v", err)
	}
	if cookiePayload["d"] != base64.StdEncoding.EncodeToString([]byte(solution)) {
		t.Fatalf("expected solved challenge payload, got %#v", cookiePayload["d"])
	}
}

func TestParseUniversalDataEnumeratesBitrateLadder(t *testing.T) {
	// PlayAddrStruct carries the default gear (540p here). The 1080p rendition
	// exists only inside bitrateInfo, so a parser that ignores bitrateInfo caps
	// every download at 540p regardless of what the upload actually supports.
	body := []byte(`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{"__DEFAULT_SCOPE__":{"webapp.video-detail":{"statusCode":0,"itemInfo":{"itemStruct":{"id":"42","desc":"d","video":{"duration":9,"height":960,"width":540,"PlayAddrStruct":{"Uri":"sd","UrlList":["https://cdn/sd.mp4"],"Width":540,"Height":960,"DataSize":"1234"},"bitrateInfo":[{"Bitrate":500000,"GearName":"normal_540","QualityType":1,"PlayAddr":{"Uri":"sd","UrlList":["https://cdn/sd.mp4"],"Width":540,"Height":960,"DataSize":"1234"}},{"Bitrate":2400000,"GearName":"normal_1080","QualityType":1,"PlayAddr":{"Uri":"hd","UrlList":["https://cdn/hd.mp4"],"Width":1080,"Height":1920,"DataSize":"5678"}}]}}}}}}</script>`)

	item, status, err := ParseUniversalData(body)
	if err != nil || status != 0 {
		t.Fatalf("parse failed: status=%d err=%v", status, err)
	}
	if got := len(item.Video.BitrateInfo); got != 2 {
		t.Fatalf("expected 2 ladder rungs, got %d", got)
	}
	if item.Video.BitrateInfo[1].PlayAddr.SizeBytes() != 5678 {
		t.Fatalf("expected DataSize 5678 parsed, got %d",
			item.Video.BitrateInfo[1].PlayAddr.SizeBytes())
	}
	if item.Video.BitrateInfo[1].PlayAddr.Width != 1080 {
		t.Fatalf("expected 1080 width on top rung, got %d",
			item.Video.BitrateInfo[1].PlayAddr.Width)
	}
}

func TestSizeBytesToleratesJunk(t *testing.T) {
	cases := map[string]int64{"": 0, "   ": 0, "abc": 0, " 4096 ": 4096}
	for in, want := range cases {
		got := (&WebPlayAddr{DataSize: in}).SizeBytes()
		if got != want {
			t.Fatalf("DataSize %q: expected %d, got %d", in, want, got)
		}
	}
	if got := (*WebPlayAddr)(nil).SizeBytes(); got != 0 {
		t.Fatalf("nil receiver: expected 0, got %d", got)
	}
}
