package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"govd/config"
	"govd/models"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type EdgeProxyClient struct {
	*http.Client

	proxyURL string
}

var (
	httpSession     *http.Client
	httpSessionOnce sync.Once

	extractorsHttpSession = make(map[string]models.HTTPClient)
)

func GetDefaultHTTPSession() *http.Client {
	httpSessionOnce.Do(func() {
		httpSession = &http.Client{
			Transport: GetBaseTransport(),
			Timeout:   60 * time.Second,
		}
	})
	return httpSession
}

func GetBaseTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       100,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableCompression:    false,
	}
}

func GetHTTPSession(extractor string) models.HTTPClient {
	if client, ok := extractorsHttpSession[extractor]; ok {
		return client
	}

	cfg := config.GetExtractorConfig(extractor)
	if cfg == nil {
		return GetDefaultHTTPSession()
	}

	if cfg.EdgeProxyURL != "" {
		client := GetEdgeProxyClient(cfg.EdgeProxyURL)
		extractorsHttpSession[extractor] = client
		return client
	}

	transport := GetBaseTransport()
	client := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	if cfg.HTTPProxy == "" && cfg.HTTPSProxy == "" {
		extractorsHttpSession[extractor] = client
		return client
	}

	var httpProxyURL, httpsProxyURL *url.URL
	var err error

	if cfg.HTTPProxy != "" {
		if httpProxyURL, err = url.Parse(cfg.HTTPProxy); err != nil {
			log.Printf("warning: invalid HTTP proxy URL '%s': %v\n", cfg.HTTPProxy, err)
		}
	}

	if cfg.HTTPSProxy != "" {
		if httpsProxyURL, err = url.Parse(cfg.HTTPSProxy); err != nil {
			log.Printf("warning: invalid HTTPS proxy URL '%s': %v\n", cfg.HTTPSProxy, err)
		}
	}

	if httpProxyURL != nil || httpsProxyURL != nil {
		noProxyList := strings.Split(cfg.NoProxy, ",")
		for i := range noProxyList {
			noProxyList[i] = strings.TrimSpace(noProxyList[i])
		}

		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if cfg.NoProxy != "" {
				host := req.URL.Hostname()
				for _, p := range noProxyList {
					if p == "" {
						continue
					}
					if p == host || (strings.HasPrefix(p, ".") && strings.HasSuffix(host, p)) {
						return nil, nil
					}
				}
			}
			if req.URL.Scheme == "https" && httpsProxyURL != nil {
				return httpsProxyURL, nil
			}
			if req.URL.Scheme == "http" && httpProxyURL != nil {
				return httpProxyURL, nil
			}
			if httpsProxyURL != nil {
				return httpsProxyURL, nil
			}
			return httpProxyURL, nil
		}
	}

	extractorsHttpSession[extractor] = client
	return client
}

func GetEdgeProxyClient(proxyURL string) *EdgeProxyClient {
	edgeProxyClient := &EdgeProxyClient{
		Client: &http.Client{
			Transport: GetBaseTransport(),
			Timeout:   60 * time.Second,
		},
		proxyURL: proxyURL,
	}
	return edgeProxyClient
}

func (c *EdgeProxyClient) Do(req *http.Request) (*http.Response, error) {
	if c.proxyURL == "" {
		return nil, fmt.Errorf("proxy URL is not set")
	}
	targetURL := req.URL.String()
	encodedURL := url.QueryEscape(targetURL)
	proxyURLWithParam := c.proxyURL + "?url=" + encodedURL

	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading request body: %w", err)
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	proxyReq, err := http.NewRequest(
		req.Method,
		proxyURLWithParam,
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating proxy request: %w", err)
	}

	for name, values := range req.Header {
		for _, value := range values {
			proxyReq.Header.Add(name, value)
		}
	}

	proxyResp, err := c.Client.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer proxyResp.Body.Close()

	body, err := io.ReadAll(proxyResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading proxy response: %w", err)
	}

	var response models.ProxyResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error parsing proxy response: %w", err)
	}

	resp := &http.Response{
		StatusCode: response.StatusCode,
		Status:     fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode)),
		Body:       io.NopCloser(bytes.NewBufferString(response.Text)),
		Header:     make(http.Header),
		Request:    req,
	}
	parsedResponseURL, err := url.Parse(response.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing response URL: %w", err)
	}
	resp.Request.URL = parsedResponseURL

	for name, value := range response.Headers {
		resp.Header.Set(name, value)
	}

	for _, cookie := range response.Cookies {
		resp.Header.Add("Set-Cookie", cookie)
	}

	return resp, nil
}
