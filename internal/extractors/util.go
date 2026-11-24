package extractors

import (
	"context"
	"time"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

const maxRedirects = 5

var extractorsByHost = getExtractorsMap()

func FromURL(url string) *models.ExtractorContext {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Minute,
	)

	var redirectCount int

	currentURL := url

	for redirectCount <= maxRedirects {
		host, err := util.ExtractBaseHost(currentURL)
		if err != nil {
			cancel()
			return nil
		}

		extractors := getExtractorsByHost(host)
		if len(extractors) == 0 {
			cancel()
			return nil
		}

		var extractor *models.Extractor
		var matches []string
		var groups map[string]string

		for _, e := range extractors {
			matches = e.URLPattern.FindStringSubmatch(currentURL)
			if matches != nil {
				extractor = e
				groups = util.GetNamedGroups(e.URLPattern, currentURL)
				break
			}
		}

		if extractor == nil || matches == nil {
			logger.L.Debugf("no extractor matched for URL: %s", currentURL)
			cancel()
			return nil
		}

		cfg := config.GetExtractorConfig(extractor.ID)
		if cfg.IsDisabled {
			logger.L.Debugf("[%s] extractor is disabled, skipping URL: %s", extractor.ID, currentURL)
			cancel()
			return nil
		}

		for _, r := range cfg.IgnoreRegex {
			if r.MatchString(currentURL) {
				logger.L.Debugf("[%s] URL matches ignore_regex, skipping URL: %s", extractor.ID, currentURL)
				cancel()
				return nil
			}
		}

		extractorCtx := &models.ExtractorContext{
			ContentID:    groups["id"],
			ContentURL:   groups["match"],
			MatchGroups:  groups,
			Extractor:    extractor,
			Context:      ctx,
			CancelFunc:   cancel,
			Config:       cfg,
			FilesTracker: models.NewFilesTracker(),
			HTTPClient: networking.NewHTTPClient(
				&networking.NewHTTPClientOptions{
					Cookies:       util.GetExtractorCookies(extractor.ID),
					EdgeProxy:     cfg.EdgeProxy,
					DownloadProxy: cfg.DownloadProxy,
					Proxy:         cfg.Proxy,
					DisableProxy:  cfg.DisableProxy,
					Impersonate:   cfg.Impersonate,
				},
			),
		}
		if !extractor.Redirect {
			return extractorCtx
		}
		// extractor requires fetching the URL for redirection
		extractorCtx.Debugf("following redirect")

		response, err := extractor.GetFunc(extractorCtx)
		if err != nil {
			extractorCtx.Errorf("redirect failed: %v", err)
			cancel()
			return nil
		}
		// A response that already carries Media is a finished answer, not a hop.
		// The extractor did the whole job itself (facebook share links do this:
		// the crawler path is a dead end, so the cookie-bearing resolver runs
		// inside GetFunc and returns the media directly). Returning nil here
		// would throw that work away and the bot would answer with silence.
		if response != nil && response.Media != nil {
			extractorCtx.Debugf("extractor returned media directly, no redirect")
			response.Media.ContentURL = extractorCtx.ContentURL
			response.Media.ContentID = extractorCtx.ContentID
			response.Media.ExtractorID = extractorCtx.Extractor.ID
			return extractorCtx
		}
		if response == nil || response.URL == "" {
			extractorCtx.Debugf("no suitable redirect URL found")
			cancel()
			return nil
		}
		// A redirect that resolves to the URL it started on did not happen. Treat
		// it as terminal and hand the context back: the extractor has already
		// decided the crawler path is a dead end and wants the cookie-bearing
		// GetMedia to do the work.
		//
		// Measured 2026-11-24 on facebook share/p: the external-agent redirect
		// lands on /login/?next=..., the extractor correctly refuses it, and this
		// loop then re-matched the login URL, found no extractor, and returned nil
		// -- dropping the message with no log line at all.
		if response.URL == currentURL {
			extractorCtx.Debugf("redirect resolved to same URL, treating as terminal")
			return extractorCtx
		}

		extractorCtx.Debugf("redirected to %s", response.URL)

		currentURL = response.URL
		redirectCount++

		if redirectCount > maxRedirects {
			extractorCtx.Errorf("exceeded maximum number of redirects (%d)", maxRedirects)
			cancel()
			return nil
		}
	}

	cancel()
	return nil
}

func getExtractorsMap() map[string][]*models.Extractor {
	extractorsByHost := make(map[string][]*models.Extractor)
	for _, extractor := range Extractors {
		if len(extractor.Host) == 0 {
			continue
		}
		for _, domain := range extractor.Host {
			extractorsByHost[domain] = append(extractorsByHost[domain], extractor)
		}
	}
	return extractorsByHost
}

func getExtractorsByHost(host string) []*models.Extractor {
	return extractorsByHost[host]
}
