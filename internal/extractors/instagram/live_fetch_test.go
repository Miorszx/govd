package instagram

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/govdbot/govd/internal/util"
)

// TestLiveFetchReelDfUDeEHzDQn does the real thing: HTTP GET with the bot's
// exact header set and cookie jar, then parse. No fixture. Proves the deployed
// binary, not a saved page.
func TestLiveFetchReelDfUDeEHzDQn(t *testing.T) {
	if os.Getenv("IG_LIVE") != "1" {
		t.Skip("set IG_LIVE=1 to hit instagram")
	}

	const sc = "DfUDeEHzDQn"
	req, err := http.NewRequest(http.MethodGet, buildPageURL(sc), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range igHTMLHeaders {
		req.Header.Set(k, v)
	}
	// same UA the networking layer injects when none is supplied
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10; SM-G960U) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.181 Mobile Safari/537.36")

	for _, c := range loadIGCookies(t) {
		req.AddCookie(c)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Logf("HTTP %d  %d bytes", resp.StatusCode, len(body))

	page, err := parseIGHTML(string(body), sc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(page.VideoVersions) == 0 {
		t.Fatalf("VIDEO MISSING -- %d images only, bot sends a photo", len(page.Images))
	}
	v := page.VideoVersions[0]
	for _, c := range page.VideoVersions {
		if c.Width*c.Height > v.Width*v.Height {
			v = c
		}
	}
	t.Logf("VIDEO %dx%d  versions=%d  images=%d  user=%q",
		v.Width, v.Height, len(page.VideoVersions), len(page.Images), page.Username)
	if v.Width < 100 || v.Height < 100 {
		t.Fatalf("implausible dimensions %dx%d", v.Width, v.Height)
	}
}

// loadIGCookies uses the bot's own parser so the test exercises the real path.
// repoRoot walks up until go.mod is found so the cookie dir resolves.
func repoRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}

func loadIGCookies(t *testing.T) []*http.Cookie {
	t.Helper()
	if err := os.Chdir(repoRoot()); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	cks := util.GetExtractorCookies("instagram")
	if len(cks) == 0 {
		t.Skip("no cookies parsed -- private/cookies/instagram.txt missing")
	}
	return cks
}

// TestDumpLivePage writes the live page so the byte difference can be diffed.
func TestDumpLivePage(t *testing.T) {
	if os.Getenv("IG_DUMP") != "1" {
		t.Skip("set IG_DUMP=1")
	}
	const sc = "DfUDeEHzDQn"
	req, _ := http.NewRequest(http.MethodGet, buildPageURL(sc), nil)
	for k, v := range igHTMLHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10; SM-G960U) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.181 Mobile Safari/537.36")
	cookies := loadIGCookies(t)
	t.Logf("cookies loaded: %d", len(cookies))
	for _, c := range cookies {
		t.Logf("  cookie %s domain=%s value=%d chars", c.Name, c.Domain, len(c.Value))
		req.AddCookie(c)
	}
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	os.WriteFile("/tmp/ig_go_live.html", body, 0o644)
	s := string(body)
	t.Logf("len=%d video_versions=%d candidates=%d code_marker=%d",
		len(s), countSub(s, `"video_versions"`), countSub(s, `"candidates"`),
		countSub(s, `"code":"DfUDeEHzDQn"`))
}

func countSub(s, sub string) int {
	n, i := 0, 0
	for {
		j := indexFrom(s, sub, i)
		if j < 0 {
			return n
		}
		n++
		i = j + len(sub)
	}
}

func indexFrom(s, sub string, from int) int {
	if from >= len(s) {
		return -1
	}
	j := -1
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			j = i
			break
		}
	}
	return j
}
