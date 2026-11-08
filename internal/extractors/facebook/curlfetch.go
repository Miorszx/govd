package facebook

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/util"
)

// fetchViaCurl fetches a Facebook page using the curl binary instead of Go's
// net/http stack. Facebook fingerprints Go's TLS ClientHello (and the HTTP/2
// frame ordering of x/net/http2) and serves a session-killing response
// (Set-Cookie: c_user=deleted) plus a 500 KB generic shell with no
// progressive_url / dash_manifest. Curl (BoringSSL/OpenSSL) receives the
// real ~3.2 MB page with media. Pure-Go TLS impersonation via uTLS did NOT
// help — Chrome_120/133, Firefox_120 specs all rejected. This is the only
// reliable way to keep FB extraction working in 2026.
//
// curl is required in the runtime image (apk add curl). Download of the
// actual media still uses Go's net/http — only the page fetch goes through
// curl.
func fetchViaCurl(ctx context.Context, url string) ([]byte, error) {
	// Build cookie header from extractor cookies.
	cookies := util.GetExtractorCookies("facebook")
	var ck strings.Builder
	for _, c := range cookies {
		ck.WriteString(c.Name)
		ck.WriteByte('=')
		ck.WriteString(c.Value)
		ck.WriteString("; ")
	}

	args := []string{
		"-sL",
		"--compressed",
		"--max-time", "30",
		"-A", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		"-H", "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"-H", "Accept-Language: en-us,en;q=0.5",
		"-H", "Sec-Fetch-Mode: navigate",
		"-H", "Sec-Fetch-Dest: document",
		"-H", "Sec-Fetch-Site: none",
		"-H", "Sec-Fetch-User: ?1",
		"-H", "Upgrade-Insecure-Requests: 1",
	}
	if ck.Len() > 0 {
		args = append(args, "-H", "Cookie: "+ck.String())
	}
	args = append(args, url)

	cctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "curl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("curl fetch %s: %w (%s)", url, err, strings.TrimSpace(stderr.String()))
	}
	body := stdout.Bytes()
	if len(body) < 1000 {
		return nil, fmt.Errorf("curl fetch %s: short body (%d bytes)", url, len(body))
	}
	return body, nil
}
