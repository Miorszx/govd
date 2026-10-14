package facebook

import (
	"strings"
	"testing"
)

// resolvePostVideoSection must never invent a video for a page where the post
// ID is not present, and must not be consulted at all when the normal
// findVideoSection lookup already succeeds (that path is untouched - see the
// call site guard in parseVideoFromBody).
func TestResolvePostVideoSectionNoFalsePositive(t *testing.T) {
	// Body with a dash_mpd_debug anchor but NOTHING tying it to the queried
	// post ID: the resolver must decline rather than guess a feed video.
	body := []byte(`{"some":"feed","dash_mpd_debug.mpd?v=9999999999999999","next":"block"}`)
	sec, vid := resolvePostVideoSection(body, "1234567890123456")
	if sec != nil || vid != "" {
		t.Fatalf("resolver guessed a video for an unrelated post: sec=%v vid=%q", sec, vid)
	}
}

// An anchor that sits far away from the post ID (beyond the bounded window)
// must be rejected, so the caller keeps its previous behaviour.
func TestResolvePostVideoSectionRejectsDistantAnchor(t *testing.T) {
	filler := strings.Repeat("x", 40000)
	body := []byte("1234567890123456" + filler + `dash_mpd_debug.mpd?v=8888888888888888|"id":"8888888888888888"`)
	sec, _ := resolvePostVideoSection(body, "1234567890123456")
	if sec != nil {
		t.Fatalf("resolver accepted a distant anchor; want nil")
	}
}
