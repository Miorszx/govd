package main

// Runs the REAL facebook extractor GetFunc (share resolve -> page fetch with
// the bot's cookie jar -> parse) from the host, exactly as the bot does, so we
// can see what caption/media the bot actually produces for a given link.
import (
	"context"
	"fmt"
	"os"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/extractors/facebook"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func run(label string, extractor *models.Extractor, url string) {
	groups := util.GetNamedGroups(extractor.URLPattern, url)
	fmt.Printf("\n=== %s ===\nurl=%s\ngroups=%+v\n", label, url, groups)

	ctx := &models.ExtractorContext{
		ContentID:   groups["id"],
		ContentURL:  url,
		MatchGroups: groups,
		Extractor:   extractor,
		Context:     context.Background(),
		Config:      config.GetExtractorConfig("facebook"),
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
			Cookies: util.GetExtractorCookies("facebook"),
		}),
		FilesTracker: models.NewFilesTracker(),
	}

	resp, err := extractor.GetFunc(ctx)
	if err != nil {
		fmt.Println("GETFUNC ERR:", err)
		return
	}
	if resp == nil || resp.Media == nil {
		fmt.Println("NIL MEDIA")
		return
	}
	fmt.Printf("OK contentID=%s\ncaption=%q\nitems=%d\n",
		resp.Media.ContentID, resp.Media.Caption, len(resp.Media.Items))
	for i, it := range resp.Media.Items {
		for _, f := range it.Formats {
			u := ""
			if len(f.URL) > 0 {
				u = f.URL[0]
			}
			fmt.Printf("  item[%d] fmt=%s type=%v size=%d url=%.100s\n", i, f.FormatID, f.Type, f.FileSize, u)
		}
	}
}

func main() {
	logger.Init()
	config.Load()
	logger.SetLevel(config.Env.LogLevel)

	link := "https://www.facebook.com/share/v/1RqkYmnKVL/"
	if len(os.Args) > 1 {
		link = os.Args[1]
	}
	// share/ links match the ShareExtractor, which only resolves the share
	// redirect and returns a URL; the dispatcher then re-runs the main
	// Extractor on that final URL. Mirror that two-step flow here.
	if facebook.ShareExtractor.URLPattern.MatchString(link) {
		groups := util.GetNamedGroups(facebook.ShareExtractor.URLPattern, link)
		ctx := &models.ExtractorContext{
			ContentID:   groups["id"],
			ContentURL:  link,
			MatchGroups: groups,
			Extractor:   facebook.ShareExtractor,
			Context:     context.Background(),
			Config:      config.GetExtractorConfig("facebook"),
			HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
				Cookies: util.GetExtractorCookies("facebook"),
			}),
			FilesTracker: models.NewFilesTracker(),
		}
		resp, err := facebook.ShareExtractor.GetFunc(ctx)
		if err != nil {
			fmt.Println("SHARE RESOLVE ERR:", err)
			return
		}
		if resp == nil || resp.URL == "" {
			fmt.Println("SHARE NIL / EMPTY URL")
			return
		}
		fmt.Printf("share resolved -> %s\n", resp.URL)
		link = resp.URL
	}
	run("facebook", facebook.Extractor, link)
}
