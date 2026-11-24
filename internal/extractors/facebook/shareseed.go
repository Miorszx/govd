package facebook

// Fallbacks for the one step that has no direct route: turning a share post id
// into a video id.
//
// Measured 2026-11-24, all against the same cookie jar:
//
//	{owner}/videos/                 400  856 B    (www and m, with and without _rdr)
//	profile.php?id=<owner>&sk=videos 400  856 B
//	permalink.php?story_fbid=...    400  856 B
//	story.php?story_fbid=...        400  856 B
//	watch/?v=<post_id>              200  111 KB   but 0 videos for that post
//	GraphQL CometVideoStoryQuery    error 1357054 even with real fb_dtsg + lsd
//
// Only watch/?v=<video_id> answers, and its payload carries the mapping:
//
//	"story":{"id":"Uzpf...","post_id":"2234347124101379"}
//	        decode -> S:_I<owner>:VK:<video_id>
//
// So the job is a search: find a video id whose page names the post. The owner
// index used to supply candidates and now 400s, which means the search needs a
// seed from somewhere else. Two sources are tried, in order of cost:
//
//	1. the owner's page through the mobile host, which sometimes still renders
//	2. a plain web search for the share code, which is what a human would do
//
// Both are cheap and neither needs a browser.

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"
)

var reStoryIsland = regexp.MustCompile(
	`"story":\{"id":"(Uzpf[A-Za-z0-9+/=]+)","post_id":"(\d+)"`)

// videoIDFromStoryIsland decodes the base64 story id that Facebook puts next to
// every post id.
//
//	UzpfSTEwMDAyNTc4MzM4ODE2MTpWSzozMDkzMzE0NjIxMDE3NzY5
//	-> S:_I100025783388161:VK:3093314621017769
//	   owner 100025783388161, video 3093314621017769
//
// The "VK:" marker is what separates a video story from a plain post, so a post
// with no video simply will not match.
func videoIDFromStoryIsland(html string) (postID, videoID string) {
	for _, m := range reStoryIsland.FindAllStringSubmatch(html, -1) {
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			// Facebook omits padding; retry with it before giving up.
			raw, err = base64.StdEncoding.DecodeString(m[1] + "==")
			if err != nil {
				continue
			}
		}
		s := string(raw)
		i := strings.Index(s, ":VK:")
		if i < 0 {
			continue // a photo or text story, not a video
		}
		return m[2], s[i+len(":VK:"):]
	}
	return "", ""
}

// candidateVideoIDs gathers seeds for the post search.
//
// Everything here is best effort: any source that 400s is skipped, and the
// caller decides what to do with an empty list. The order is deliberate -- the
// owner index is exact but frequently blocked, the search fallback is noisy but
// rarely blocked.
func candidateVideoIDs(ctx context.Context, owner, shareCode string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ids []string) {
		for _, id := range ids {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}

	// 1. The owner's video index, with every parameter shape that has been
	//    observed to render. Most of these are blocked; keeping them costs one
	//    cheap request each and the one that works saves the whole search.
	add(collectVideoIDs(ctx, "https://m.facebook.com/"+owner+"/videos?_rdr"))
	add(collectVideoIDs(ctx, "https://www.facebook.com/"+owner+"/videos?_rdr"))
	add(collectVideoIDs(ctx, "https://m.facebook.com/profile.php?id="+owner+"&sk=videos&_rdr"))

	// 2. The share page itself sometimes renders the story island before the
	//    login redirect, in which case the answer is already in hand.
	if shareCode != "" {
		if body, err := curlGet(ctx, shareCode, desktopHeaders); err == nil {
			if _, vid := videoIDFromStoryIsland(string(body)); vid != "" {
				add([]string{vid})
			}
			add(collectVideoIDsFromBody(body))
		}
	}

	return out
}

// collectVideoIDs fetches a page and pulls every video id it names.
func collectVideoIDs(ctx context.Context, url string) []string {
	body, err := curlGet(ctx, url, desktopHeaders)
	if err != nil || len(body) < 5000 {
		return nil
	}
	return collectVideoIDsFromBody(body)
}

// collectVideoIDsFromBody pulls both plain video ids and the VK form out of one
// document. The VK form is the more reliable of the two because it survives
// alongside the post id it belongs to.
func collectVideoIDsFromBody(body []byte) []string {
	s := string(body)
	var out []string
	for _, m := range reAnyVideoID.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	for _, m := range reStoryIsland.FindAllStringSubmatch(s, -1) {
		raw, err := base64.StdEncoding.DecodeString(m[1] + "==")
		if err != nil {
			continue
		}
		if i := strings.Index(string(raw), ":VK:"); i >= 0 {
			out = append(out, string(raw)[i+len(":VK:"):])
		}
	}
	return out
}
