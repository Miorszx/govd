package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"govd/models"
	"govd/util/av"

	"github.com/google/uuid"
)

var downloadHTTPSession = GetDefaultHTTPClient()

func DownloadFile(
	ctx context.Context,
	urlList []string,
	fileName string,
	config *models.DownloadConfig,
) (string, error) {
	var errs []error
	for _, fileURL := range urlList {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			if err := EnsureDownloadDir(config.DownloadDir); err != nil {
				return "", err
			}

			filePath := filepath.Join(config.DownloadDir, fileName)
			err := runChunkedDownload(ctx, fileURL, filePath, config)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if config.Remux {
				err := av.RemuxFile(filePath)
				if err != nil {
					os.Remove(filePath)
					return "", fmt.Errorf("remuxing failed: %w", err)
				}
			}
			return filePath, nil
		}
	}

	return "", fmt.Errorf("%w: %v", ErrDownloadFailed, errs)
}

func DownloadFileWithSegments(
	ctx context.Context,
	segmentURLs []string,
	fileName string,
	config *models.DownloadConfig,
) (string, error) {
	if err := EnsureDownloadDir(config.DownloadDir); err != nil {
		return "", err
	}
	tempDir := filepath.Join(
		config.DownloadDir,
		"segments"+uuid.NewString(),
	)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	downloadedFiles, err := downloadSegments(ctx, tempDir, segmentURLs, config)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to download segments: %w", err)
	}
	mergedFilePath, err := av.MergeSegments(downloadedFiles, fileName)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to merge segments: %w", err)
	}
	if err := os.RemoveAll(tempDir); err != nil {
		return "", fmt.Errorf("failed to remove temporary directory: %w", err)
	}
	return mergedFilePath, nil
}

func DownloadFileInMemory(
	ctx context.Context,
	urlList []string,
	config *models.DownloadConfig,
) (*bytes.Reader, error) {
	var errs []error
	for _, fileURL := range urlList {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			data, err := downloadInMemory(
				ctx, fileURL,
				config,
			)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			return bytes.NewReader(data), nil
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, errs)
}

func downloadInMemory(
	ctx context.Context,
	fileURL string,
	config *models.DownloadConfig,
) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(
		ctx,
		config.Timeout,
	)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// continue with the request
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range config.Cookies {
		req.AddCookie(cookie)
	}

	resp, err := downloadHTTPSession.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if resp.ContentLength > int64(config.MaxInMemory) {
		return nil, fmt.Errorf("file too large for in-memory download: %d bytes", resp.ContentLength)
	}

	// allocate a single buffer with the
	// correct size upfront to prevent reallocations
	var data []byte
	if resp.ContentLength > 0 {
		data = make([]byte, 0, resp.ContentLength)
	} else {
		// 64KB initial capacity
		data = make([]byte, 0, 64*1024)
	}

	// use a limited reader to prevent
	// exceeding memory limits even if content-length is wrong
	limitedReader := io.LimitReader(resp.Body, int64(config.MaxInMemory))

	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := limitedReader.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
	}

	return data, nil
}

func EnsureDownloadDir(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create downloads directory: %w", err)
			}
		} else {
			return fmt.Errorf("error accessing directory: %w", err)
		}
	}
	return nil
}

func runChunkedDownload(
	ctx context.Context,
	fileURL string,
	filePath string,
	config *models.DownloadConfig,
) error {
	// reduce concurrency if it's greater
	// than the number of available CPUs
	maxProcs := runtime.GOMAXPROCS(0)
	optimalConcurrency := int(math.Max(1, float64(maxProcs-1)))

	if config.Concurrency > optimalConcurrency {
		config.Concurrency = optimalConcurrency
	}

	fileSize, err := getFileSize(ctx, fileURL, config)
	if err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// pre-allocate file size if possible
	if fileSize > 0 {
		if err := file.Truncate(int64(fileSize)); err != nil {
			return fmt.Errorf("failed to allocate file space: %w", err)
		}
	}

	numChunks := 1
	if fileSize > 0 {
		numChunks = int(math.Ceil(float64(fileSize) / float64(config.ChunkSize)))
	}

	semaphore := make(chan struct{}, config.Concurrency)
	var wg sync.WaitGroup

	errChan := make(chan error, numChunks)
	var downloadErr error
	var errOnce sync.Once

	var completedChunks atomic.Int64
	var completedBytes atomic.Int64

	downloadCtx, cancelDownload := context.WithCancel(ctx)
	defer cancelDownload()

	// use a mutex to synchronize file access
	var fileMutex sync.Mutex

	for i := range numChunks {
		wg.Add(1)

		go func(chunkIndex int) {
			defer wg.Done()

			// calculate chunk bounds
			start := chunkIndex * config.ChunkSize
			end := start + config.ChunkSize - 1
			if end >= fileSize && fileSize > 0 {
				end = fileSize - 1
			}

			// respect concurrency limit
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-downloadCtx.Done():
				return
			}

			err := downloadChunkToFile(
				downloadCtx, fileURL,
				file, start, end,
				config, &fileMutex,
			)
			if err != nil {
				errOnce.Do(func() {
					downloadErr = fmt.Errorf("chunk %d: %w", chunkIndex, err)
					cancelDownload() // cancel all other downloads
					errChan <- downloadErr
				})
				return
			}

			// update progress
			chunkSize := end - start + 1
			completedChunks.Add(1)
			completedBytes.Add(int64(chunkSize))
			if fileSize > 0 {
				progress := float64(completedBytes.Load()) / float64(fileSize)
				if config.ProgressUpdater != nil {
					config.ProgressUpdater(progress)
				}
			}
		}(i)
	}

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(errChan)
		close(done)
	}()

	var multiErr []error

	select {
	case err := <-errChan:
		if err != nil {
			multiErr = append(multiErr, err)
			// collect all errors
			for e := range errChan {
				if e != nil {
					multiErr = append(multiErr, e)
				}
			}
		}
		<-done
	case <-ctx.Done():
		cancelDownload()
		<-done // wait for all goroutines to finish
		os.Remove(filePath)
		return ctx.Err()
	case <-done:
		// no errors
	}

	if len(multiErr) > 0 {
		os.Remove(filePath)
		return fmt.Errorf("multiple download errors: %v", multiErr)
	}

	return nil
}

func getFileSize(
	ctx context.Context,
	fileURL string,
	config *models.DownloadConfig,
) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, fileURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range config.Cookies {
		log.Println(cookie.Name, cookie.Value)
		req.AddCookie(cookie)
	}

	resp, err := downloadHTTPSession.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to get file size: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to get file size: status code %d", resp.StatusCode)
	}

	return int(resp.ContentLength), nil
}

func downloadChunkToFile(
	ctx context.Context,
	fileURL string,
	file *os.File,
	start int,
	end int,
	config *models.DownloadConfig,
	fileMutex *sync.Mutex,
) error {
	var lastErr error

	for attempt := 0; attempt <= config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// wait before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(config.RetryDelay):
			}
		}

		err := downloadAndWriteChunk(
			ctx, fileURL, file,
			start, end, config,
			fileMutex,
		)
		if err == nil {
			return nil
		}

		lastErr = err
	}

	return fmt.Errorf("all %d attempts failed: %w", config.RetryAttempts+1, lastErr)
}

func downloadAndWriteChunk(
	ctx context.Context,
	fileURL string,
	file *os.File,
	start int,
	end int,
	config *models.DownloadConfig,
	fileMutex *sync.Mutex,
) error {
	reqCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range config.Cookies {
		req.AddCookie(cookie)
	}

	// set the range header for partial content
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := downloadHTTPSession.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// use a fixed-size buffer for
	// copying to avoid large allocations (32KB)
	buf := make([]byte, 32*1024)

	fileMutex.Lock()
	defer fileMutex.Unlock()

	if _, err := file.Seek(int64(start), io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}

	_, err = io.CopyBuffer(file, resp.Body, buf)
	if err != nil {
		return fmt.Errorf("failed to write chunk data: %w", err)
	}

	return nil
}

func downloadFile(
	ctx context.Context,
	fileURL string,
	filePath string,
	config *models.DownloadConfig,
) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range config.Cookies {
		req.AddCookie(cookie)
	}
	resp, err := downloadHTTPSession.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// use a fixed-size buffer for
	// copying to avoid large allocations (32KB)
	buf := make([]byte, 32*1024)
	_, err = io.CopyBuffer(file, resp.Body, buf)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return filePath, nil
}

func downloadSegments(
	ctx context.Context,
	path string,
	segmentURLs []string,
	config *models.DownloadConfig,
) ([]string, error) {
	semaphore := make(chan struct{}, config.Concurrency)
	var wg sync.WaitGroup

	var firstErr atomic.Value

	downloadedFiles := make([]string, len(segmentURLs))
	defer func() {
		if firstErr.Load() != nil {
			for _, path := range downloadedFiles {
				if path != "" {
					os.Remove(path)
				}
			}
		}
	}()

	downloadCtx, cancelDownload := context.WithCancel(ctx)
	defer cancelDownload()

	for i, segmentURL := range segmentURLs {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()

			select {
			case <-downloadCtx.Done():
				return
			default:
				// continue with the download
			}

			// acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			segmentFileName := fmt.Sprintf("segment_%05d", idx)
			segmentPath := filepath.Join(path, segmentFileName)

			filePath, err := downloadFile(
				ctx, url,
				segmentPath,
				config,
			)

			if err != nil {
				if firstErr.Load() == nil {
					firstErr.Store(fmt.Errorf("failed to download segment %d: %w", idx, err))
					cancelDownload()
				}
				return
			}

			downloadedFiles[idx] = filePath
		}(i, segmentURL)
	}
	wg.Wait()

	if err := firstErr.Load(); err != nil {
		if e, ok := err.(error); ok {
			return nil, e
		}
		return nil, fmt.Errorf("unknown error: %v", err)
	}

	for i, file := range downloadedFiles {
		if file == "" {
			return nil, fmt.Errorf("segment %d was not downloaded", i)
		}
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return nil, fmt.Errorf("segment %d file does not exist: %w", i, err)
		}
	}

	return downloadedFiles, nil
}
