// compare HEAD vs GET-range on mbasic m367 urls, and check cookie requirement
package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const uaM = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func loadCookies(path string) []*http.Cookie {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []*http.Cookie
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) < 7 {
			continue
		}
		out = append(out, &http.Cookie{Name: p[5], Value: p[6], Domain: p[0], Path: p[2]})
	}
	return out
}

func get(url string, ck []*http.Cookie) (int, string) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", uaM)
	for _, c := range ck {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func main() {
	id := os.Args[1]
	ck := loadCookies("/app/private/cookies/facebook.txt")
	st, body := get("https://mbasic.facebook.com/reel/"+id, ck)
	if st != 200 {
		fmt.Println("mbasic fail", st)
		return
	}
	norm := strings.ReplaceAll(body, `\/`, "/")
	re := regexp.MustCompile(`https:[^"'\s\\]+\.mp4[^"'\s\\]*`)
	all := re.FindAllString(norm, -1)
	seen := map[string]bool{}
	var hd []string
	for _, u := range all {
		if seen[u] {
			continue
		}
		seen[u] = true
		if strings.Contains(u, "m367") || strings.Contains(u, "m366") {
			hd = append(hd, u)
		}
	}
	fmt.Printf("hd urls: %d\n", len(hd))

	for i, u := range hd {
		fmt.Printf("\n--- hd[%d] host=%s\n", i, u[8:strings.Index(u[8:], "/")+8])
		// HEAD
		req, _ := http.NewRequest("HEAD", u, nil)
		req.Header.Set("User-Agent", uaM)
		r, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		if err != nil {
			fmt.Printf("    HEAD err %v\n", err)
		} else {
			fmt.Printf("    HEAD status=%d type=%q len=%s\n", r.StatusCode, r.Header.Get("Content-Type"), r.Header.Get("Content-Length"))
			r.Body.Close()
		}
		// GET range
		req2, _ := http.NewRequest("GET", u, nil)
		req2.Header.Set("User-Agent", uaM)
		req2.Header.Set("Range", "bytes=0-1023")
		r2, err2 := (&http.Client{Timeout: 20 * time.Second}).Do(req2)
		if err2 != nil {
			fmt.Printf("    GET err %v\n", err2)
			continue
		}
		n, _ := io.Copy(io.Discard, r2.Body)
		r2.Body.Close()
		fmt.Printf("    GET  status=%d type=%q range=%q bytes=%d\n", r2.StatusCode, r2.Header.Get("Content-Type"), r2.Header.Get("Content-Range"), n)
	}
}
