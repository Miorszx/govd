//go:build ignore

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

func main() {
	logger.Init()
	ck := util.GetExtractorCookies("facebook")
	ctx := &models.ExtractorContext{
		Context:    context.Background(),
		ContentURL: "https://www.facebook.com/share/p/1A5dDnp3j5/",
		ContentID:  "1A5dDnp3j5",
		Extractor:  facebook.Extractor,
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: ck}),
	}
	m, err := facebook.GetMedia(ctx)
	if err != nil {
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	for i, it := range m.Items {
		for _, f := range it.Formats {
			fmt.Printf("%d|%s|%s|%s\n", i, f.FormatID, f.VideoCodec, f.URL[0])
		}
	}
}
