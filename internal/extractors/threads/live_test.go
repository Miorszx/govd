package threads

import (
	"fmt"
	"os"
	"testing"

	"github.com/govdbot/govd/internal/models"
)

// TestRealLivePost runs the real parser against a body fetched from the live
// site, saved to /tmp/t_post2_body.html. Not part of the normal suite.
func TestRealLivePost(t *testing.T) {
	body, err := os.ReadFile("/tmp/t_post2_body.html")
	if err != nil {
		t.Skipf("no live body available: %v", err)
	}
	code := "DeYWKe5D2hG"
	ctx := &models.ExtractorContext{
		ContentID:  code,
		ContentURL: "https://www.threads.com/@harmcheon/post/" + code,
		Extractor:  &models.Extractor{ID: "threads"},
	}
	media, err := ParsePostMedia(ctx, body)
	if err != nil {
		t.Fatalf("ParsePostMedia ERR: %v", err)
	}
	fmt.Printf("LIVE RESULT -> caption=%q items=%d\n", media.Caption, len(media.Items))
	for i, it := range media.Items {
		for _, f := range it.Formats {
			u := ""
			if len(f.URL) > 0 {
				u = f.URL[0]
			}
			if len(u) > 90 {
				u = u[:90]
			}
			fmt.Printf("  item %d fmt=%s url=%s\n", i, f.FormatID, u)
		}
	}
}
