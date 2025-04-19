package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"govd/models"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var (
	edgeProxyClient     *EdgeProxyClient
	edgeProxyClientOnce sync.Once
)

type EdgeProxyClient struct {
	*http.Client
}

func GetEdgeProxyClient() *EdgeProxyClient {
	edgeProxyClientOnce.Do(func() {
		edgeProxyClient = &EdgeProxyClient{
			Client: &http.Client{
				Transport: baseTransport,
				Timeout:   60 * time.Second,
			},
		}
	})
	return edgeProxyClient
}

func (c *EdgeProxyClient) Do(req *http.Request) (*http.Response, error) {
	proxyURL := os.Getenv("EDGE_PROXY_URL")
	if proxyURL == "" {
		return nil, fmt.Errorf("EDGE_PROXY_URL environment variable is not set")
	}
	targetURL := req.URL.String()
	encodedURL := url.QueryEscape(targetURL)
	proxyURLWithParam := proxyURL + "?url=" + encodedURL

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
