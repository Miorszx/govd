package main

import (
	"fmt"
	"io"
	"net/http"
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
	cookies := util.GetExtractorCookies("facebook")
	client := networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: true})
	iPhoneUA := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

	urls := []string{
		"https://mbasic.facebook.com/100044483197537/posts/1584014339757991/",
		"https://m.facebook.com/100044483197537/posts/1584014339757991/",
		"https://www.facebook.com/100044483197537/posts/1584014339757991/",
		"https://mbasic.facebook.com/61586617688146/posts/122116959507220589/",
		"https://m.facebook.com/61586617688146/posts/122116959507220589/",
	}

	scontentRe := regexp.MustCompile(`https?://[^"\s']*scontent[^"\s']*`)
	photoRe := regexp.MustCompile(`photo\.php\?fbid=(\d+)`)
	ogRe := regexp.MustCompile(`property="og:image"[^>]+content="([^"]+)"`)

	for _, url := range urls {
		fmt.Printf("\n========== %s ==========\n", url)
		params := &networking.RequestParams{Cookies: cookies, Headers: map[string]string{"User-Agent": iPhoneUA}}
		resp, err := client.Fetch(http.MethodGet, url, params)
		if err != nil {
			fmt.Printf("fetch err %v\n", err)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		s := string(b)
		fmt.Printf("len=%d status=%d final=%s\n", len(s), resp.StatusCode, resp.Request.URL.String())
		// scontent
		allS := scontentRe.FindAllString(s, -1)
		fmt.Printf("scontent total matches (raw): %d\n", len(allS))
		uniq := map[string]bool{}
		filenames := map[string][]string{}
		for _, u := range allS {
			// clean up \/ and &amp;
			clean := strings.ReplaceAll(u, `\/`, "/")
			clean = strings.ReplaceAll(clean, "&amp;", "&")
			uniq[clean] = true
			// filename
			tmp := clean
			if idx := strings.Index(tmp, "?"); idx != -1 {
				tmp = tmp[:idx]
			}
			if idx := strings.LastIndex(tmp, "/"); idx != -1 {
				tmp = tmp[idx+1:]
			}
			filenames[tmp] = append(filenames[tmp], clean)
		}
		fmt.Printf("unique scontent urls: %d\n", len(uniq))
		for fn, list := range filenames {
			if len(fn) > 10 {
				fmt.Printf("  file=%s count=%d example=%.120s\n", fn, len(list), list[0])
			}
		}
		// photo.php fbid
		fbids := photoRe.FindAllStringSubmatch(s, -1)
		uniqFb := map[string]bool{}
		for _, m := range fbids {
			if len(m) >= 2 {
				uniqFb[m[1]] = true
			}
		}
		fmt.Printf("photo.php fbid unique: %d\n", len(uniqFb))
		for id := range uniqFb {
			fmt.Printf("  fbid=%s\n", id)
		}
		// og:image
		if m := ogRe.FindStringSubmatch(s); len(m) >= 2 {
			fmt.Printf("og:image=%.150s\n", m[1])
		} else {
			fmt.Printf("og:image NOT found\n")
		}
		// check for subattachments / attachments
		if strings.Contains(s, "subattachments") {
			fmt.Printf("contains subattachments keyword\n")
			// print snippet around it
			idx := strings.Index(s, "subattachments")
			if idx != -1 {
				start := idx - 100
				if start < 0 { start = 0 }
				end := idx + 500
				if end > len(s) { end = len(s) }
				snip := s[start:end]
				snip = strings.ReplaceAll(snip, "\n", " ")
				fmt.Printf("  snippet: %.500s\n", snip)
			}
		}
	}
}
