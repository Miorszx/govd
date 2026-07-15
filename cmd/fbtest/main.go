package main
import (
"fmt"
"time"
"github.com/govdbot/govd/internal/config"
_ "github.com/govdbot/govd/internal/extractors"
"github.com/govdbot/govd/internal/extractors"
"github.com/govdbot/govd/internal/logger"
)
func main(){
config.Load()
logger.Init()
urls := []string{
"https://www.facebook.com/share/r/18u16rJdCw/",
"https://www.facebook.com/share/1TCHajK2yJ/",
"https://www.facebook.com/share/v/1HdwFEBAEU/",
"https://www.facebook.com/share/r/1JLtAksBnE/",
"https://www.facebook.com/share/1Csq4udEtr/",
"https://www.facebook.com/share/r/1F39cTrRXR/",
"https://www.facebook.com/share/v/1GabDbzdiB/",
"https://www.facebook.com/share/r/17tgTbXFtB/",
"https://www.facebook.com/share/r/1cTReqwkAi/",
"https://www.facebook.com/share/v/18sfXaKbNP/",
"https://www.facebook.com/share/r/1F1zBwNBCD/",
}
for idx, u := range urls {
fmt.Printf("\n=== [%d/%d] %s ===\n", idx+1, len(urls), u)
ctx := extractors.FromURL(u)
if ctx == nil {
fmt.Printf("FAIL: no extractor matched\n")
continue
}
fmt.Printf("initial ID=%s\n", ctx.ContentID)
resp, err := ctx.Extractor.GetFunc(ctx)
if err != nil {
fmt.Printf("FAIL initial: %v\n", err)
continue
}
if resp.URL != "" {
fmt.Printf("REDIRECT -> %s\n", resp.URL[:100])
ctx2 := extractors.FromURL(resp.URL)
if ctx2 == nil {
fmt.Printf("FAIL no extractor redirect\n")
continue
}
fmt.Printf("redirect ID=%s\n", ctx2.ContentID)
resp2, err := ctx2.Extractor.GetFunc(ctx2)
if err != nil {
fmt.Printf("FAIL redirect GetFunc: %v\n", err)
continue
}
if resp2.Media != nil {
c := resp2.Media.Caption
if len(c) > 150 { fmt.Printf("CAPTION len=%d: %q...\n", len(c), c[:150]) } else { fmt.Printf("CAPTION len=%d: %q\n", len(c), c) }
fmt.Printf("ITEMS=%d formats=%d\n", len(resp2.Media.Items), func() int { if len(resp2.Media.Items)>0 {return len(resp2.Media.Items[0].Formats)}; return 0 }())
} else {
fmt.Printf("NO MEDIA after redirect\n")
}
} else if resp.Media != nil {
c := resp.Media.Caption
fmt.Printf("CAPTION len=%d: %q\n", len(c), c)
} else {
fmt.Printf("NO MEDIA\n")
}
time.Sleep(500*time.Millisecond)
}
}
