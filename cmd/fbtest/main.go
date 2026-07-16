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
desktopUA:="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
urls:=[]string{
"https://www.facebook.com/share/19HrxCEJWd/",
"https://www.facebook.com/100091807068839/posts/992354417168118/",
"https://www.facebook.com/HambaLagiFakir/posts/992354417168118/",
"https://www.facebook.com/story.php?story_fbid=992354417168118&id=100091807068839",
"https://m.facebook.com/100091807068839/posts/992354417168118/",
"https://mbasic.facebook.com/100091807068839/posts/992354417168118/",
}
reDoc:=regexp.MustCompile(`"doc_id"\s*:\s*"(\d+)"`)
reFriendly:=regexp.MustCompile(`"fb_api_req_friendly_name"\s*:\s*"([^"]+)"`)
reDTSG:=regexp.MustCompile(`"DTSGInitialData".*?"token"\s*:\s*"([^"]+)"`)
reJazo:=regexp.MustCompile(`jazoest=(\d+)`)
reLSD:=regexp.MustCompile(`"LSD".*?"token"\s*:\s*"([^"]+)"`)
for _,u:=range urls{
for _,ua:=range []string{iPhoneUA, desktopUA}{
fmt.Printf("\n=== %s UA=%.20s ===\n", u, ua[:20])
params:=&networking.RequestParams{Cookies: cookies, Headers: map[string]string{"User-Agent": ua}}
resp,err:=client.Fetch(http.MethodGet, u, params)
if err!=nil{fmt.Printf("err %v\n",err);continue}
b,_:=io.ReadAll(resp.Body); resp.Body.Close()
s:=string(b)
fmt.Printf("len=%d final=%s\n", len(s), resp.Request.URL.String())
docs:=reDoc.FindAllStringSubmatch(s,-1)
fmt.Printf("doc_id count=%d\n", len(docs))
uniq:=map[string]int{}
for _,m:=range docs{uniq[m[1]]++}
cnt:=0
for id,c:=range uniq{
if cnt<15{
fmt.Printf(" doc %s x%d\n", id, c)
cnt++
}
}
fns:=reFriendly.FindAllStringSubmatch(s,-1)
uniqF:=map[string]int{}
for _,m:=range fns{uniqF[m[1]]++}
fmt.Printf("friendly %d\n", len(uniqF))
cnt=0
for fn,c:=range uniqF{
if cnt<20{
fmt.Printf("  %s x%d\n", fn, c)
cnt++
}
}
if m:=reDTSG.FindStringSubmatch(s); len(m)>=2{
fmt.Printf("DTSG %.30s\n", m[1])
}
if m:=reLSD.FindStringSubmatch(s); len(m)>=2{
fmt.Printf("LSD %.30s\n", m[1])
}
if m:=reJazo.FindStringSubmatch(s); len(m)>=2{
fmt.Printf("jazoest=%s\n", m[1])
}
}
}
}
