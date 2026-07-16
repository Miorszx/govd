package main
import (
"fmt"
"io"
"net/http"
"regexp"
"github.com/govdbot/govd/internal/config"
"github.com/govdbot/govd/internal/networking"
"github.com/govdbot/govd/internal/util"
"github.com/govdbot/govd/internal/logger"
)
func main(){
config.Load()
logger.Init()
cookies:=util.GetExtractorCookies("facebook")
client:=networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: true})
iPhoneUA:="Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
url:="https://mbasic.facebook.com/share/p/19Fea5TgkK/"
params:=&networking.RequestParams{Cookies: cookies, Headers: map[string]string{"User-Agent": iPhoneUA}}
resp,err:=client.Fetch(http.MethodGet, url, params)
if err!=nil{fmt.Printf("err %v\n",err); return}
b,_:=io.ReadAll(resp.Body); resp.Body.Close()
s:=string(b)
fmt.Printf("len=%d final=%s\n", len(s), resp.Request.URL.String())
reOg:=regexp.MustCompile(`property="og:image"[^>]*content="([^"]+)"`)
if m:=reOg.FindStringSubmatch(s); len(m)>=2{
fmt.Printf("og:image FULL LEN %d\n%s\n", len(m[1]), m[1])
}
reUrl:=regexp.MustCompile(`property="og:url"[^>]*content="([^"]+)"`)
if m:=reUrl.FindStringSubmatch(s); len(m)>=2{
fmt.Printf("og:url=%s\n", m[1])
}
// also try find all scontent with full
reScontent:=regexp.MustCompile(`https://[^"\s']*scontent[^"\s']*`)
matches:=reScontent.FindAllString(s,-1)
fmt.Printf("scontent raw %d\n", len(matches))
for i,m:=range matches{
if i<3{
fmt.Printf(" [%d] len=%d full=%s\n", i, len(m), m)
}
}
}
