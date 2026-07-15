package main
import (
"fmt"; "io"; "net/http"; "os"; "regexp"
"github.com/govdbot/govd/internal/config"
"github.com/govdbot/govd/internal/util"
"github.com/govdbot/govd/internal/logger"
"github.com/govdbot/govd/internal/networking"
)
func main(){
config.Load(); logger.Init()
cookies:=util.GetExtractorCookies("facebook")
client:=networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: true})
headers:=map[string]string{
 "User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
}
u:="https://www.facebook.com/share/p/1FtTAuWcPo/"
params:=&networking.RequestParams{Cookies: cookies, Headers: headers}
resp,err:=client.Fetch(http.MethodGet, u, params)
if err!=nil{return}
b,_:=io.ReadAll(resp.Body); resp.Body.Close()
os.WriteFile("/tmp/new_body.html", b, 0644)
fmt.Printf("wrote %d\n", len(b))
s:=string(b)
for _,kw:=range []string{"post_id","story_fbid","top_level_post_id","photo_id","fbid","story_token"}{
 re:=regexp.MustCompile(kw+`[^0-9]*(\d{10,})`)
 ms:=re.FindAllStringSubmatch(s, 10)
 for _,m:=range ms{
  fmt.Printf("%s => %s\n",kw,m[1])
 }
}
}
