package instagram

import "testing"

// TestOwnershipBothDirections pins the two ownership shapes Instagram ships.
//
// Shape A (desktop UA): `"code"` precedes the media block, so backward
// ownership works.
//
// Shape B (default mobile UA, measured on DfUDeEHzDQn): the item is flat and
// `"code"` comes AFTER `"video_versions"` in the same object. Backward-only
// ownership found no owner and the reel was served as an image. This test
// fails if that regression returns.
func TestOwnershipBothDirections(t *testing.T) {
	// Shape A: marker first
	backward := `{"items":[{"code":"AAA","video_versions":[{"width":10,"height":10,"url":"u"}],` +
		`"candidates":[{"width":5,"height":5,"url":"c"}]}]}`
	// Shape B: media first, marker after, same object
	forward := `{"items":[{"video_versions":[{"width":20,"height":20,"url":"u2"}],` +
		`"image_versions2":{"candidates":[{"width":6,"height":6,"url":"c2"}]},` +
		`"code":"BBB"}]}`

	cases := []struct {
		name string
		html string
		sc   string
		want int // expected video_versions blocks
	}{
		{"backward owner", backward, "AAA", 1},
		{"backward non-owner", backward, "ZZZ", 0},
		{"forward owner", forward, "BBB", 1},
		{"forward non-owner", forward, "ZZZ", 0},
	}

	for _, c := range cases {
		got := findOwnedBlocks(c.html, `"video_versions"`, c.sc)
		if len(got) != c.want {
			t.Errorf("%s: got %d blocks, want %d", c.name, len(got), c.want)
		}
	}
}

// TestForwardOwnershipDoesNotCrossObject is the guard that matters most:
// a marker belonging to the NEXT item must not claim a block from this one.
func TestForwardOwnershipDoesNotCrossObject(t *testing.T) {
	html := `{"items":[{"video_versions":[{"width":1,"height":1,"url":"a"}]},` +
		`{"code":"NEXT","video_versions":[{"width":2,"height":2,"url":"b"}]}]}`

	if got := findOwnedBlocks(html, `"video_versions"`, "NEXT"); len(got) != 1 {
		t.Fatalf("NEXT should own exactly 1 block, got %d", len(got))
	}
	if got := findOwnedBlocks(html, `"video_versions"`, "NONE"); len(got) != 0 {
		t.Fatalf("NONE should own 0 blocks, got %d -- forward scan crossed the object boundary", len(got))
	}
}
