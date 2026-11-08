package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"time"

	utls "github.com/refraction-networking/utls"
)

var (
	reProg = regexp.MustCompile(`progressive_url`)
	reMani = regexp.MustCompile(`"manifest_xml"`)
)

// Probe via raw HTTP/1.1 GET over uTLS conn, with ALPN forced to http/1.1
// via custom spec override. We use the ApplyPreset API to mutate ALPN ext.
func probeH1(label string, spec *utls.ClientHelloID, cookies string) {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	conn, err := dialer.DialContext(context.Background(), "tcp", "www.facebook.com:443")
	if err != nil {
		fmt.Println(label, "dial:", err)
		return
	}
	cfg := &utls.Config{ServerName: "www.facebook.com"}
	tc := utls.UClient(conn, cfg, *spec)
	// Override ALPN to http/1.1 via custom extension before handshake.
	tc.BuildHandshakeState()
	// Mutate: find ALPN extension and replace with http/1.1 only.
	for _, ext := range tc.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
	if err := tc.Handshake(); err != nil {
		fmt.Println(label, "handshake:", err)
		return
	}
	st := tc.ConnectionState()
	fmt.Printf("%-22s ALPN=%q\n", label, st.NegotiatedProtocol)

	req := fmt.Sprintf("GET /watch/?v=2182203379338397&_rdr HTTP/1.1\r\nHost: www.facebook.com\r\nUser-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36\r\nAccept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\nAccept-Language: en-us,en;q=0.5\r\nSec-Fetch-Mode: navigate\r\nAccept-Encoding: identity\r\nConnection: close\r\nCookie: %s\r\n\r\n", cookies)
	tc.Write([]byte(req))

	buf := make([]byte, 0, 1<<22)
	tmp := make([]byte, 32*1024)
	tc.SetReadDeadline(time.Now().Add(40 * time.Second))
	for {
		n, rerr := tc.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	tc.Close()
	s := string(buf)
	body := s
	for i := 0; i+4 <= len(s); i++ {
		if s[i:i+4] == "\r\n\r\n" {
			body = s[i+4:]
			break
		}
	}
	fmt.Printf("  total=%d body=%d prog=%d manifest=%d\n",
		len(buf), len(body),
		len(reProg.FindAllString(body, -1)), len(reMani.FindAllString(body, -1)))
	if reSC := regexp.MustCompile(`Set-Cookie: [^\r\n]+`); reSC != nil {
		if m := reSC.FindString(s); m != "" {
			fmt.Printf("  %s\n", trunc(m, 100))
		}
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func main() {
	b, err := os.ReadFile("private/cookies/facebook.txt")
	if err != nil {
		fmt.Println("cookie err:", err)
		return
	}
	var ck string
	for _, line := range regexp.MustCompile(`\r?\n`).Split(string(b), -1) {
		if line == "" || line[0] == '#' {
			continue
		}
		p := regexp.MustCompile(`\t`).Split(line, -1)
		if len(p) >= 7 {
			ck += p[5] + "=" + p[6] + "; "
		}
	}
	fmt.Println("cookies built:", len(ck), "chars")

	probeH1("Chrome_120 h1-ALPN", &utls.HelloChrome_120, ck)
	probeH1("Firefox_120 h1-ALPN", &utls.HelloFirefox_120, ck)
	probeH1("Golang h1-ALPN", &utls.HelloGolang, ck)
	_ = io.EOF
	_ = http.MethodGet
}
