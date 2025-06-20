package parser

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/govdbot/govd/enums"
	"github.com/govdbot/govd/models"
	"github.com/govdbot/govd/util"

	"github.com/pkg/errors"
	"github.com/unki2aut/go-mpd"
	"github.com/unki2aut/go-xsd-types"
	"go.uber.org/zap"
)

var segmentTemplateRE = regexp.MustCompile(`\$([A-Za-z]+)(?:\%0(\d+)d)?\$`)

func ParseMPDContent(content []byte, baseURL string, cookies []*http.Cookie) ([]*models.MediaFormat, error) {
	return ParseMPDContentWithContext(
		context.Background(),
		content,
		baseURL,
		cookies,
		DefaultParseOptions(),
	)
}

func ParseMPDContentWithContext(
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

	mpdDoc := &mpd.MPD{}
	if err := mpdDoc.Decode(content); err != nil {
		return nil, fmt.Errorf("failed parsing MPD: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	zap.S().Debug("detected mpd manifest")
	return parseMPDWithContext(timeoutCtx, mpdDoc, baseURLObj, opts)
}

func ParseMPDFromURL(url string, cookies []*http.Cookie) ([]*models.MediaFormat, error) {
	body, err := fetchContent(url, cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MPD content: %w", err)
	}
	return ParseMPDContent(body, url, cookies)
}

func ParseMPDContentWithOptions(
	content []byte,
	baseURL string,
	cookies []*http.Cookie,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	return ParseMPDContentWithContext(context.Background(), content, baseURL, cookies, opts)
}

func parseMPDWithContext(
	ctx context.Context,
	mpdDoc *mpd.MPD,
	baseURL *url.URL,
	opts *ParseOptions,
) ([]*models.MediaFormat, error) {
	if len(mpdDoc.Period) == 0 {
		return nil, errors.New("no periods found in mpd")
	}

	// process first period (most common case)
	period := mpdDoc.Period[0]
	if len(period.AdaptationSets) == 0 {
		return nil, errors.New("no adaptation sets found in period")
	}

	estimatedFormats := countRepresentations(period.AdaptationSets)
	formats := make([]*models.MediaFormat, 0, estimatedFormats)

	mpdBaseURL := resolveMPDBaseURL(baseURL, mpdDoc.BaseURL)
	periodBaseURL := resolvePeriodBaseURL(mpdBaseURL, period.BaseURL)

	adaptationFormats, err := processAdaptationSets(
		ctx, period.AdaptationSets,
		periodBaseURL, opts, mpdDoc,
	)
	if err != nil {
		return nil, fmt.Errorf("failed processing adaptation sets: %w", err)
	}
	formats = append(formats, adaptationFormats...)

	return formats, nil
}

func processAdaptationSets(
	ctx context.Context,
	adaptationSets []*mpd.AdaptationSet,
	baseURL *url.URL,
	opts *ParseOptions,
	mpdDoc *mpd.MPD,
) ([]*models.MediaFormat, error) {
	if !opts.EnableConcurrentFetch || len(adaptationSets) <= 1 {
		return processAdaptationSetsSequential(mpdDoc, adaptationSets, baseURL)
	}
	return processAdaptationSetsConcurrent(ctx, adaptationSets, baseURL, opts, mpdDoc)
}

func processAdaptationSetsSequential(
	mpdDoc *mpd.MPD,
	adaptationSets []*mpd.AdaptationSet,
	baseURL *url.URL,
) ([]*models.MediaFormat, error) {
	var formats []*models.MediaFormat

	for _, adaptationSet := range adaptationSets {
		if adaptationSet == nil {
			continue
		}

		setFormats, err := processAdaptationSet(adaptationSet, baseURL, mpdDoc)
		if err != nil {
			zap.S().Warnf("skipping adaptation set due to: %v", err)
			continue
		}
		formats = append(formats, setFormats...)
	}
	return formats, nil
}

func processAdaptationSetsConcurrent(
	ctx context.Context,
	adaptationSets []*mpd.AdaptationSet,
	baseURL *url.URL,
	opts *ParseOptions,
	mpdDoc *mpd.MPD,
) ([]*models.MediaFormat, error) {
	validSets := filterValidAdaptationSets(adaptationSets)
	if len(validSets) == 0 {
		return nil, nil
	}

	semaphore := make(chan struct{}, opts.MaxConcurrency)
	results := make(chan []*models.MediaFormat, len(validSets))
	errors := make(chan error, len(validSets))

	var wg sync.WaitGroup

	for _, adaptationSet := range validSets {
		wg.Add(1)
		go func(set *mpd.AdaptationSet) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			setFormats, err := processAdaptationSet(set, baseURL, mpdDoc)
			if err != nil {
				errors <- err
				return
			}
			if len(setFormats) > 0 {
				results <- setFormats
			}
		}(adaptationSet)
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
		case setFormats, ok := <-results:
			if !ok {
				results = nil
			} else {
				formats = append(formats, setFormats...)
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
		zap.S().Warnf("adaptation set processing error: %v", err)
	}

	return formats, nil
}

func processAdaptationSet(
	adaptationSet *mpd.AdaptationSet,
	baseURL *url.URL,
	mpdDoc *mpd.MPD,
) ([]*models.MediaFormat, error) {
	if len(adaptationSet.Representations) == 0 {
		return nil, nil
	}

	adaptationBaseURL := resolveAdaptationSetBaseURL(baseURL, adaptationSet.BaseURL)
	var formats []*models.MediaFormat

	for _, representation := range adaptationSet.Representations {
		if representation.ID == nil || representation.Bandwidth == nil {
			continue
		}

		format, err := processRepresentation(
			representation, adaptationSet,
			adaptationBaseURL, mpdDoc,
		)
		if err != nil {
			zap.S().Warnf("skipping representation %s due to: %v", *representation.ID, err)
			continue
		}
		if format != nil {
			formats = append(formats, format)
		}
	}

	return formats, nil
}

func processRepresentation(
	representation mpd.Representation,
	adaptationSet *mpd.AdaptationSet,
	baseURL *url.URL,
	mpdDoc *mpd.MPD,
) (*models.MediaFormat, error) {
	mediaType, videoCodec, audioCodec := parseAdaptationSetType(adaptationSet, representation)
	representationBaseURL := resolveRepresentationBaseURL(baseURL, representation.BaseURL)

	var width, height int64
	if representation.Width != nil {
		width = int64(*representation.Width)
	}
	if representation.Height != nil {
		height = int64(*representation.Height)
	}

	format := &models.MediaFormat{
		FormatID:   fmt.Sprintf("dash-%d", *representation.Bandwidth/1000),
		Type:       mediaType,
		VideoCodec: videoCodec,
		AudioCodec: audioCodec,
		Bitrate:    int64(*representation.Bandwidth),
		Width:      width,
		Height:     height,
		URL:        []string{representationBaseURL.String()},
		Duration:   getTotalDurationSeconds(mpdDoc.MediaPresentationDuration),
	}

	// process segment template if available
	segmentTemplate := getSegmentTemplate(representation, adaptationSet)
	if segmentTemplate != nil {
		segments, initSegment, err := extractSegmentsFromTemplate(
			segmentTemplate, representation, representationBaseURL, mpdDoc,
		)
		if err != nil {
			return nil, fmt.Errorf("failed extracting segments: %w", err)
		}

		format.Segments = segments
		format.InitSegment = initSegment
	}

	// handle content protection
	if err := handleContentProtection(adaptationSet, representation, format); err != nil {
		return nil, err
	}

	return format, nil
}

// segment extraction functions
func extractSegmentsFromTemplate(
	segmentTemplate *mpd.SegmentTemplate,
	representation mpd.Representation,
	baseURL *url.URL,
	mpdDoc *mpd.MPD,
) ([]string, string, error) {
	var segments []string
	var initSegment string

	// handle initialization segment
	if segmentTemplate.Initialization != nil {
		initURL := expandSegmentTemplate(*segmentTemplate.Initialization, representation, 0, 0)
		initSegment = resolveURL(baseURL, initURL)
	}

	if segmentTemplate.SegmentTimeline != nil {
		// timeline-based segments
		segments = extractTimelineSegments(segmentTemplate, representation, baseURL)
		zap.S().Debugf("extracted %d timeline segments", len(segments))
	} else if segmentTemplate.Media != nil {
		// template-based segments
		segmentCount := calculateSegmentCount(segmentTemplate, mpdDoc)
		segments = extractTemplateSegments(segmentTemplate, representation, baseURL, segmentCount)
		zap.S().Debugf("extracted %d template segments", len(segments))
	}

	return segments, initSegment, nil
}

func extractTimelineSegments(
	segmentTemplate *mpd.SegmentTemplate,
	representation mpd.Representation,
	baseURL *url.URL,
) []string {
	var segments []string

	if segmentTemplate.SegmentTimeline == nil || len(segmentTemplate.SegmentTimeline.S) == 0 {
		return segments
	}

	startNumber := uint64(1)
	if segmentTemplate.StartNumber != nil {
		startNumber = *segmentTemplate.StartNumber
	}

	segmentNumber := startNumber
	var currentTime uint64

	for _, s := range segmentTemplate.SegmentTimeline.S {
		if s.T != nil {
			currentTime = *s.T
		}

		repeatCount := int64(0)
		if s.R != nil {
			repeatCount = *s.R
		}

		for i := int64(0); i <= repeatCount; i++ {
			mediaURL := expandSegmentTemplate(*segmentTemplate.Media, representation, segmentNumber, currentTime)
			segmentURL := resolveURL(baseURL, mediaURL)
			segments = append(segments, segmentURL)

			currentTime += s.D
			segmentNumber++
		}
	}

	return segments
}

func extractTemplateSegments(
	segmentTemplate *mpd.SegmentTemplate,
	representation mpd.Representation,
	baseURL *url.URL,
	segmentCount int,
) []string {
	var segments []string

	startNumber := uint64(1)
	if segmentTemplate.StartNumber != nil {
		startNumber = *segmentTemplate.StartNumber
	}

	for i := range segmentCount {
		segmentNumber := startNumber + uint64(i)
		mediaURL := expandSegmentTemplate(*segmentTemplate.Media, representation, segmentNumber, 0)
		segmentURL := resolveURL(baseURL, mediaURL)
		segments = append(segments, segmentURL)
	}

	return segments
}

func calculateSegmentCount(segmentTemplate *mpd.SegmentTemplate, mpdDoc *mpd.MPD) int {
	totalDurationSeconds := getTotalDurationSeconds(mpdDoc.MediaPresentationDuration)

	var segmentDurationSeconds float64 = 10.0 // default 10 seconds
	if segmentTemplate.Duration != nil && segmentTemplate.Timescale != nil {
		segmentDurationSeconds = float64(*segmentTemplate.Duration) / float64(*segmentTemplate.Timescale)
		zap.S().Debugf("segment duration calculation: %d / %d = %.4f seconds",
			*segmentTemplate.Duration, *segmentTemplate.Timescale, segmentDurationSeconds)
	}
	if totalDurationSeconds > 0 && segmentDurationSeconds > 0 {
		segmentCount := int(math.Ceil(float64(totalDurationSeconds) / segmentDurationSeconds))
		zap.S().Debugf("total duration: %d seconds, segment duration: %.4f seconds, segment count: %d",
			totalDurationSeconds, segmentDurationSeconds, segmentCount)
		return segmentCount
	}

	return 1
}

func expandSegmentTemplate(template string, representation mpd.Representation, number, time uint64) string {
	result := template

	result = segmentTemplateRE.ReplaceAllStringFunc(result, func(match string) string {
		submatch := segmentTemplateRE.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}

		identifier := submatch[1]
		width := 0
		if len(submatch) > 2 && submatch[2] != "" {
			width, _ = strconv.Atoi(submatch[2])
		}

		switch identifier {
		case "RepresentationID":
			if representation.ID != nil {
				return *representation.ID
			}
		case "Number":
			if width > 0 {
				return fmt.Sprintf("%0*d", width, number)
			}
			return strconv.FormatUint(number, 10)
		case "Time":
			if width > 0 {
				return fmt.Sprintf("%0*d", width, time)
			}
			return strconv.FormatUint(time, 10)
		case "Bandwidth":
			if representation.Bandwidth != nil {
				return strconv.FormatUint(*representation.Bandwidth, 10)
			}
		}
		return match
	})

	return result
}

func handleContentProtection(
	adaptationSet *mpd.AdaptationSet,
	representation mpd.Representation,
	format *models.MediaFormat,
) error {
	protections := adaptationSet.ContentProtections
	if len(representation.ContentProtections) > 0 {
		protections = representation.ContentProtections
	}

	for _, protection := range protections {
		if protection.SchemeIDURI == nil {
			continue
		}

		scheme := strings.ToLower(*protection.SchemeIDURI)
		if strings.Contains(scheme, "cenc") || strings.Contains(scheme, "clearkey") {
			format.DecryptionKey = &models.DecryptionKey{
				Method: "AES-128",
			}

			if protection.CencDefaultKeyId != nil {
				if key, err := util.ParseHex(*protection.CencDefaultKeyId); err == nil {
					format.DecryptionKey.Key = key
				}
			}
			break
		}
	}

	return nil
}

func getTotalDurationSeconds(duration *xsd.Duration) int64 {
	if duration == nil {
		return 0
	}
	var total float64

	if duration.Hours != 0 {
		total += float64(duration.Hours) * 3600
	}
	if duration.Minutes != 0 {
		total += float64(duration.Minutes) * 60
	}
	total += float64(duration.Seconds)

	return int64(total)
}

func getSegmentTemplate(representation mpd.Representation, adaptationSet *mpd.AdaptationSet) *mpd.SegmentTemplate {
	if representation.SegmentTemplate != nil {
		return representation.SegmentTemplate
	}
	return adaptationSet.SegmentTemplate
}

func parseAdaptationSetType(adaptationSet *mpd.AdaptationSet, representation mpd.Representation) (enums.MediaType, enums.MediaCodec, enums.MediaCodec) {
	// determine codecs
	var codecs string
	if representation.Codecs != nil {
		codecs = *representation.Codecs
	} else if adaptationSet.Codecs != nil {
		codecs = *adaptationSet.Codecs
	}

	videoCodec := getVideoCodec(codecs)
	audioCodec := getAudioCodec(codecs)

	// determine media type from mime type and codecs
	mimeType := strings.ToLower(adaptationSet.MimeType)
	var mediaType enums.MediaType

	switch {
	case strings.HasPrefix(mimeType, "video/") || videoCodec != "":
		mediaType = enums.MediaTypeVideo
	case strings.HasPrefix(mimeType, "audio/") || audioCodec != "":
		mediaType = enums.MediaTypeAudio
	case adaptationSet.ContentType != nil:
		contentType := strings.ToLower(*adaptationSet.ContentType)
		if contentType == "video" {
			mediaType = enums.MediaTypeVideo
		} else if contentType == "audio" {
			mediaType = enums.MediaTypeAudio
		}
	}

	return mediaType, videoCodec, audioCodec
}

func countRepresentations(adaptationSets []*mpd.AdaptationSet) int {
	count := 0
	for _, set := range adaptationSets {
		if set != nil {
			count += len(set.Representations)
		}
	}
	return count
}

func filterValidAdaptationSets(adaptationSets []*mpd.AdaptationSet) []*mpd.AdaptationSet {
	valid := make([]*mpd.AdaptationSet, 0, len(adaptationSets))
	for _, set := range adaptationSets {
		if set != nil && len(set.Representations) > 0 {
			valid = append(valid, set)
		}
	}
	return valid
}

func resolveMPDBaseURL(baseURL *url.URL, baseURLs []*mpd.BaseURL) *url.URL {
	if len(baseURLs) > 0 && baseURLs[0] != nil && baseURLs[0].Value != "" {
		if resolved, err := url.Parse(baseURLs[0].Value); err == nil {
			return baseURL.ResolveReference(resolved)
		}
	}
	return baseURL
}

func resolvePeriodBaseURL(baseURL *url.URL, baseURLs []*mpd.BaseURL) *url.URL {
	return resolveMPDBaseURL(baseURL, baseURLs)
}

func resolveAdaptationSetBaseURL(baseURL *url.URL, baseURLs []*mpd.BaseURL) *url.URL {
	return resolveMPDBaseURL(baseURL, baseURLs)
}

func resolveRepresentationBaseURL(baseURL *url.URL, baseURLs []*mpd.BaseURL) *url.URL {
	return resolveMPDBaseURL(baseURL, baseURLs)
}
