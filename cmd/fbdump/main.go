package main

// Dumps the raw body the facebook extractor fetches for a reel, so we can
// locate where the caption regex actually matches (random feed story vs the
// real post message).
import (
	"fmt"
	"os"

	"github.com/govdbot/govd/internal/core"
	"github.com/govdbot/govd/internal/extractors/facebook"
)

func main() {
	link := "https://www.facebook.com/share/v/1RqkYmnKVL/"
	if len(os.Args) > 1 {
		link = os.Args[1]
	}
	// resolve share
	final := link
	if facebook.ShareExtractor.URLPattern.MatchString(link) {
		if u, err := facebook.ShareExtractor.GetFunc(nil, &core.ExtractorContext{ContentURL: link}); err == nil && u != nil {
			final = u.URL
		}
	}
	id := ""
	fmt.Sscanf(final[0:0], "")
	for i := len(final) - 1; i >= 0; i-- {
		if final[i] == '=' {
			id = final[i+1:]
			break
		}
	}
	fmt.Println("final:", final)
	fmt.Println("id:", id)
	// fetch watch page with cookies
	ctx := &core.ExtractorContext{ContentURL: final, ContentID: id}
	body, err := ctx.FetchRaw("GET", "https://www.facebook.com/watch/?v="+id)
	if err != nil {
		fmt.Println("fetch err:", err)
		return
	}
	os.WriteFile("/tmp/fb_body.html", body, 0644)
	fmt.Println("body bytes:", len(body))
}
