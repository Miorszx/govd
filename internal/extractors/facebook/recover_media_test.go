package facebook

import (
	"strings"
	"testing"
)

// These tests pin the behaviour of recoverMediaFromBody, the ladder that was
// lifted out of parseVideoFromBody. They use synthetic page fragments built
// from the same markers the extractor keys on, so no real post, video or CDN
// identifier is committed here.
//
// The important contract is not just "it finds a URL": it is the ORDER of the
// ladder. Group videos must prefer progressive_urls over the feed m4 matches
// that 403, image posts must fall through to the image extractor instead of
// returning a feed video, and a page with nothing usable must report false so
// the caller keeps the decision.

const (
	testMp4SD    = `https://scontent.example/v/t1/m412/abc.mp4?oh=1`
	testMp4HD    = `https://scontent.example/v/t1/m367/def.mp4?oh=2`
	testProgHD   = `https://scontent.example/v/t1/prog_hd.mp4`
	testProgSD   = `https://scontent.example/v/t1/prog_sd.mp4`
	testImageURL = `https://scontent.example/v/t1/photo.jpg?oh=abc123&oe=x`
)

// progressiveFragment mirrors the field order the extractor's regexes expect:
// progressive_url, then failure_reason, then metadata.quality.
func progressiveFragment(url, quality string) string {
	return `"progressive_url":"` + url + `","failure_reason":null,"metadata":{"quality":"` + quality + `"}`
}

func TestRecoverMediaDirectMp4WhenNoSection(t *testing.T) {
	body := []byte(`{"video":"` + testMp4SD + `"}`)
	data := &VideoData{}

	got := recoverMediaFromBody(data, body, nil, "", false, false, "")

	if !got {
		t.Fatalf("expected media found, got false")
	}
	if data.SDURL == "" && data.HDURL == "" {
		t.Fatalf("expected a direct mp4 URL to be captured, got neither HD nor SD")
	}
}

func TestRecoverMediaGroupVideoPrefersProgressiveOverFeedMp4(t *testing.T) {
	// A group video permalink typically carries feed m4 blobs that answer 403
	// plus one valid progressive_url. The ladder must take the progressive one.
	body := []byte(
		`{"feed":"` + testMp4SD + `",` +
			progressiveFragment(testProgHD, "HD") + `,` +
			progressiveFragment(testProgSD, "SD") + `}`)
	data := &VideoData{}

	got := recoverMediaFromBody(data, body, nil, "123456789012", false, true, "")

	if !got {
		t.Fatalf("expected media found, got false")
	}
	if data.HDURL != testProgHD {
		t.Fatalf("expected HD progressive URL %q, got %q", testProgHD, data.HDURL)
	}
	if data.SDURL == testMp4SD {
		t.Fatalf("took the feed m4 URL %q instead of the progressive one", testMp4SD)
	}
}

func TestRecoverMediaImagePostDoesNotReturnFeedVideo(t *testing.T) {
	// An image post whose page still embeds feed m4 blobs must end up with
	// images and no video. This is the "post ada gambar, bukan video" case.
	body := []byte(`{"feed":"` + testMp4SD + `","img":"` + testImageURL + `"}`)
	data := &VideoData{}

	got := recoverMediaFromBody(data, body, nil, "123456789012", true, false, "")

	if data.HDURL != "" || data.SDURL != "" {
		t.Fatalf("image post was served a video URL: hd=%q sd=%q", data.HDURL, data.SDURL)
	}
	if len(data.ImageURLs) == 0 && data.ImageURL == "" {
		t.Fatalf("image post produced no image")
	}
	// The ladder deliberately reports false here: the caller continues to the
	// title/caption stage with the populated image data.
	if got {
		t.Fatalf("expected false so the caller keeps its own flow, got true")
	}
}

func TestRecoverMediaSectionPathFillsDataButReportsFalse(t *testing.T) {
	// On the section path the ladder writes HD/SD onto data and returns false.
	// That is the contract the caller relies on: false means "not finished,
	// continue to caption/title", not "nothing found". parseVideoFromBody
	// guards its "no video URLs" error with data.HDURL == "" && data.SDURL == "",
	// so as long as the URLs land on data the post survives.
	section := []byte(`{"dash_mpd_debug.mpd?v=123456789012"}`)
	body := []byte(progressiveFragment(testProgHD, "HD") + `,` + progressiveFragment(testProgSD, "SD"))
	data := &VideoData{}

	got := recoverMediaFromBody(data, body, section, "123456789012", false, true, "")

	if data.HDURL != testProgHD {
		t.Fatalf("expected HD %q, got %q", testProgHD, data.HDURL)
	}
	if got {
		t.Fatalf("section path is expected to return false so the caller continues to caption extraction, got true")
	}
}

func TestRecoverMediaNothingUsableReportsFalse(t *testing.T) {
	data := &VideoData{}

	got := recoverMediaFromBody(data, []byte(`{"nothing":"here"}`), nil, "", false, false, "")

	if got {
		t.Fatalf("expected false for a page with no media, got true")
	}
	if data.HDURL != "" || data.SDURL != "" || len(data.ImageURLs) != 0 {
		t.Fatalf("expected no media on data, got hd=%q sd=%q imgs=%d",
			data.HDURL, data.SDURL, len(data.ImageURLs))
	}
}

func TestRecoverMediaDoesNotMutateBodyInput(t *testing.T) {
	// The ladder rewrites \\/ to / while matching. It must do that on a copy:
	// the caller still uses the original body for caption parsing afterwards.
	original := `{"v":"https:\/\/scontent.example\/v\/t1\/m412\/abc.mp4"}`
	body := []byte(original)
	data := &VideoData{}

	recoverMediaFromBody(data, body, nil, "", false, false, "")

	if string(body) != original {
		t.Fatalf("body was mutated in place:\n want %s\n  got %s", original, string(body))
	}
}

func TestRecoverMediaEscapedSlashesStillMatch(t *testing.T) {
	// Pages commonly come back with JSON-escaped slashes.
	body := []byte(strings.ReplaceAll(`{"v":"`+testMp4SD+`"}`, "/", `\/`))
	data := &VideoData{}

	got := recoverMediaFromBody(data, body, nil, "", false, false, "")

	if !got {
		t.Fatalf("expected escaped-slash mp4 to be found, got false")
	}
	if data.SDURL == "" && data.HDURL == "" {
		t.Fatalf("no URL captured from escaped body")
	}
}
