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
			ForceAttemptHTTP2: true,

			MaxIdleConns:    100,
			IdleConnTimeout: 90 * time.Second,

			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,

			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,

			ResponseHeaderTimeout: 10 * time.Second,

			DisableCompression: false,
		}

		httpSession = &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		}
	})
	return httpSession
}
