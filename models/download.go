package models

import (
	"maps"
	"net/http"
	"os"
	"time"
)

type DownloadConfig struct {
	ChunkSize       int               // size of each chunk in bytes
	Concurrency     int               // maximum number of concurrent downloads
	Timeout         time.Duration     // timeout for individual HTTP requests
	DownloadDir     string            // directory to save downloaded files
	RetryAttempts   int               // number of retry attempts per chunk
	RetryDelay      time.Duration     // delay between retries
	Remux           bool              // whether to remux the downloaded file with ffmpeg
	ProgressUpdater func(float64)     // optional function to report download progress
	MaxInMemory     int               // maximum file size for in-memory downloads
	Headers         map[string]string // custom HTTP headers for the request
	Cookies         []*http.Cookie    // cookies to send with the request
}

func DefaultDownloadConfig() *DownloadConfig {
	downloadsDir := os.Getenv("DOWNLOADS_DIR")
	if downloadsDir == "" {
		downloadsDir = "downloads"
	}
	return &DownloadConfig{
		ChunkSize:     10 * 1024 * 1024, // 10MB
		Concurrency:   4,
		Timeout:       30 * time.Second,
		DownloadDir:   downloadsDir,
		RetryAttempts: 3,
		RetryDelay:    2 * time.Second,
		Remux:         true,
		MaxInMemory:   50 * 1024 * 1024, // 50MB
		Headers:       make(map[string]string),
		Cookies:       make([]*http.Cookie, 0),
	}
}

// GetDownloadConfig returns a new DownloadConfig with default values merged with the provided config.
// if the provided config is nil, it returns a new config with default values.
func GetDownloadConfig(config *DownloadConfig) *DownloadConfig {
	defaultConfig := DefaultDownloadConfig()
	if config == nil {
		return defaultConfig
	}
	config.Merge(defaultConfig)
	return config
}

func (dc *DownloadConfig) Merge(other *DownloadConfig) {
	if other.ChunkSize > 0 {
		dc.ChunkSize = other.ChunkSize
	}
	if other.Concurrency > 0 {
		dc.Concurrency = other.Concurrency
	}
	if other.Timeout > 0 {
		dc.Timeout = other.Timeout
	}
	if other.DownloadDir != "" {
		dc.DownloadDir = other.DownloadDir
	}
	if other.RetryAttempts > 0 {
		dc.RetryAttempts = other.RetryAttempts
	}
	if other.RetryDelay > 0 {
		dc.RetryDelay = other.RetryDelay
	}
	if other.Remux {
		dc.Remux = other.Remux
	}
	if other.MaxInMemory > 0 {
		dc.MaxInMemory = other.MaxInMemory
	}
	maps.Copy(dc.Headers, other.Headers)
	dc.Cookies = append(dc.Cookies, other.Cookies...)
}
