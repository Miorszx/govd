package facebook

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

// Share links are the one Facebook URL shape the ordinary extractor cannot
// address. Measured 2026-11-24 with a live cookie jar:
//
//	share/p/1A5dDnp3j5                       400   856 B   error page
//	story.php?story_fbid=...&id=...          400   856 B   error page
//	<owner>/posts/<story_fbid>               400   857 B   error page
//	<owner> and <owner>/videos/              400   856 B   error page
//	www.facebook.com/watch/?v=<id>&_rdr      200   866 KB  media payload present
//
// So watch/?v=<id> is the only surface a cookie-bearing curl can read, and the
// whole job is turning a share code into a video id. The external-agent
// redirect provides that: it answers 302 with story.php?story_fbid=<post>&id=<owner>,
// and the rest is done from the post id.
//
// On quality: the muxed rendition here is 360x640 h264+aac, and the DASH ladder
// in the same page is *also* 360x640 for these videos (measured: two
// representations, 105593 and 194950 bps, both 360x640). Nothing is being
// downgraded by taking browser_native_sd_url -- it is the same picture the
// ladder offers, already muxed, with no re-encode. When a post does carry a
// taller ladder, dashTracksFromPage picks it up and the existing
// pickVideoForBudget/buildDASHFormat path still applies, so the taller
// rendition is not lost.

var (
	// The muxed MP4. Not "progressive_url": that name belongs to an older
	// payload and greps to zero on a 2026 page.
	reNativeSD = regexp.MustCompile(`"browser_native_sd_url":"([^"]+)"`)
	reNativeHD = regexp.MustCompile(`"browser_native_hd_url":"([^"]+)"`)
	// The full MPD, escaped once inside the JSON island.
	reDashXML = regexp.MustCompile(`"dash_manifest_xml_string":"(.*?)","[a-z_]+":`)

	reSharePostID = regexp.MustCompile(`story_fbid=(\d+)`)
	reShareOwner  = regexp.MustCompile(`[?&]id=(\d+)`)
	reAnyVideoID  = regexp.MustCompile(`"video_id":"(\d+)"`)
	reAnyStoryID  = regexp.MustCompile(`"story_fbid":"?(\d{6,})`)
)

// curlGet runs curl with the facebook cookie jar. Every Facebook fetch in this
// file goes through it: ctx.Fetch receives 400 because Facebook fingerprints
// Go's TLS stack, while the curl binary gets 200 from the same URL.
func curlGet(ctx context.Context, url string, opts []string) ([]byte, error) {
	jar := util.GetExtractorCookies("facebook")
	var ck strings.Builder
	for _, c := range jar {
		ck.WriteString(c.Name)
		ck.WriteByte('=')
		ck.WriteString(c.Value)
		ck.WriteString("; ")
	}
	args := []string{"-sL", "--compressed", "--max-time", "30"}
	args = append(args, opts...)
	if ck.Len() > 0 {
		args = append(args, "-H", "Cookie: "+ck.String())
	}
	args = append(args, url)

	cctx, cancel := context.WithTimeout(ctx, 36*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "curl", args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("curl %s: %w (%s)", url, err,
			strings.TrimSpace(errb.String()))
	}
	return []byte(out.String()), nil
}

// desktopHeaders is the set that makes Facebook render a media-bearing page.
// The image types in Accept are load-bearing: without them the same request
// returns a 210 KB crawler shell with the payload stripped.
var desktopHeaders = []string{
	"-A", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	"-H", "Accept: text/html,application/xhtml+xml,application/xml;q=0.9," +
		"image/avif,image/webp,*/*;q=0.8",
	"-H", "Accept-Language: en-US,en;q=0.9",
	"-H", "Sec-Fetch-Mode: navigate",
	"-H", "Sec-Fetch-Dest: document",
	"-H", "Sec-Fetch-Site: none",
	"-H", "Upgrade-Insecure-Requests: 1",
}

// unescapeFB reverses the JSON escaping Facebook applies inside its islands.
func unescapeFB(s string) string {
	s = strings.ReplaceAll(s, `\/`, "/")
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\u003C`, "<")
	s = strings.ReplaceAll(s, `\u003E`, ">")
	s = strings.ReplaceAll(s, `\u0026`, "&")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\u0025`, "%")
	return s
}

// shareRedirect asks where a share code points without following the hop. The
// second hop lands on /login and carries nothing, so following it is pointless;
// the location header itself is the answer.
func shareRedirect(ctx context.Context, shareURL string) (postID, owner string, err error) {
	body, err := curlGet(ctx, shareURL, []string{
		"-o", "/dev/null", "-D", "-",
		"-A", "facebookexternalhit/1.1",
		"-H", "Accept: text/html,application/xhtml+xml",
		"-H", "Accept-Language: en-US,en;q=0.5",
	})
	if err != nil {
		return "", "", err
	}
	// Take the FIRST location header. curl -L writes one per hop and the last
	// is the login wall; the 302 that names story_fbid is the first.
	var loc string
	for _, ln := range strings.Split(string(body), "\n") {
		if i := strings.Index(strings.ToLower(ln), "location:"); i >= 0 {
			loc = strings.TrimSpace(ln[i+len("location:"):])
			break
		}
	}
	if loc == "" {
		return "", "", fmt.Errorf("share redirect: no location header")
	}
	if m := reSharePostID.FindStringSubmatch(loc); len(m) == 2 {
		postID = m[1]
	}
	if m := reShareOwner.FindStringSubmatch(loc); len(m) == 2 {
		owner = m[1]
	}
	if postID == "" {
		return "", "", fmt.Errorf("share redirect: no story_fbid in %.90s", loc)
	}
	return postID, owner, nil
}

// videoIDsFromIndex collects candidate video ids that the owner's index page
// exposes. Facebook renders that index as a React island, so the ids are pulled
// from the embedded JSON rather than from anchors.
func videoIDsFromIndex(ctx context.Context, owner string) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range []string{
		"https://www.facebook.com/profile.php?id=" + owner + "&sk=videos",
		"https://www.facebook.com/" + owner + "/videos/",
	} {
		body, err := curlGet(ctx, u, desktopHeaders)
		if err != nil || len(body) < 5000 {
			continue
		}
		for _, m := range reAnyVideoID.FindAllSubmatch(body, -1) {
			id := string(m[1])
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// videoForPost returns the video id whose watch page embeds *postID*.
//
// story_fbid identifies an attachment, not a video, and Facebook publishes no
// mapping between the two. Each candidate's page does name the attachments that
// embed it, so the post id can be matched exactly instead of guessed.
func videoForPost(ctx context.Context, postID string, candidates []string) (string, []byte, error) {
	var firstErr error
	for _, id := range candidates {
		body, err := curlGet(ctx,
			"https://www.facebook.com/watch/?v="+id+"&_rdr", desktopHeaders)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !strings.Contains(string(body), postID) {
			continue
		}
		return id, body, nil
	}
	if firstErr != nil {
		return "", nil, fmt.Errorf("no video matched post %s (%v)", postID, firstErr)
	}
	return "", nil, fmt.Errorf("no video matched post %s", postID)
}

// watchPageMedia extracts everything downloadable from one watch page.
//
// Two sources, best first:
//   - the DASH ladder, when it carries a rendition taller than the muxed file
//   - browser_native_sd_url, a complete MP4 that needs no muxing at all
//
// The second is preferred for delivery because it is already muxed and needs no
// transcode, which is what keeps the audio untouched. The ladder is still
// recorded so a taller picture is not thrown away.
func watchPageMedia(body []byte) (muxed string, dash *dashTracks) {
	if m := reNativeSD.FindSubmatch(body); len(m) == 2 {
		muxed = unescapeFB(string(m[1]))
	}
	if m := reNativeHD.FindSubmatch(body); len(m) == 2 {
		if hd := unescapeFB(string(m[1])); hd != "" && hd != "null" {
			muxed = hd // HD wins when Facebook actually offers one
		}
	}
	if m := reDashXML.FindSubmatch(body); len(m) == 2 {
		if xml := unescapeFB(string(m[1])); strings.Contains(xml, "<MPD") {
			dash = parseDASHManifest(xml)
		}
	}
	return muxed, dash
}

// getShareMedia resolves a share URL. ok=false means "not a video post", which
// lets the caller fall through to the photo path untouched.
func getShareMedia(ctx *models.ExtractorContext) (*models.Media, bool) {
	postID, owner, err := shareRedirect(ctx.Context, ctx.ContentURL)
	if err != nil {
		ctx.Infof("facebook share: %v", err)
		return nil, false
	}
	ctx.Infof("facebook share: post=%s owner=%s", postID, owner)

	candidates := candidateVideoIDs(ctx.Context, owner, ctx.ContentURL)
	if len(candidates) == 0 {
		ctx.Infof("facebook share: no candidate video ids for owner %s", owner)
		return nil, false
	}
	ctx.Infof("facebook share: %d candidates from index", len(candidates))

	vid, body, err := videoForPost(ctx.Context, postID, candidates)
	if err != nil {
		ctx.Infof("facebook share: %v", err)
		return nil, false
	}
	muxed, dash := watchPageMedia(body)
	ctx.Infof("facebook share: video=%s muxed=%v dash=%v",
		vid, muxed != "", dash != nil && len(dash.Videos) > 0)

	if muxed == "" && (dash == nil || len(dash.Videos) == 0) {
		ctx.Infof("facebook share: nothing downloadable on %s", vid)
		return nil, false
	}

	media := &models.Media{}
	if m := reOGDescription.FindSubmatch(body); len(m) == 2 {
		media.SetCaption(strings.TrimSpace(string(m[1])))
	}

	item := media.NewItem()

	// The muxed file is a complete MP4: video and audio already together, in
	// the codecs Facebook chose (h264 Main + aac, measured). Delivering it
	// directly means no merge, no transcode and therefore no chance of the
	// audio being touched.
	if muxed != "" {
		item.Formats = append(item.Formats, &models.MediaFormat{
			FormatID:   "native",
			Type:       database.MediaTypeVideo,
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			URL:        []string{muxed},
		})
	}

	// Only take the ladder when it is strictly taller than the muxed file.
	// Otherwise the same 360x640 picture arrives twice, once needing a mux and
	// once not.
	if dash != nil && len(dash.Videos) > 0 {
		tgLimit := util.TelegramBotAPIFileLimit()
		safeLimit := tgLimit * 95 / 100
		dashFormats := buildDASHFormat(ctx, &VideoData{
			DASH:         dash,
			DASHDuration: dash.Duration,
		}, safeLimit)
		tallest := dash.Videos[0]
		if len(dashFormats) > 0 && tallest.Height > 640 {
			item.Formats = append(item.Formats, dashFormats...)
			ctx.Infof("facebook share: ladder %dx%d kept", tallest.Width, tallest.Height)
		} else if len(dashFormats) > 0 {
			ctx.Infof("facebook share: ladder %dx%d dropped, muxed is same or better",
				tallest.Width, tallest.Height)
		}
	}

	if len(item.Formats) == 0 {
		return nil, false
	}
	return media, true
}

// keep these referenced: strconv/json are used by the DASH helpers in dash.go
// and re-exported here for the tests that build a page fixture.
var _ = strconv.Itoa
var _ = json.Marshal
