//go:build ignore

// Direct extractor probe. No Telegram, no curl one-by-one -- call the extractor
// and print what it actually returns, so the truth is one table.
//
//	go run zz_e2e/zz_verify.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/govdbot/govd/internal/extractors"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/core"
	"github.com/govdbot/govd/internal/models"
)

func probe(url string) {
	fmt.Printf("\n=== %s\n", url)
	start := time.Now()

	ec := extractors.FromURL(url)
	if ec == nil || ec.Extractor == nil {
		fmt.Printf("  FromURL: NIL  (%.1fs)  <- bot replies NOTHING\n",
			time.Since(start).Seconds())
		return
	}
	defer ec.CancelFunc()
	fmt.Printf("  FromURL: ok  extractor=%s  id=%s  (%.1fs)\n",
		ec.Extractor.ID, ec.ContentID, time.Since(start).Seconds())

	resp, err := ec.Extractor.GetFunc(ec)
	if err != nil {
		fmt.Printf("  GetFunc: ERR %v  (%.1fs)\n", err, time.Since(start).Seconds())
		return
	}
	if resp == nil || resp.Media == nil || len(resp.Media.Items) == 0 {
		fmt.Printf("  GetFunc: NO MEDIA  (%.1fs)\n", time.Since(start).Seconds())
		return
	}

	fmt.Printf("  GetFunc: ok  items=%d  caption=%.40q  (%.1fs)\n",
		len(resp.Media.Items), resp.Media.Caption, time.Since(start).Seconds())
	for i, it := range resp.Media.Items {
		for _, f := range it.Formats {
			u := ""
			if len(f.URL) > 0 {
				u = f.URL[0]
				if len(u) > 46 {
					u = u[:46] + "..."
				}
			}
			fmt.Printf("    [%d] %-11s codec=%-4s %dx%d size=%dB  %s\n",
				i, f.FormatID, f.VideoCodec, f.Width, f.Height, f.FileSize, u)
		}
	}

	// The delivery path, not just extraction. This is where a share link died
	// silently: everything above passed, then the bot produced no message.
	fmt.Printf("  Download: probing...\n")
	ds := time.Now()
	res, err := core.DownloadFormats(ec, resp.Media)
	if err != nil {
		fmt.Printf("  Download: ERR %v  (%.1fs)\n", err, time.Since(ds).Seconds())
		return
	}
	fmt.Printf("  Download: ok  formats=%d  (%.1fs)\n",
		len(res), time.Since(ds).Seconds())
	for _, d := range res {
		st := "ok"
		if d.Error != nil {
			st = "ERR " + d.Error.Error()
		}
		fmt.Printf("    [%d] %s  file=%s\n", d.Index, st, d.FilePath)
	}
}

func main() {
	logger.Init()
	urls := os.Args[1:]
	if len(urls) == 0 {
		urls = []string{
			// control: a plain reel still works
			"https://www.facebook.com/reel/3093314621017769",
			// the share link that keeps failing
			"https://www.facebook.com/share/p/1A5dDnp3j5/",
		}
	}
	for _, u := range urls {
		probe(u)
	}
	_ = context.Background
	_ = models.ExtractorResponse{}
}
