package facebook

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func TestManifestMeta(t *testing.T) {
	ids := []string{
		"1084425931240979",
		"1002661609544760",
		"1723125672710581",
		"1148401390917826",
	}
	c := &models.ExtractorContext{
		Context: t.Context(),
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
			Cookies: util.ParseCookieFile("facebook.txt"),
		}),
	}
	for _, id := range ids {
		start := time.Now()
		m := fetchReelMedia(c, id)
		if m == nil || m.DASH == nil {
			t.Logf("%s  NO DASH", id)
			continue
		}
		br := float64(m.DASH.VideoSize) * 8 / float64(m.DASH.Duration)
		t.Logf("%s  %dx%d  src=%dB  dur=%ds  srcbitrate=%.2f Mbps  fetch=%s",
			id, m.DASH.Width, m.DASH.Height, m.DASH.VideoSize,
			m.DASH.Duration, br/1e6, time.Since(start).Round(time.Millisecond))
	}
	_ = exec.Command
	_ = os.Getenv
}
