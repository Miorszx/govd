package facebook

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

// Shows exactly which caption the GraphQL path returns and whether the videoID
// is near it (anchored) or it came from the random recommendation feed.
func TestDumpReelCaption(t *testing.T) {
	logger.Init()
	config.Load()
	id := "1778660466707947"
	ctx := &models.ExtractorContext{
		ContentID:    id,
		ContentURL:   "https://www.facebook.com/reel/" + id + "/",
		Context:      context.Background(),
		Config:       config.GetExtractorConfig("facebook"),
		HTTPClient:   networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: util.GetExtractorCookies("facebook")}),
		FilesTracker: models.NewFilesTracker(),
	}
	body := ""
	for i := 0; i < 3; i++ {
		resp, err := ctx.Fetch("GET", "https://www.facebook.com/reel/"+id+"/", &networking.RequestParams{Headers: webHeaders})
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 0)
		tmp := make([]byte, 4096)
		for {
			n, e := resp.Body.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if e != nil {
				break
			}
		}
		resp.Body.Close()
		body = string(buf)
		os.WriteFile(fmt.Sprintf("/tmp/reel_page_%d.html", i), buf, 0644)

		// anchored caption (the only legit path)
		anch := findCaptionAnchoredToID(buf, id)
		// what tryFetchReelCaptionViaGraphQL returns
		capv := tryFetchReelCaptionViaGraphQL(ctx, id)

		cnt := regexp.MustCompile(`"message":\s*\{\s*"text":\s*"`).FindAllStringIndex(body, -1)
		fmt.Printf("run %d: bytes=%d allMessageText=%d anchored=%q graphql=%q\n",
			i, len(buf), len(cnt), anch, capv)
	}
}
