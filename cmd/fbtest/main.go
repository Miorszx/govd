package main
import (
"fmt"
"io"
"net/http"
"github.com/govdbot/govd/internal/config"
"github.com/govdbot/govd/internal/util"
"github.com/govdbot/govd/internal/logger"
"github.com/govdbot/govd/internal/networking"
)
func main(){
config.Load()
logger.Init()
cookies:=util.GetExtractorCookies("facebook")
client:=networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: true})
headers:=map[string]string{
 "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
 "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}
tests:=[]string{
"https://www.facebook.com/share/p/1DK7Xm5tcL/",
"https://www.facebook.com/share/1HKdWKVXwn/",
}
for _,u:=range tests{
 fmt.Printf("\n=== FETCH %s desktop ===\n",u)
 params:=&networking.RequestParams{Cookies: cookies, Headers: headers}
 resp,err:=client.Fetch(http.MethodGet, u, params)
 if err!=nil{fmt.Printf("err %v\n",err);continue}
 b,_:=io.ReadAll(resp.Body)
 resp.Body.Close()
 s:=string(b)
 fmt.Printf("len %d final %s status %d\n",len(s), resp.Request.URL.String(), resp.StatusCode)
 if len(s)<2000{
  fmt.Printf("body %.1000s\n", s)
 } else {
  fmt.Printf("head %.500s\n", s[:500])
  // save full to /tmp for grep
  // quick search for story
  if len(s)>50000{
   // already have
  }
  // look for 1DK7Xm5tcL inside
  idx:=0
  for i:=0;i<3;i++{
   pos:=indexOf(s[idx:], "1DK7Xm5tcL")
   if pos>=0{
    fmt.Printf("found 1DK7Xm5tcL at %d: %.200s\n", idx+pos, s[idx+pos-50:idx+pos+150])
    idx+=pos+10
   } else {break}
  }
  pos:=indexOf(s, "post_id")
  if pos>=0{
   fmt.Printf("post_id ctx %.300s\n", s[pos-20:pos+100])
  }
 }
}
}
func indexOf(s, sub string) int{
 for i:=0;i<=len(s)-len(sub);i++{
  if s[i:i+len(sub)]==sub{return i}
 }
 return -1
}
