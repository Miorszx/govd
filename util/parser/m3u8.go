package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/govdbot/govd/enums"
	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util"

	"github.com/grafov/m3u8"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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

func ParseM3U8Content(content []byte, baseURL string, cookies []*http.Cookie) ([]*models.MediaFormat, error) {
	return ParseM3U8ContentWithContext(
		context.Background(),
		content,
		baseURL,
		cookies,
		DefaultParseOptions(),
	)
}

func ParseM3U8ContentWithContext(
	ctx context.Context,
	content []byte,
	baseURL string,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	baseURLObj, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}

	buf := bytes.NewBuffer(content)
	playlist, listType, err := m3u8.DecodeFrom(buf, false)
	if err != nil {
		return nil, fmt.Errorf("failed parsing M3U8: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	switch listType {
	case m3u8.MASTER:
		zap.S().Debug("detected master playlist")
		master, ok := playlist.(*m3u8.MasterPlaylist)
		if !ok {
			return nil, errors.New("failed to cast to master playlist")
		}
		return parseMasterPlaylistWithContext(timeoutCtx, master, baseURLObj, cookies, opts)
	case m3u8.MEDIA:
		zap.S().Debug("detected media playlist")
		media, ok := playlist.(*m3u8.MediaPlaylist)
		if !ok {
			return nil, errors.New("failed to cast to media playlist")
		}
		return parseMediaPlaylistWithContext(timeoutCtx, media, baseURLObj, cookies)
	default:
		return nil, errors.New("unsupported M3U8 playlist type")
	}
}

func parseMasterPlaylistWithContext(
	ctx context.Context,
	playlist *m3u8.MasterPlaylist,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	if len(playlist.Variants) == 0 {
		return nil, errors.New("no variants found in master playlist")
	}

	estimatedFormats := len(playlist.Variants) + countAlternatives(playlist.Variants)
	formats := make([]*models.MediaFormat, 0, estimatedFormats)

	altFormats := processAlternatives(ctx, playlist.Variants, baseURL, cookies, opts)
	formats = append(formats, altFormats...)

	variantFormats, err := processVariants(ctx, playlist.Variants, baseURL, cookies, opts)
	if err != nil {
		return nil, fmt.Errorf("failed processing variants: %w", err)
	}
	formats = append(formats, variantFormats...)

	return formats, nil
}

// handles alternative audio streams
func processAlternatives(
	ctx context.Context,
	variants []*m3u8.Variant,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) []*models.MediaFormat {
	seenAlternatives := make(map[string]bool)
	var formats []*models.MediaFormat

	for _, variant := range variants {
		if variant == nil {
			continue
		}
		for _, alt := range variant.Alternatives {
			if alt == nil || alt.GroupId == "" || seenAlternatives[alt.GroupId] {
				continue
			}
			seenAlternatives[alt.GroupId] = true

			if format := parseAlternativeWithContext(ctx, variants, alt, baseURL, cookies, opts); format != nil {
				formats = append(formats, format)
			}
		}
	}
	return formats
}

// handles video/audio variants
func processVariants(
	ctx context.Context,
	variants []*m3u8.Variant,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	if !opts.EnableConcurrentFetch || len(variants) <= 1 {
		return processVariantsSequential(ctx, variants, baseURL, cookies, opts)
	}
	return processVariantsConcurrent(ctx, variants, baseURL, cookies, opts)
}

// processes variants one by one
func processVariantsSequential(
	ctx context.Context,
	variants []*m3u8.Variant,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	formats := make([]*models.MediaFormat, 0, len(variants))

	for _, variant := range variants {
		if variant == nil || variant.URI == "" {
			continue
		}

		format, err := processVariant(ctx, variant, baseURL, cookies, opts)
		if err != nil {
			zap.S().Warnf("skipping variant due to: %v", err)
			continue
		}
		if format != nil {
			formats = append(formats, format)
		}
	}
	return formats, nil
}

// processes variants concurrently
func processVariantsConcurrent(
	ctx context.Context,
	variants []*m3u8.Variant,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	validVariants := filterValidVariants(variants)
	if len(validVariants) == 0 {
		return nil, nil
	}

	// use buffered channel to limit concurrency
	semaphore := make(chan struct{}, opts.MaxConcurrency)
	results := make(chan *models.MediaFormat, len(validVariants))
	errors := make(chan error, len(validVariants))

	var wg sync.WaitGroup

	for _, variant := range validVariants {
		wg.Add(1)
		go func(v *m3u8.Variant) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			format, err := processVariant(ctx, v, baseURL, cookies, opts)
			if err != nil {
				errors <- err
				return
			}
			if format != nil {
				results <- format
			}
		}(variant)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	var formats []*models.MediaFormat
	var errs []error

	for {
		select {
		case format, ok := <-results:
			if !ok {
				results = nil
			} else {
				formats = append(formats, format)
			}
		case err, ok := <-errors:
			if !ok {
				errors = nil
			} else {
				errs = append(errs, err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		if results == nil && errors == nil {
			break
		}
	}

	for _, err := range errs {
		zap.S().Warnf("variant processing error: %v", err)
	}

	return formats, nil
}

// handles a single variant
func processVariant(
	ctx context.Context,
	variant *m3u8.Variant,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) (*models.MediaFormat, error) {
	width, height := parseResolution(variant.Resolution)
	mediaType, videoCodec, audioCodec := parseVariantType(variant)
	variantURL := resolveURL(baseURL, variant.URI)

	// clear audio codec if separate audio stream exists
	if variant.Audio != "" {
		audioCodec = ""
	}

	format := &models.MediaFormat{
		FormatID:   fmt.Sprintf("hls-%d", variant.Bandwidth/1000),
		Type:       mediaType,
		VideoCodec: videoCodec,
		AudioCodec: audioCodec,
		Bitrate:    int64(variant.Bandwidth),
		Width:      width,
		Height:     height,
		URL:        []string{variantURL},
	}

	variantContent, err := fetchContentWithContext(ctx, variantURL, cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch variant content: %w", err)
	}

	// avoid infinite recursion
	recursiveOpts := &ParseOptions{
		EnableConcurrentFetch: false,
		MaxConcurrency:        1,
		Timeout:               opts.Timeout,
	}

	variantFormats, err := ParseM3U8ContentWithContext(
		ctx, variantContent, variantURL,
		cookies, recursiveOpts,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse variant content: %w", err)
	}

	if len(variantFormats) > 0 {
		enrichFormatFromVariant(format, variantFormats[0])
	}

	return format, nil
}

// handles media playlist parsing
func parseMediaPlaylistWithContext(
	ctx context.Context,
	playlist *m3u8.MediaPlaylist,
	baseURL *url.URL,
	cookies []*http.Cookie,
) ([]*models.MediaFormat, error) {
	segments, initSegment, totalDuration := extractSegments(playlist, baseURL)

	format := &models.MediaFormat{
		FormatID:    "hls",
		Duration:    int64(totalDuration),
		URL:         []string{baseURL.String()},
		Segments:    segments,
		InitSegment: initSegment,
	}

	// handle encryption if present
	if err := handleEncryption(ctx, playlist, baseURL, cookies, format); err != nil {
		return nil, err
	}

	return []*models.MediaFormat{format}, nil
}

// handles alternative audio streams
func parseAlternativeWithContext(
	ctx context.Context,
	variants []*m3u8.Variant,
	alternative *m3u8.Alternative,
	baseURL *url.URL,
	cookies []*http.Cookie,
	opts *ParseOptions,
) *models.MediaFormat {
	if alternative == nil || alternative.URI == "" || alternative.Type != "AUDIO" {
		return nil
	}

	altURL := resolveURL(baseURL, alternative.URI)
	audioCodec := getAudioAlternativeCodec(variants, alternative)

	format := &models.MediaFormat{
		FormatID:   "hls-" + alternative.GroupId,
		Type:       enums.MediaTypeAudio,
		AudioCodec: audioCodec,
		URL:        []string{altURL},
	}

	altContent, err := fetchContentWithContext(ctx, altURL, cookies)
	if err != nil {
		zap.S().Warnf("failed to fetch alternative content: %v", err)
		return nil
	}

	// avoid infinite recursion
	recursiveOpts := &ParseOptions{
		EnableConcurrentFetch: false,
		MaxConcurrency:        1,
		Timeout:               opts.Timeout,
	}

	altFormats, err := ParseM3U8ContentWithContext(
		ctx, altContent, altURL,
		cookies, recursiveOpts,
	)
	if err != nil {
		zap.S().Warnf("failed to parse alternative content: %v", err)
		return nil
	}

	if len(altFormats) > 0 {
		enrichFormatFromVariant(format, altFormats[0])
	}

	return format
}

func countAlternatives(variants []*m3u8.Variant) int {
	seen := make(map[string]bool)
	count := 0
	for _, variant := range variants {
		if variant == nil {
			continue
		}
		for _, alt := range variant.Alternatives {
			if alt != nil && !seen[alt.GroupId] {
				seen[alt.GroupId] = true
				count++
			}
		}
	}
	return count
}

func filterValidVariants(variants []*m3u8.Variant) []*m3u8.Variant {
	valid := make([]*m3u8.Variant, 0, len(variants))
	for _, variant := range variants {
		if variant != nil && variant.URI != "" {
			valid = append(valid, variant)
		}
	}
	return valid
}

func parseResolution(resolution string) (int64, int64) {
	if resolution == "" {
		return 0, 0
	}
	parts := strings.Split(resolution, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	width, _ := strconv.ParseInt(parts[0], 10, 64)
	height, _ := strconv.ParseInt(parts[1], 10, 64)
	return width, height
}

func extractSegments(playlist *m3u8.MediaPlaylist, baseURL *url.URL) ([]string, string, float64) {
	segments := make([]string, 0, len(playlist.Segments))
	var totalDuration float64
	var initSegment string

	// handle initialization segment separately
	if playlist.Map != nil && playlist.Map.URI != "" {
		initSegment = resolveURL(baseURL, playlist.Map.URI)
	}

	// add only media segments
	for _, segment := range playlist.Segments {
		if segment == nil || segment.URI == "" {
			continue
		}

		segmentURL := resolveURL(baseURL, segment.URI)
		segments = append(segments, segmentURL)
		totalDuration += segment.Duration

		// skip byte-range segments (not supported)
		if segment.Limit > 0 {
			break
		}
	}
	return segments, initSegment, totalDuration
}

func handleEncryption(
	ctx context.Context,
	playlist *m3u8.MediaPlaylist,
	baseURL *url.URL,
	cookies []*http.Cookie,
	format *models.MediaFormat,
) error {
	if playlist.Key == nil || playlist.Key.URI == "" {
		return nil
	}

	keyURL := resolveURL(baseURL, playlist.Key.URI)
	key, err := fetchContentWithContext(ctx, keyURL, cookies)
	if err != nil {
		return fmt.Errorf("failed to fetch encryption key: %w", err)
	}

	iv, err := util.ParseHexIV(playlist.Key.IV)
	if err != nil {
		return fmt.Errorf("invalid initialization vector: %w", err)
	}

	format.DecryptionKey = &models.DecryptionKey{
		Method:        playlist.Key.Method,
		Key:           key,
		IV:            iv,
		MediaSequence: int(playlist.SeqNo),
	}

	return nil
}

func enrichFormatFromVariant(dst, src *models.MediaFormat) {
	if src.Segments != nil {
		dst.Segments = src.Segments
	}
	if src.InitSegment != "" {
		dst.InitSegment = src.InitSegment
	}
	if src.Duration > 0 {
		dst.Duration = src.Duration
	}
	if src.DecryptionKey != nil {
		dst.DecryptionKey = src.DecryptionKey
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

func getAudioAlternativeCodec(variants []*m3u8.Variant, alt *m3u8.Alternative) enums.MediaCodec {
	if alt == nil || alt.URI == "" || alt.Type != "AUDIO" {
		return ""
	}

	for _, variant := range variants {
		if variant == nil || variant.URI == "" || variant.Audio != alt.GroupId {
			continue
		}
		if audioCodec := getAudioCodec(variant.Codecs); audioCodec != "" {
			return audioCodec
		}
	}
	return ""
}

func ParseM3U8FromURL(url string, cookies []*http.Cookie) ([]*models.MediaFormat, error) {
	body, err := fetchContent(url, cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch M3U8 content: %w", err)
	}
	return ParseM3U8Content(body, url, cookies)
}

// Added this for backward compatibility
func ParseM3U8ContentWithOptions(
	content []byte,
	baseURL string,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	return ParseM3U8ContentWithContext(context.Background(), content, baseURL, cookies, opts)
}

func parseVariantType(variant *m3u8.Variant) (enums.MediaType, enums.MediaCodec, enums.MediaCodec) {
	videoCodec := getVideoCodec(variant.Codecs)
	audioCodec := getAudioCodec(variant.Codecs)

	var mediaType enums.MediaType
	switch {
	case videoCodec != "":
		mediaType = enums.MediaTypeVideo
	case audioCodec != "":
		mediaType = enums.MediaTypeAudio
	}

	return mediaType, videoCodec, audioCodec
}

func getVideoCodec(codecs string) enums.MediaCodec {
	codecs = strings.ToLower(codecs)
	switch {
	case strings.Contains(codecs, "avc") || strings.Contains(codecs, "h264"):
		return enums.MediaCodecAVC
	case strings.Contains(codecs, "hvc") || strings.Contains(codecs, "h265"):
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

func resolveURL(base *url.URL, uri string) string {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return uri
	}
	ref, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return base.ResolveReference(ref).String()
}
