package facebook

import (
	"os"
	"strings"
	"testing"
)

// mustReadFile reads a sibling source file. These tests guard shape, not
// behaviour, so reading the source is the honest check: the thing that breaks
// is somebody trimming a header or moving a guard, not a data structure.
func mustReadFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// The Accept header is the entire reason restricted posts used to come back
// empty. Facebook picks the response shape from it:
//
//	without image/avif,image/webp -> 210 KB, progressive_url count 0
//	with                          -> 600 KB, progressive_url count 4
//
// Measured 2026-11-24 against reel 3093314621017769 with a live cookie jar,
// same URL, same cookies, same UA. If anyone trims this header back to the
// crawler-shaped one, every private post silently stops resolving again, and
// the symptom is "content isn't available" rather than an error.
func TestCurlAcceptHeaderCarriesImageTypes(t *testing.T) {
	src := mustReadFile(t, "curlfetch.go")
	for _, need := range []string{"image/avif", "image/webp"} {
		if !strings.Contains(src, need) {
			t.Fatalf("curlfetch.go Accept header lost %q; Facebook then serves "+
				"the crawler shell with no progressive_url", need)
		}
	}
	if strings.Contains(src, `"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"`) {
		t.Fatal("curlfetch.go is back on the media-less Accept header")
	}
}

// sharePVideoIDs must find every attachment, in order, once.
func TestSharePVideoIDs(t *testing.T) {
	body := []byte(`{"a":{"video_id":"3093314621017769","x":1},` +
		`"b":{"video_id":"1767102851051174"},` +
		`"c":{"video_id":"3093314621017769"}}`)
	got := sharePVideoIDs(body)
	if len(got) != 2 {
		t.Fatalf("want 2 distinct ids, got %d: %v", len(got), got)
	}
	if got[0] != "3093314621017769" || got[1] != "1767102851051174" {
		t.Fatalf("order not preserved: %v", got)
	}
}

// A real photo album has no video_id; the dispatcher must fall through to the
// existing photo path rather than treating the post as empty.
func TestSharePVideoIDsEmptyOnPhotoAlbum(t *testing.T) {
	body := []byte(`{"node":{"__typename":"Photo","id":"123"}}`)
	if ids := sharePVideoIDs(body); len(ids) != 0 {
		t.Fatalf("photo album should yield no ids, got %v", ids)
	}
}

// The share code /p/ must be probed for video before the photo path runs, and
// the probe must live in GetMedia where a *Media can actually be returned.
func TestSharePProbeWiredInGetMedia(t *testing.T) {
	src := mustReadFile(t, "main.go")
	i := strings.Index(src, "func GetMedia(")
	if i < 0 {
		t.Fatal("GetMedia not found")
	}
	body := src[i:]
	end := strings.Index(body, "\nfunc ")
	if end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "getSharePMedia(ctx)") {
		t.Fatal("GetMedia does not probe share/p for video")
	}
	if !strings.Contains(body, `"/share/p/"`) {
		t.Fatal("GetMedia lost the share/p guard")
	}
	// The guard must run before GetVideoData: the photo branch would answer
	// "content isn't available" and never come back.
	if strings.Index(body, "getSharePMedia") > strings.Index(body, "GetVideoData") {
		t.Fatal("share/p probe runs after the photo path, so it is dead code")
	}
}
