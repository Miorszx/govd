package util

import (
	"bytes"
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

	"github.com/bytedance/sonic"
)

var (
	defaultClient     *http.Client
	defaultClientOnce sync.Once
	extractorClients  = make(map[string]models.HTTPClient)
)

func GetDefaultHTTPClient() *http.Client {
	defaultClientOnce.Do(func() {
		defaultClient = &http.Client{
			Transport: createBaseTransport(),
			Timeout:   60 * time.Second,
		}
	})
	return defaultClient
}

func createBaseTransport() *http.Transport {
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

func GetHTTPClient(extractor string) models.HTTPClient {
	if client, exists := extractorClients[extractor]; exists {
		return client
	}

	cfg := config.GetExtractorConfig(extractor)
	if cfg == nil {
		return GetDefaultHTTPClient()
	}

	var client models.HTTPClient

	if cfg.EdgeProxyURL != "" {
		client = NewEdgeProxyClient(cfg.EdgeProxyURL)
	} else {
		client = createClientWithProxy(cfg)
	}

	extractorClients[extractor] = client
	return client
}

func createClientWithProxy(cfg *models.ExtractorConfig) *http.Client {
	transport := createBaseTransport()

	if cfg.HTTPProxy != "" || cfg.HTTPSProxy != "" {
		configureProxyTransport(transport, cfg)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}
}

func configureProxyTransport(
	transport *http.Transport,
	cfg *models.ExtractorConfig,
) {
	var httpProxyURL, httpsProxyURL *url.URL
	var err error

	if cfg.HTTPProxy != "" {
		httpProxyURL, err = url.Parse(cfg.HTTPProxy)
		if err != nil {
			log.Printf("warning: invalid HTTP proxy URL '%s': %v\n", cfg.HTTPProxy, err)
		}
	}

	if cfg.HTTPSProxy != "" {
		httpsProxyURL, err = url.Parse(cfg.HTTPSProxy)
		if err != nil {
			log.Printf("warning: invalid HTTPS proxy URL '%s': %v\n", cfg.HTTPSProxy, err)
		}
	}

	if httpProxyURL == nil && httpsProxyURL == nil {
		return
	}

	noProxyList := parseNoProxyList(cfg.NoProxy)

	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if shouldBypassProxy(req.URL.Hostname(), noProxyList) {
			return nil, nil
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

func parseNoProxyList(noProxy string) []string {
	if noProxy == "" {
		return nil
	}

	list := strings.Split(noProxy, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list
}

func shouldBypassProxy(host string, noProxyList []string) bool {
	for _, p := range noProxyList {
		if p == "" {
			continue
		}
		if p == host || (strings.HasPrefix(p, ".") && strings.HasSuffix(host, p)) {
			return true
		}
	}
	return false
}

type EdgeProxyClient struct {
	client   *http.Client
	proxyURL string
}

func NewEdgeProxyClient(proxyURL string) *EdgeProxyClient {
	return &EdgeProxyClient{
		client: &http.Client{
			Transport: createBaseTransport(),
			Timeout:   60 * time.Second,
		},
		proxyURL: proxyURL,
	}
}

func (c *EdgeProxyClient) Do(req *http.Request) (*http.Response, error) {
	if c.proxyURL == "" {
		return nil, fmt.Errorf("proxy URL is not set")
	}

	targetURL := req.URL.String()
	encodedURL := url.QueryEscape(targetURL)
	proxyURLWithParam := c.proxyURL + "?url=" + encodedURL

	bodyBytes, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}

	proxyReq, err := http.NewRequest(
		req.Method,
		proxyURLWithParam,
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating proxy request: %w", err)
	}

	copyHeaders(req.Header, proxyReq.Header)

	proxyResp, err := c.client.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer proxyResp.Body.Close()

	return parseProxyResponse(proxyResp, req)
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading request body: %w", err)
	}

	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	return bodyBytes, nil
}

func copyHeaders(source, destination http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func parseProxyResponse(proxyResp *http.Response, originalReq *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(proxyResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading proxy response: %w", err)
	}

	var response models.ProxyResponse
	if err := sonic.ConfigFastest.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error parsing proxy response: %w", err)
	}

	resp := &http.Response{
		StatusCode: response.StatusCode,
		Status:     fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode)),
		Body:       io.NopCloser(bytes.NewBufferString(response.Text)),
		Header:     make(http.Header),
		Request:    originalReq,
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
