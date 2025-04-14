package util

import (
	"net/http"
	"time"
)

var httpSession = NewHTTPSession()

func NewHTTPSession() *http.Client {
	session := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}
	return session
}

func GetHTTPSession() *http.Client {
	return httpSession
}
