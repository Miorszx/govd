package ext

import (
	"fmt"
	"govd/models"
)

var maxRedirects = 5

func CtxByURL(url string) (*models.DownloadContext, error) {
	var redirectCount int

	currentURL := url

	for redirectCount <= maxRedirects {
		for _, extractor := range List {
			matches := extractor.URLPattern.FindStringSubmatch(currentURL)
			if matches == nil {
				continue
			}

			groupNames := extractor.URLPattern.SubexpNames()
			if len(matches) == 0 {
				continue
			}

			groups := make(map[string]string)
			for i, name := range groupNames {
				if name != "" {
					groups[name] = matches[i]
				}
			}
			groups["match"] = matches[0]

			ctx := &models.DownloadContext{
				MatchedContentID:  groups["id"],
				MatchedContentURL: groups["match"],
				MatchedGroups:     groups,
				Extractor:         extractor,
			}

			if !extractor.IsRedirect {
				return ctx, nil
			}

			response, err := extractor.Run(ctx)
			if err != nil {
				return nil, err
			}
			if response.URL == "" {
				return nil, fmt.Errorf("no URL found in response")
			}

			currentURL = response.URL
			redirectCount++

			break
		}

		if redirectCount > maxRedirects {
			return nil, fmt.Errorf("exceeded maximum number of redirects (%d)", maxRedirects)
		}
	}
	return nil, nil
}

func ByCodeName(codeName string) *models.Extractor {
	for _, extractor := range List {
		if extractor.CodeName == codeName {
			return extractor
		}
	}
	return nil
}
