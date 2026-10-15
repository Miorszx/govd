package facebook

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

// Dumps the exact body parseProgressiveURLsAndCaptionFromBody sees, so we can
// find where the random caption regex actually matches.
func TestDumpReelBody(t *testing.T) {
	logger.Init()
	config.Load()
	id := "1778660466707947"
	ctx := &models.ExtractorContext{
		ContentID:   id,
		ContentURL:  "https://www.facebook.com/watch/?v=" + id,
		Context:     context.Background(),
		Config:      config.GetExtractorConfig("facebook"),
		HTTPClient:  networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: util.GetExtractorCookies("facebook")}),
		FilesTracker: models.NewFilesTracker(),
	}
	resp, err := ctx.Fetch("GET", "https://www.facebook.com/watch/?v="+id, &networking.RequestParams{
		Headers: webHeaders,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0)
	tmp := make([]byte, 4096)
	for {
		n, e := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if e != nil {
			break
		}
	}
	os.WriteFile("/tmp/reel_body_dump.html", buf, 0644)
	fmt.Println("dumped bytes:", len(buf), "status:", resp.StatusCode)
	cap := tryExtractCaptionFromBodyBytes(buf, id)
	fmt.Printf("caption from body = %q\n", cap)
}
