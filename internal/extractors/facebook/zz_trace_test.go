package facebook

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func newCtx(id string) *models.ExtractorContext {
	return &models.ExtractorContext{
		ContentID:    id,
		ContentURL:   "https://www.facebook.com/reel/" + id + "/",
		Context:      context.Background(),
		Config:       config.GetExtractorConfig("facebook"),
		HTTPClient:   networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: util.GetExtractorCookies("facebook")}),
		FilesTracker: models.NewFilesTracker(),
	}
}

func TestDumpWatch(t *testing.T) {
	logger.Init()
	config.Load()
	id := "1778660466707947"
	ctx := newCtx(id)
	// exact headers the extractor uses for the progressive path
	resp, err := ctx.Fetch(http.MethodGet, "https://www.facebook.com/watch/?v="+id, &networking.RequestParams{
		Headers: map[string]string{
			"User-Agent":      webHeaders["User-Agent"],
			"Accept":          webHeaders["Accept"],
			"Accept-Language": webHeaders["Accept-Language"],
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 1<<20)
	tmp := make([]byte, 32*1024)
	for {
		n, e := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if e != nil {
			break
		}
	}
	os.WriteFile("/tmp/watch_dump.html", buf, 0644)
	fmt.Printf("status=%d bytes=%d\n", resp.StatusCode, len(buf))
	// How many times does the video id appear?
	fmt.Printf("id occurrences: %d\n", countOccur(buf, []byte(id)))
	// Does the body contain a caption anchored near the id?
	fmt.Printf("anchored savable: %q\n", findCaptionAnchoredToID(buf, id))
	fmt.Printf("pure caption anchored: %q\n", findPureCaptionAnchored(buf, id))
	fmt.Printf("bodybytes caption: %q\n", tryExtractCaptionFromBodyBytes(buf, id))
}
