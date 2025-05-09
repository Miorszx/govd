package util

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"govd/config"
	"govd/models"

	"go.uber.org/zap"
)

var (
	defaultClient     *http.Client
	defaultClientOnce sync.Once
	extractorClients  = make(map[string]models.HTTPClient)
)

func GetDefaultHTTPClient() *http.Client {
	defaultClientOnce.Do(func() {
		defaultClient = &http.Client{
			Transport: GetBaseTransport(),
			Timeout:   60 * time.Second,
		}
	})
	return defaultClient
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
		client = NewEdgeProxyFromConfig(cfg)
	} else {
		client = NewClientFromConfig(cfg)
	}
	extractorClients[extractor] = client

	return client
}

func NewClientFromConfig(cfg *models.ExtractorConfig) *http.Client {
	var baseClient *http.Client
	if cfg.Impersonate {
		baseClient = NewChromeClient()
	} else {
		baseClient = GetDefaultHTTPClient()
	}
	transport := GetBaseTransport()
	if cfg.HTTPProxy != "" || cfg.HTTPSProxy != "" {
		configureProxyTransport(transport, cfg)
	}
	baseClient.Transport = transport
	return baseClient
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
			zap.S().Warnf("warning: invalid HTTP proxy URL '%s': %v", cfg.HTTPProxy, err)
		}
	}
	if cfg.HTTPSProxy != "" {
		httpsProxyURL, err = url.Parse(cfg.HTTPSProxy)
		if err != nil {
			zap.S().Warnf("warning: invalid HTTPS proxy URL '%s': %v", cfg.HTTPSProxy, err)
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
