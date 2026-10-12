package main

// Runs the REAL threads extractor GetFunc (share resolve -> page fetch with the
// bot's cookie jar -> parse) from the host, exactly as the bot does, so we can
// see the failure the bot hits.
import (
	"context"
	"fmt"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/extractors/threads"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func main() {
	logger.Init()
	config.Load()
	logger.SetLevel(config.Env.LogLevel)

	share := "BATCM-ya8F"
	url := "https://www.threads.com/share/" + share + "/"
	re := threads.Extractor.URLPattern
	groups := util.GetNamedGroups(re, url)
	fmt.Printf("GROUPS: %+v\n", groups)

	ctx := &models.ExtractorContext{
		ContentID:   groups["id"],
		ContentURL:  url,
		MatchGroups: groups,
		Extractor:   threads.Extractor,
		Context:     context.Background(),
		Config:      config.GetExtractorConfig("threads"),
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
			Cookies: util.GetExtractorCookies("threads"),
		}),
	}

	resp, err := threads.Extractor.GetFunc(ctx)
	if err != nil {
		fmt.Println("GETFUNC ERR:", err)
		return
	}
	if resp == nil || resp.Media == nil {
		fmt.Println("NIL MEDIA")
		return
	}
	fmt.Printf("OK contentID=%s caption=%q items=%d\n", resp.Media.ContentID, resp.Media.Caption, len(resp.Media.Items))
	for i, it := range resp.Media.Items {
		for _, f := range it.Formats {
			u := ""
			if len(f.URL) > 0 {
				u = f.URL[0]
			}
			if len(u) > 80 {
				u = u[:80]
			}
			fmt.Printf("  item %d type=%s url=%s\n", i, f.Type, u)
		}
	}
}
