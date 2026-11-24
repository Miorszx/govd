package facebook

// Post-id to video-id memo.
//
// Resolving a share link costs three Facebook requests: the redirect, the owner
// index, and the watch page. The first two are the fragile ones -- measured
// 2026-11-24, two messages sent nine seconds apart:
//
//	15:14:20  group    1 candidates from index   -> resolved
//	15:14:29  private  no candidate video ids    -> B8DF020F, failed
//
// Same cookie, same post, nine seconds apart. Facebook rate-limits the index
// per session, so the second caller saw nothing and the share link failed for
// no reason the user could see.
//
// The mapping itself never changes, though: post 2234347124101379 is video
// 3093314621017769 today and will be next month. Caching it turns a
// three-request walk into one request, and removes the step that gets
// rate-limited from the second caller onwards.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// shareMemoPath lives beside the cookies: it is per-session state, not repo
// content, and it must survive a bot restart so a rate-limited minute does not
// throw away everything learned before it.
func shareMemoPath() string {
	if p := os.Getenv("SHORTLINK_SHARE_MEMO"); p != "" {
		return p
	}
	return filepath.Join("private", "fb_share_memo.json")
}

var (
	shareMemoMu sync.RWMutex
	shareMemo   map[string][]string // post id -> video ids (a post can hold several)
	shareLoaded bool
)

func loadShareMemo() {
	if shareLoaded {
		return
	}
	shareLoaded = true
	shareMemo = map[string][]string{}
	b, err := os.ReadFile(shareMemoPath())
	if err != nil {
		return
	}
	// Two shapes exist on disk. The first release stored a bare string per post;
	// posts can hold several videos, so the current shape is a list. Decode
	// loosely and normalise, otherwise an existing memo would fail to load and
	// silently lose everything it had.
	var loose map[string]json.RawMessage
	if err := json.Unmarshal(b, &loose); err != nil {
		return
	}
	for post, raw := range loose {
		var one string
		if err := json.Unmarshal(raw, &one); err == nil {
			if one != "" {
				shareMemo[post] = []string{one}
			}
			continue
		}
		var many []string
		if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 {
			shareMemo[post] = many
		}
	}
}

// memoGet returns the video ids previously learned for postID.
func memoGet(postID string) ([]string, bool) {
	shareMemoMu.RLock()
	defer shareMemoMu.RUnlock()
	loadShareMemo()
	v, ok := shareMemo[postID]
	return v, ok
}

// memoPut records postID -> videoIDs. Write failures are ignored: the memo is an
// optimisation, and losing it only costs the requests it was meant to save.
func memoPut(postID string, videoIDs []string) {
	if postID == "" || len(videoIDs) == 0 {
		return
	}
	shareMemoMu.Lock()
	defer shareMemoMu.Unlock()
	loadShareMemo()
	old := shareMemo[postID]
	if len(old) == len(videoIDs) {
		same := true
		for i := range old {
			if old[i] != videoIDs[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	shareMemo[postID] = videoIDs
	if b, err := json.MarshalIndent(shareMemo, "", " "); err == nil {
		_ = os.WriteFile(shareMemoPath(), b, 0o644)
	}
}

// memoSize reports how many post ids are known, for the log line that tells an
// operator whether the cache is doing anything.
func memoSize() int {
	shareMemoMu.RLock()
	defer shareMemoMu.RUnlock()
	loadShareMemo()
	return len(shareMemo)
}
