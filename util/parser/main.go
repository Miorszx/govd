package parser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/govdbot/govd/enums"
)

const (
	defaultHTTPTimeout   = 30 * time.Second
	maxConcurrentFetches = 10
)

var (
	httpClient *http.Client
	once       sync.Once
)

// returns a singleton HTTP client with optimized settings
func getHTTPClient() *http.Client {
	once.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		}
		httpClient = &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: transport,
		}
	})
	return httpClient
}

type ParseOptions struct {
	EnableConcurrentFetch bool
	MaxConcurrency        int
	Timeout               time.Duration
}

func DefaultParseOptions() *ParseOptions {
	return &ParseOptions{
		EnableConcurrentFetch: true,
		MaxConcurrency:        maxConcurrentFetches,
		Timeout:               defaultHTTPTimeout,
	}
}

func getVideoCodec(codecs string) enums.MediaCodec {
	codecs = strings.ToLower(codecs)
	switch {
	case strings.Contains(codecs, "avc") || strings.Contains(codecs, "h264"):
		return enums.MediaCodecAVC
	case strings.Contains(codecs, "hvc") || strings.Contains(codecs, "h265") || strings.Contains(codecs, "hev1"):
		return enums.MediaCodecHEVC
	case strings.Contains(codecs, "av01"):
		return enums.MediaCodecAV1
	case strings.Contains(codecs, "vp9"):
		return enums.MediaCodecVP9
	case strings.Contains(codecs, "vp8"):
		return enums.MediaCodecVP8
	default:
		return ""
	}
}

func getAudioCodec(codecs string) enums.MediaCodec {
	codecs = strings.ToLower(codecs)
	switch {
	case strings.Contains(codecs, "mp4a"):
		return enums.MediaCodecAAC
	case strings.Contains(codecs, "opus"):
		return enums.MediaCodecOpus
	case strings.Contains(codecs, "mp3"):
		return enums.MediaCodecMP3
	case strings.Contains(codecs, "flac"):
		return enums.MediaCodecFLAC
	case strings.Contains(codecs, "vorbis"):
		return enums.MediaCodecVorbis
	default:
		return ""
	}
}

// fetches content with context support
func fetchContentWithContext(ctx context.Context, url string, cookies []*http.Cookie) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// backward-compatible function for fetching content without context
func fetchContent(url string, cookies []*http.Cookie) ([]byte, error) {
	return fetchContentWithContext(context.Background(), url, cookies)
}
