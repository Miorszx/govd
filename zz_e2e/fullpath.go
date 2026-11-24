//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/govdbot/govd/internal/core"
	"github.com/govdbot/govd/internal/extractors"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/util"
)

func main() {
	logger.Init()
	url := "https://www.facebook.com/share/p/1A5dDnp3j5/"
	fmt.Println("STEP 1 FromURL")
	ec := extractors.FromURL(url)
	if ec == nil || ec.Extractor == nil {
		fmt.Println("  RESULT nil -> bot replies NOTHING")
		os.Exit(2)
	}
	fmt.Printf("  OK extractor=%s url=%s id=%s\n", ec.Extractor.ID, ec.ContentURL, ec.ContentID)

	fmt.Println("STEP 2 GetMedia")
	resp, err := ec.Extractor.GetFunc(ec)
	if err != nil {
		fmt.Println("  RESULT err:", err)
		os.Exit(3)
	}
	if resp == nil || resp.Media == nil {
		fmt.Printf("  RESULT no media (resp=%+v)\n", resp)
		os.Exit(4)
	}
	fmt.Printf("  OK items=%d\n", len(resp.Media.Items))
	for i, it := range resp.Media.Items {
		for _, f := range it.Formats {
			fmt.Printf("    [%d] %s %s %dx%d\n", i, f.FormatID, f.VideoCodec, f.Width, f.Height)
		}
	}
	_ = util.TelegramBotAPIFileLimit
	_ = core.HandleError
	_ = context.Background
}
