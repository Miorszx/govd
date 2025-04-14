package util

import (
	"net/http"
	"time"
)

var httpSession = &http.Client{
	Timeout: 20 * time.Second,
}

func GetHTTPSession() *http.Client {
	return httpSession
}
