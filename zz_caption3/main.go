package main

import (
	"context"
	"fmt"

	"github.com/govdbot/govd/internal/config"
	fb "github.com/govdbot/govd/internal/extractors/facebook"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func main() {
	config.Load()
	cookies := util.GetExtractorCookies("facebook")
	for _, id := range []string{"2792910907770618", "1432737025036561", "2182203379338397"} {
		ctx := &models.ExtractorContext{
			ContentURL: "https://www.facebook.com/reel/" + id,
			ContentID:  id,
			Context:    context.Background(),
			HTTPClient: &networking.HTTPClient{Cookies: cookies},
			Extractor:  &models.Extractor{ID: "facebook"},
		}
		m, err := fb.GetMedia(ctx)
		if err != nil {
			fmt.Println(id, "err:", err)
			continue
		}
		fmt.Printf("%s caption=%q\n", id, m.Caption)
	}
}
