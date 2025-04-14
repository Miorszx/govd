package core

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"govd/enums"
	"govd/models"
	"govd/util"
)

func downloadMediaItem(
	ctx context.Context,
	media *models.Media,
	config *models.DownloadConfig,
	idx int,
) (*models.DownloadedMedia, error) {
	if config == nil {
		config = util.DefaultConfig()
	}

	format := media.Format
	if format == nil {
		return nil, fmt.Errorf("media format is nil")
	}

	fileName := format.GetFileName()
	var filePath string
	var thumbnailFilePath string

	if format.Type != enums.MediaTypePhoto {
		if len(format.Segments) == 0 {
			path, err := util.DownloadFile(
				ctx, format.URL,
				fileName, config,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to download file: %w", err)
			}
			filePath = path
		} else {
			path, err := util.DownloadFileWithSegments(
				ctx, format.Segments,
				fileName, config,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to download segments: %w", err)
			}
			filePath = path
		}

		if format.Type == enums.MediaTypeVideo || format.Type == enums.MediaTypeAudio {
			path, err := getFileThumbnail(format, filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to get thumbnail: %w", err)
			}
			thumbnailFilePath = path
		}

		if format.Type == enums.MediaTypeVideo {
			if format.Width == 0 || format.Height == 0 || format.Duration == 0 {
				insertVideoInfo(format, filePath)
			}
		}
	} else {
		file, err := util.DownloadFileInMemory(ctx, format.URL, config)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
		path := filepath.Join(config.DownloadDir, fileName)
		if err := util.ImgToJPEG(file, path); err != nil {
			return nil, fmt.Errorf("failed to convert image: %w", err)
		}
		filePath = path
	}

	return &models.DownloadedMedia{
		FilePath:          filePath,
		ThumbnailFilePath: thumbnailFilePath,
		Media:             media,
		Index:             idx,
	}, nil
}

func StartDownloadTask(
	media *models.Media,
	idx int,
	config *models.DownloadConfig,
) (*models.DownloadedMedia, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	return downloadMediaItem(ctx, media, config, idx)
}

func StartConcurrentDownload(
	media *models.Media,
	resultsChan chan<- models.DownloadedMedia,
	config *models.DownloadConfig,
	errChan chan<- error,
	wg *sync.WaitGroup,
	idx int,
) {
	defer wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := downloadMediaItem(ctx, media, config, idx)
	if err != nil {
		errChan <- err
		return
	}

	resultsChan <- *result
}

func DownloadMedia(
	media *models.Media,
	config *models.DownloadConfig,
) (*models.DownloadedMedia, error) {
	return StartDownloadTask(media, 0, config)
}

func DownloadMedias(
	medias []*models.Media,
	config *models.DownloadConfig,
) ([]*models.DownloadedMedia, error) {
	if len(medias) == 0 {
		return []*models.DownloadedMedia{}, nil
	}

	if len(medias) == 1 {
		result, err := DownloadMedia(medias[0], config)
		if err != nil {
			return nil, err
		}
		return []*models.DownloadedMedia{result}, nil
	}

	resultsChan := make(chan models.DownloadedMedia, len(medias))
	errChan := make(chan error, len(medias))
	var wg sync.WaitGroup

	for idx, media := range medias {
		wg.Add(1)
		go StartConcurrentDownload(media, resultsChan, config, errChan, &wg, idx)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
		close(errChan)
	}()

	var results []*models.DownloadedMedia
	var firstError error

	select {
	case err := <-errChan:
		if err != nil {
			firstError = err
		}
	default:
		// no errors (yet)
	}

	for result := range resultsChan {
		resultCopy := result // create a copy to avoid pointer issues
		results = append(results, &resultCopy)
	}

	if firstError != nil {
		return results, firstError
	}

	if len(results) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].Index < results[j].Index
		})
	}

	return results, nil
}
