//go:build ignore

// Live check for the share/p video patch. Run from the repo root:
//
//	go run zz_e2e/sharep_live.go
//
// It drives the real extractor against the post this patch was written for and
// prints what each item resolved to, so the answer is measured rather than
// assumed.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/govdbot/govd/internal/extractors/facebook"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

const target = "https://www.facebook.com/share/p/1A5dDnp3j5/"

func main() {
	logger.Init()
	cookies := util.GetExtractorCookies("facebook")
	fmt.Printf("cookies: %d\n", len(cookies))
	if len(cookies) == 0 {
		fmt.Println("no cookie jar; the post is restricted and will not resolve")
		os.Exit(1)
	}
	ctx := &models.ExtractorContext{
		Context:    context.Background(),
		ContentURL: target,
		ContentID:  "1A5dDnp3j5",
		Extractor:  facebook.Extractor,
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
			Cookies: cookies,
		}),
	}
	media, err := facebook.GetMedia(ctx)
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("caption: %.80s\n", media.Caption)
	fmt.Printf("items:   %d\n", len(media.Items))
	for i, it := range media.Items {
		for _, f := range it.Formats {
			if f.FormatID == "dash" {
				fmt.Printf("  [%d] dash %dx%d %dB %s\n", i, f.Width,
					f.Height, f.FileSize, truncate(f.URL[0], 90))
			} else {
				fmt.Printf("  [%d] %-12s %s\n", i, f.FormatID,
					truncate(f.URL[0], 90))
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
