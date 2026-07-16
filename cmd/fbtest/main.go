package main

import (
 "fmt"
 "io"
 "regexp"
 "strings"
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
 desktop:="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
 url:="https://www.facebook.com/photo.php?fbid=992354417168118"
 params:=&networking.RequestParams{Cookies: cookies, Headers: map[string]string{"User-Agent": desktop}}
 resp,err:=client.Fetch("GET", url, params)
 if err!=nil { fmt.Printf("err %v\n", err); return }
 b,_:=io.ReadAll(resp.Body); resp.Body.Close()
 s:=string(b)
 fmt.Printf("len=%d final=%s\n", len(b), resp.Request.URL.String())
 unesc:=strings.ReplaceAll(s, `\/`, "/")
 fmt.Printf("scontent raw %d\n", strings.Count(s, "scontent"))
 // print all scontent URLs raw with permissive up to next quote
 reRaw:=regexp.MustCompile(`https:\\/\\/[^"]*scontent[^"]*`)
 ms:=reRaw.FindAllString(s, -1)
 fmt.Printf("raw escaped matches %d\n", len(ms))
 for i,m:=range ms {
  if i>=10 { break }
  fmt.Printf(" [%d] %s\n", i, func()string{ if len(m)>400 { return m[:400] } else { return m } }())
 }
 re2:=regexp.MustCompile(`https://[^"'\s<>]*scontent[^"'\s<>]*`)
 ms2:=re2.FindAllString(unesc, -1)
 fmt.Printf("unesc permissive matches %d\n", len(ms2))
 for i,m:=range ms2 {
  if i>=20 { break }
  fmt.Printf(" [%d] %.500s\n", i, m)
 }
 // also look for fbid patterns
 reFbid:=regexp.MustCompile(`\b\d{15,16}\b`)
 allNums:=reFbid.FindAllString(s, -1)
 uniq:=map[string]int{}
 for _,n:=range allNums { if strings.HasPrefix(n, "99235") { uniq[n]++ } }
 fmt.Printf("nums starting 99235: %v\n", uniq)
 // check for og:description
 reOgDesc:=regexp.MustCompile(`og:description"[^>]*content="([^"]+)"`)
 if m:=reOgDesc.FindStringSubmatch(s); len(m)>=2 { fmt.Printf("ogdesc %.500s\n", m[1]) }
 // check for description text patterns
 reDesc:=regexp.MustCompile(`"text"\s*:\s*"([^"]{20,})"`)
 long:=reDesc.FindAllStringSubmatch(s, -1)
 fmt.Printf("long text patterns %d\n", len(long))
 for i,m:=range long {
  if i>=5 { break }
  fmt.Printf("  %d %.200s\n", i, m[1])
 }
}
