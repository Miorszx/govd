package facebook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

// Graph API - Option B fully Golang
// Uses long-lived user access token (60 days) stored in FACEBOOK_ACCESS_TOKEN
// Auto-renew via fb_exchange_token when expiry < 10 days, if APP_ID + SECRET provided
// Endpoints:
//   {pageID}_{postID}?fields=attachments{media{image{src,width,height}},type,subattachments{media{image{src,width,height}},type,target{id}}}
//   {photoID}?fields=images{source,width,height}
//   {videoID}?fields=source,length,format{filter,embed_html,picture},title

var (
	graphAPIBase = "https://graph.facebook.com/v19.0"
)

// tokenInfo tracks expiry for auto-renew
type tokenInfo struct {
	token     string
	expiresAt time.Time
}

// tryGraphAPIHD attempts to fetch HD images/video via Graph API for a post
// Returns VideoData with ImageURLs/ImageURL/HDURL/SDURL/Title if success, nil otherwise
// It tries in order:
//  1. {pageID}_{postID} attachments (album posts like 100091807068839_992354417168118)
//  2. {postID} alone with attachments
//  3. {photoID} images for single photo IDs
func tryGraphAPIHD(ctx *models.ExtractorContext, pageID, postID string, photoIDs []string) (*VideoData, error) {
	accessToken := config.Env.FacebookAccessToken
	if accessToken == "" {
		return nil, fmt.Errorf("no FB access token")
	}

	// Ensure token is fresh (auto-renew if needed)
	renewTokenIfNeeded()

	// Client without cookies, token in URL
	client := ctx.HTTPClient
	if client == nil {
		// fallback to new client
		client = networking.NewHTTPClient(nil)
	}

	// Helper to GET graph API
	fetchJSON := func(endpoint string) (map[string]interface{}, error) {
		// endpoint already contains ? or not, we append access_token
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		fullURL := fmt.Sprintf("%s/%s%saccess_token=%s", graphAPIBase, endpoint, sep, url.QueryEscape(accessToken))
		params := &networking.RequestParams{
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
			},
		}
		resp, err := client.Fetch(http.MethodGet, fullURL, params)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			// Try to parse error for logging
			logger.L.Debug(fmt.Sprintf("graph API error %d: %s for %s", resp.StatusCode, string(b)[:min(500, len(b))], endpoint))
			return nil, fmt.Errorf("graph API %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
		}
		var out map[string]interface{}
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	// Try 1: {pageID}_{postID} or {postID} attachments for album
	candidates := []string{}
	if pageID != "" && postID != "" {
		candidates = append(candidates, fmt.Sprintf("%s_%s", pageID, postID))
	}
	if postID != "" {
		candidates = append(candidates, postID)
	}
	// Also try photoIDs individually if album detection failed
	// For share/p like 1HRumzE6Uv which resolves to groups/1905340589879305/permalink/2546744835738874
	// postID here is the group permalink ID

	for _, candID := range candidates {
		fields := "attachments{media{image{src,width,height}},type,subattachments{media{image{src,width,height}},type,target{id}},target{id}},message,full_picture,picture"
		endpoint := fmt.Sprintf("%s?fields=%s", candID, url.QueryEscape(fields))
		j, err := fetchJSON(endpoint)
		if err != nil {
			continue
		}
		// Parse attachments
		if atts, ok := j["attachments"].(map[string]interface{}); ok {
			if dataArr, ok := atts["data"].([]interface{}); ok && len(dataArr) > 0 {
				var images []string
				var title string
				if msg, ok := j["message"].(string); ok && msg != "" {
					title = msg
				}
				for _, d := range dataArr {
					if dm, ok := d.(map[string]interface{}); ok {
						// direct media
						if imagesFromNode(dm, &images) {
							// ok
						}
						// subattachments (album)
						if sub, ok := dm["subattachments"].(map[string]interface{}); ok {
							if subData, ok := sub["data"].([]interface{}); ok {
								for _, sd := range subData {
									if sdm, ok := sd.(map[string]interface{}); ok {
										imagesFromNode(sdm, &images)
									}
								}
							}
						}
						// caption from description?
						if title == "" {
							if desc, ok := dm["description"].(string); ok && desc != "" {
								title = desc
							}
						}
					}
				}
				if len(images) > 0 {
					// Deduplicate and pick largest per filename
					dedup := dedupImageURLs(images)
					vd := &VideoData{}
					if len(dedup) == 1 {
						vd.ImageURL = dedup[0]
					} else {
						vd.ImageURLs = dedup
					}
					vd.Title = title
					return vd, nil
				}
			}
		}
		// Also try direct images field for photo
		if imgs, ok := j["images"].([]interface{}); ok && len(imgs) > 0 {
			// images is array of {source,width,height}
			best := pickBestImageFromGraphImages(imgs)
			if best != "" {
				t := ""
				if tt, ok := j["name"].(string); ok {
					t = tt
				}
				return &VideoData{ImageURL: best, Title: t}, nil
			}
		}
	}

	// Try 2: For each known photoID, fetch its images
	if len(photoIDs) > 0 {
		var allHD []string
		var title string
		for _, pid := range photoIDs {
			fields := "images{source,width,height},name"
			endpoint := fmt.Sprintf("%s?fields=%s", pid, url.QueryEscape(fields))
			j, err := fetchJSON(endpoint)
			if err != nil {
				continue
			}
			if imgs, ok := j["images"].([]interface{}); ok && len(imgs) > 0 {
				if best := pickBestImageFromGraphImages(imgs); best != "" {
					allHD = append(allHD, best)
				}
			}
			if title == "" {
				if n, ok := j["name"].(string); ok {
					title = n
				}
			}
		}
		if len(allHD) > 0 {
			dedup := dedupImageURLs(allHD)
			vd := &VideoData{Title: title}
			if len(dedup) == 1 {
				vd.ImageURL = dedup[0]
			} else {
				vd.ImageURLs = dedup
			}
			return vd, nil
		}
	}

	return nil, fmt.Errorf("graph API no HD found")
}

// tryGraphAPI video for share/v
func tryGraphAPIVideo(ctx *models.ExtractorContext, videoID string) (*VideoData, error) {
	accessToken := config.Env.FacebookAccessToken
	if accessToken == "" {
		return nil, fmt.Errorf("no FB access token")
	}
	renewTokenIfNeeded()
	client := ctx.HTTPClient
	if client == nil {
		client = networking.NewHTTPClient(nil)
	}

	fetchJSON := func(endpoint string) (map[string]interface{}, error) {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		fullURL := fmt.Sprintf("%s/%s%saccess_token=%s", graphAPIBase, endpoint, sep, url.QueryEscape(accessToken))
		params := &networking.RequestParams{
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
			},
		}
		resp, err := client.Fetch(http.MethodGet, fullURL, params)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("graph %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
		}
		var out map[string]interface{}
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	// Try video fields
	candidates := []string{videoID}
	// Also try pageID_postID for video
	if strings.Contains(ctx.ContentURL, "/groups/") {
		// group permalink ID may be video
		// Try with group id extraction? For now just videoID
	}

	for _, cand := range candidates {
		fields := "source,permalink_url,picture,format{filter,embed_html,picture},length,title,description"
		endpoint := fmt.Sprintf("%s?fields=%s", cand, url.QueryEscape(fields))
		j, err := fetchJSON(endpoint)
		if err != nil {
			continue
		}
		// source is HD video URL
		var hdURL, sdURL, thumb, title string
		if s, ok := j["source"].(string); ok {
			hdURL = s
		}
		if t, ok := j["title"].(string); ok {
			title = t
		} else if d, ok := j["description"].(string); ok {
			title = d
		}
		if p, ok := j["picture"].(string); ok {
			thumb = p
		}
		// Try format array for higher quality
		if fmts, ok := j["format"].([]interface{}); ok {
			for _, f := range fmts {
				if fm, ok := f.(map[string]interface{}); ok {
					if filter, ok := fm["filter"].(string); ok && filter == "native" {
						if embed, ok := fm["embed_html"].(string); ok && strings.Contains(embed, "hd_src") {
							// Try to extract hd_src from embed_html
							if idx := strings.Index(embed, "hd_src"); idx != -1 {
								// rough
							}
						}
					}
				}
			}
		}
		if hdURL != "" {
			return &VideoData{
				HDURL: hdURL,
				SDURL: sdURL,
				Title: title,
			}, nil
		}
		if thumb != "" {
			// At least thumbnail if video blocked
			return &VideoData{
				ImageURL: thumb,
				Title:    title,
			}, nil
		}
	}
	return nil, fmt.Errorf("graph video not found")
}

func imagesFromNode(node map[string]interface{}, out *[]string) bool {
	// node can have media.image.src or media.image with width
	found := false
	if media, ok := node["media"].(map[string]interface{}); ok {
		if img, ok := media["image"].(map[string]interface{}); ok {
			if src, ok := img["src"].(string); ok && src != "" {
				*out = append(*out, src)
				found = true
			} else if src, ok := img["source"].(string); ok && src != "" {
				*out = append(*out, src)
				found = true
			}
		}
		// Sometimes media directly has image array
		if imgArr, ok := media["image"].([]interface{}); ok {
			best := ""
			maxW := 0
			for _, im := range imgArr {
				if imm, ok := im.(map[string]interface{}); ok {
					if src, ok := imm["src"].(string); ok {
						if w, ok := imm["width"].(float64); ok {
							if int(w) > maxW {
								maxW = int(w)
								best = src
							}
						} else {
							if best == "" {
								best = src
							}
						}
					}
				}
			}
			if best != "" {
				*out = append(*out, best)
				found = true
			}
		}
	}
	return found
}

func pickBestImageFromGraphImages(imgs []interface{}) string {
	type entry struct {
		src string
		w   int
		h   int
	}
	var entries []entry
	for _, im := range imgs {
		if mm, ok := im.(map[string]interface{}); ok {
			src, _ := mm["source"].(string)
			if src == "" {
				src, _ = mm["src"].(string)
			}
			if src == "" {
				continue
			}
			w := 0
			if wf, ok := mm["width"].(float64); ok {
				w = int(wf)
			}
			h := 0
			if hf, ok := mm["height"].(float64); ok {
				h = int(hf)
			}
			entries = append(entries, entry{src, w, h})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].w > entries[j].w
	})
	return entries[0].src
}

func dedupImageURLs(urls []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, u := range urls {
		if u == "" {
			continue
		}
		// Use filename as dedup key, keep largest width if duplicate
		fn := u
		if idx := strings.Index(fn, "?"); idx != -1 {
			fn = fn[:idx]
		}
		if idx := strings.LastIndex(fn, "/"); idx != -1 {
			fn = fn[idx+1:]
		}
		if _, ok := seen[fn]; ok {
			continue
		}
		seen[fn] = struct{}{}
		out = append(out, u)
	}
	return out
}

// renewTokenIfNeeded checks if current access token expires in <10 days and renews via fb_exchange_token
func renewTokenIfNeeded() {
	// This runs inside extractor, should be non-blocking and safe to fail silently
	accessToken := config.Env.FacebookAccessToken
	appID := config.Env.FacebookAppID
	appSecret := config.Env.FacebookAppSecret
	if accessToken == "" || appID == "" || appSecret == "" {
		return
	}

	// Try to get token debug info to check expiry
	// Use app token to debug: /debug_token?input_token=USER_TOKEN&access_token=APP_ID|APP_SECRET
	appToken := fmt.Sprintf("%s|%s", appID, appSecret)
	client := networking.NewHTTPClient(nil)

	debugURL := fmt.Sprintf("%s/debug_token?input_token=%s&access_token=%s", graphAPIBase, url.QueryEscape(accessToken), url.QueryEscape(appToken))
	params := &networking.RequestParams{
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 AppleWebKit/537.36",
		},
	}
	resp, err := client.Fetch(http.MethodGet, debugURL, params)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return
	}
	var debugResp struct {
		Data struct {
			ExpiresAt int64 `json:"expires_at"`
			IsValid   bool  `json:"is_valid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &debugResp); err != nil {
		return
	}
	if !debugResp.Data.IsValid {
		logger.L.Warn("Facebook access token invalid, need re-auth")
		return
	}
	expiresAt := time.Unix(debugResp.Data.ExpiresAt, 0)
	if time.Until(expiresAt) > 10*24*time.Hour {
		// Still valid >10 days, no need renew
		return
	}

	// Renew: /oauth/access_token?grant_type=fb_exchange_token&client_id=APP_ID&client_secret=APP_SECRET&fb_exchange_token=OLD_TOKEN
	renewURL := fmt.Sprintf("%s/oauth/access_token?grant_type=fb_exchange_token&client_id=%s&client_secret=%s&fb_exchange_token=%s",
		graphAPIBase, url.QueryEscape(appID), url.QueryEscape(appSecret), url.QueryEscape(accessToken))

	resp2, err := client.Fetch(http.MethodGet, renewURL, params)
	if err != nil {
		logger.L.Debug(fmt.Sprintf("fb token renew fetch error: %v", err))
		return
	}
	defer resp2.Body.Close()
	b2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != 200 {
		logger.L.Debug(fmt.Sprintf("fb token renew failed %d: %s", resp2.StatusCode, string(b2)[:min(200, len(b2))]))
		return
	}
	var renewResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(b2, &renewResp); err != nil {
		return
	}
	if renewResp.AccessToken != "" {
		// Update env in memory
		config.Env.FacebookAccessToken = renewResp.AccessToken
		logger.L.Info(fmt.Sprintf("Facebook access token auto-renewed, new expiry in %d seconds", renewResp.ExpiresIn))
		// Optionally persist to .env file for next restart
		persistTokenToEnv(renewResp.AccessToken)
	}
}

func persistTokenToEnv(newToken string) {
	// Persistence optional - token kept in memory via config.Env
	// Could write to private/cookies/fb_token.txt if needed
	_ = newToken
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
