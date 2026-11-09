package main

import (
	"context"
	"fmt"

	"github.com/govdbot/govd/internal/config"
	fb "github.com/govdbot/govd/internal/extractors/facebook"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func main() {
	config.Load()
	logger.Init()
	client := networking.NewHTTPClient(&networking.NewHTTPClientOptions{
		Cookies:     util.GetExtractorCookies("facebook"),
		Impersonate: true,
	})
	ext := fb.Extractor

	ctx := &models.ExtractorContext{
		ContentID:  "2792910907770618",
		ContentURL: "https://www.facebook.com/reel/2792910907770618",
		Extractor:  ext,
		HTTPClient: client,
		Context:    context.Background(),
	}
	res, err := ext.GetFunc(ctx)
	if err != nil {
		fmt.Printf("ERR: %v\n", err)
		return
	}
	if res == nil || res.Media == nil {
		fmt.Println("NIL media")
		return
	}
	m := res.Media
	fmt.Printf("caption_len=%d\n", len(m.Caption))
	fmt.Printf("caption=%q\n", m.Caption)
}
