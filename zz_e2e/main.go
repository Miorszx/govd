package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/govdbot/govd/internal/config"
	fb "github.com/govdbot/govd/internal/extractors/facebook"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

func main() {
	config.Load()
	logger.Init()

	jar, err := loadCookies("private/cookies/facebook.txt")
	if err != nil {
		fmt.Println("cookie err:", err)
		os.Exit(1)
	}

	ctx := &models.ExtractorContext{
		ContentURL: "https://www.facebook.com/reel/1434560375447743/",
		ContentID:  "1434560375447743",
		Extractor:  &models.Extractor{ID: "facebook"},
		HTTPClient: &networking.HTTPClient{Client: &http.Client{}, Cookies: jar},
		Context:    context.Background(),
	}

	media, err := fb.GetMedia(ctx)
	if err != nil {
		fmt.Println("GetMedia err:", err)
		os.Exit(1)
	}
	fmt.Printf("CAPTION: %q\n", media.Caption)
	for i, item := range media.Items {
		fmt.Printf("item[%d] formats=%d\n", i, len(item.Formats))
		for j, f := range item.Formats {
			fmt.Printf("  fmt[%d] id=%q type=%v vcodec=%q acodec=%q %dx%d size=%d dur=%d plugins=%d url=%s\n",
				j, f.FormatID, f.Type, f.VideoCodec, f.AudioCodec, f.Width, f.Height, f.FileSize, f.Duration, len(f.Plugins), f.URL[0][:min(60, len(f.URL[0]))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
