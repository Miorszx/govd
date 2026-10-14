package facebook

import (
	"os"
	"regexp"
	"testing"
)

// Regression test for group permalink posts where the numeric post ID differs
// from the numeric video ID. Such a page only anchors dash_mpd_debug.mpd?v= on
// the real video ID, so a naive full-body scan picks the first progressive URL
// from an unrelated feed video instead of the post's own video.
//
// The fixture is a saved Googlebot-+cookie body (path via FB_FIXTURE, default
// /tmp/t_googlebot.html). Post/video IDs are derived from the fixture so no
// real identifiers are hardcoded here.
func TestResolvePostVideoSection(t *testing.T) {
	path := os.Getenv("FB_FIXTURE")
	if path == "" {
		path = "/tmp/t_googlebot.html"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture not present: %v", err)
	}

	// Post ID is the numeric permalink segment in the body (slashes may be
	// JSON-escaped as \/).
	m := regexp.MustCompile(`(?:/|\\/)permalink(?:/|\\/)(\d{10,})`).FindSubmatch(raw)
	if len(m) < 2 {
		t.Skip("fixture has no permalink post id")
	}
	postID := string(m[1])

	sec, vid := resolvePostVideoSection(raw, postID)
	if sec == nil {
		t.Fatalf("resolver returned nil for post id %q", postID)
	}
	if vid == postID {
		t.Fatalf("resolver returned the post id as the video id (%q) - did not re-anchor", vid)
	}

	// The resolved section must carry the post's own progressive video.
	if m := sdURLPattern.FindSubmatch(sec); len(m) < 2 {
		t.Errorf("expected a progressive SD URL in the resolved section, got none")
	}
}
