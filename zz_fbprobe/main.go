package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

var (
	reProg = regexp.MustCompile(`progressive_url`)
	reMani = regexp.MustCompile(`"manifest_xml"`)
)

func probe(label string, client *networking.HTTPClient, url string, headers map[string]string) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Client.Do(req)
	if err != nil {
		fmt.Printf("%-34s ERR %v\n", label, err)
		return
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 1<<21)
	tmp := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	s := string(buf)
	fmt.Printf("%-34s proto=%-10s status=%d size=%-9d prog=%-3d manifest=%-3d\n",
		label, resp.Proto, resp.StatusCode, len(buf),
		len(reProg.FindAllString(s, -1)), len(reMani.FindAllString(s, -1)))
}

func main() {
	config.Load()
	logger.Init()
	cookies := util.GetExtractorCookies("facebook")

	// 1) chrome/HTTP2 (current prod behaviour)
	chrome := networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: true})
	// 2) plain HTTP/2 transport
	plainH2 := networking.NewHTTPClient(&networking.NewHTTPClientOptions{Cookies: cookies, Impersonate: false})
	// 3) plain with ForceAttemptHTTP2=false (HTTP/1.1)
	t := networking.NewTransport()
	t.ForceAttemptHTTP2 = false
	h1 := &networking.HTTPClient{
		Client:  &http.Client{Transport: t, Timeout: 0},
		Cookies: cookies,
	}

	UA150 := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
	H := map[string]string{
		"User-Agent":      UA150,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-us,en;q=0.5",
		"Sec-Fetch-Mode":  "navigate",
	}
	id := "2182203379338397"
	if len(os.Args) > 1 {
		id = os.Args[1]
	}
	u := "https://www.facebook.com/watch/?v=" + id + "&_rdr"

	probe("chrome (HTTP2)", chrome, u, H)
	probe("plain (HTTP2)", plainH2, u, H)
	probe("plain HTTP/1.1 FORCED", h1, u, H)
}
