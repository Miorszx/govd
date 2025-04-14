package util

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	httpSession     *http.Client
	httpSessionOnce sync.Once
)

func GetHTTPSession() *http.Client {
	httpSessionOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConnsPerHost:   10,
			MaxConnsPerHost:       10,
		}

		httpSession = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	})
	return httpSession
}
