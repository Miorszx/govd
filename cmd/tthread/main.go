package main
import (
  "context"; "fmt"; "time"
  "github.com/govdbot/govd/internal/config"
  "github.com/govdbot/govd/internal/extractors/threads"
  "github.com/govdbot/govd/internal/logger"
  "github.com/govdbot/govd/internal/models"
  "github.com/govdbot/govd/internal/networking"
  "github.com/govdbot/govd/internal/util"
)
func main(){
  config.Load(); logger.Init()
  cookies := util.GetExtractorCookies("threads")
  client := networking.NewHTTPClient(&networking.NewHTTPClientOptions{Impersonate:true, Cookies:cookies})
  
  ctx := &models.ExtractorContext{
    ContentURL: "https://www.threads.com/share/BAZZ_5kDeB/",
    ContentID: "BAZZ_5kDeB",
    MatchGroups: map[string]string{"shareid": "BAZZ_5kDeB"},
    HTTPClient: client,
    Context: context.Background(),
    Extractor: threads.Extractor,
  }
  t := time.Now()
  resp, err := threads.Extractor.GetFunc(ctx)
  if err != nil { fmt.Println("ERR:", err); return }
  fmt.Printf("caption: %.80s\n", resp.Media.Caption)
  fmt.Printf("items: %d (%v)\n", len(resp.Media.Items), time.Since(t))
}
