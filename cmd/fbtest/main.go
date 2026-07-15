package main
import (
"fmt"
"github.com/govdbot/govd/internal/config"
_ "github.com/govdbot/govd/internal/extractors"
"github.com/govdbot/govd/internal/extractors"
"github.com/govdbot/govd/internal/logger"
)
func main(){
config.Load()
logger.Init()
urls:=[]string{
"https://www.facebook.com/share/1CKF4qojsQ/",
"https://www.facebook.com/share/p/1FtTAuWcPo/",
}
for _,u:=range urls{
fmt.Printf("\n=== TEST %s ===\n",u)
ctx:=extractors.FromURL(u)
if ctx==nil{fmt.Println("FromURL NIL");continue}
fmt.Printf("FromURL ID=%s URL=%.120s\n",ctx.ContentID, ctx.ContentURL)
resp,err:=ctx.Extractor.GetFunc(ctx)
if err!=nil{fmt.Printf("FAIL %v\n",err);continue}
if resp.URL!=""{
 fmt.Printf("REDIRECT -> %s\n",resp.URL)
 ctx2:=extractors.FromURL(resp.URL)
 if ctx2==nil{fmt.Println("second FromURL NIL");continue}
 fmt.Printf("second ID=%s URL=%.120s\n",ctx2.ContentID, ctx2.ContentURL)
 resp2,err:=ctx2.Extractor.GetFunc(ctx2)
 if err!=nil{fmt.Printf("second FAIL %v\n",err);continue}
 if resp2.Media!=nil && len(resp2.Media.Items)>0 && len(resp2.Media.Items[0].Formats)>0{
  f:=resp2.Media.Items[0].Formats[0]
  fmt.Printf("second OK type=%s items=%d url=%.150s cap=%.100q\n", f.Type, len(resp2.Media.Items), f.URL[0], resp2.Media.Caption)
  if len(resp2.Media.Items[0].Formats)>1{
   fmt.Printf("  2nd fmt type=%s url=%.150s\n", resp2.Media.Items[0].Formats[1].Type, resp2.Media.Items[0].Formats[1].URL[0])
  }
 }
}else{
 if resp.Media!=nil && len(resp.Media.Items)>0{
  f:=resp.Media.Items[0].Formats[0]
  fmt.Printf("OK type=%s items=%d url=%.150s cap=%.100q\n", f.Type, len(resp.Media.Items), f.URL[0], resp.Media.Caption)
 }
}
}
}
