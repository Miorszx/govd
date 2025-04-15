package models

import "time"

type DownloadConfig struct {
	ChunkSize       int           // size of each chunk in bytes
	Concurrency     int           // maximum number of concurrent downloads
	Timeout         time.Duration // timeout for individual HTTP requests
	DownloadDir     string        // directory to save downloaded files
	RetryAttempts   int           // number of retry attempts per chunk
	RetryDelay      time.Duration // delay between retries
	Remux           bool          // whether to remux the downloaded file with ffmpeg
	ProgressUpdater func(float64) // optional function to report download progress
	MaxInMemory     int           // maximum file size for in-memory downloads
}
