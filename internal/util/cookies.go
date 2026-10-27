package util

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aki237/nscjar"
	"github.com/govdbot/govd/internal/logger"
)

const cookiesDir = "private/cookies"

// cookieEntry is a parsed cookie file plus the file identity it was parsed
// from. Storing the identity lets us invalidate the cache when the file is
// replaced on disk (e.g. the operator exports fresh cookies) without needing
// a restart.
type cookieEntry struct {
	cookies []*http.Cookie
	modTime time.Time
	size    int64
}

var (
	cookiesMu    sync.RWMutex
	cookiesCache = make(map[string]cookieEntry)
)

func GetExtractorCookies(extractorID string) []*http.Cookie {
	if extractorID == "" {
		return nil
	}
	return ParseCookieFile(extractorID + ".txt")
}

func ParseCookieFile(fileName string) []*http.Cookie {
	return parseCookieFileFrom(cookiesDir, fileName)
}

// parseCookieFileFrom parses fileName under dir, reusing the cached result
// only while the file's mtime and size are unchanged.
func parseCookieFileFrom(dir, fileName string) []*http.Cookie {
	if fileName == "" {
		return nil
	}

	cookiePath := filepath.Join(dir, fileName)

	info, err := os.Stat(cookiePath)
	if err != nil {
		return nil
	}
	modTime, size := info.ModTime(), info.Size()

	cookiesMu.RLock()
	entry, ok := cookiesCache[fileName]
	cookiesMu.RUnlock()

	if ok && entry.modTime.Equal(modTime) && entry.size == size {
		return cloneCookies(entry.cookies)
	}

	cookieFile, err := os.Open(cookiePath)
	if err != nil {
		return nil
	}
	defer cookieFile.Close()

	var parser nscjar.Parser
	cookies, err := parser.Unmarshal(cookieFile)
	if err != nil {
		if logger.L != nil {
			logger.L.Warnf("failed parsing cookie file %s: %v", fileName, err)
		}
		return nil
	}

	cookiesMu.Lock()
	cookiesCache[fileName] = cookieEntry{
		cookies: cookies,
		modTime: modTime,
		size:    size,
	}
	cookiesMu.Unlock()

	if logger.L != nil {
		logger.L.Debugf("parsed cookie file: %s (%d cookies)", fileName, len(cookies))
	}

	return cloneCookies(cookies)
}

// cloneCookies deep-copies the slice so callers cannot mutate the cached
// entries. http.Cookie carries a pointer to its raw form, so a shallow slice
// copy would still leak mutations back into the cache.
func cloneCookies(in []*http.Cookie) []*http.Cookie {
	if len(in) == 0 {
		return nil
	}
	out := make([]*http.Cookie, len(in))
	for i, c := range in {
		if c == nil {
			continue
		}
		clone := *c
		out[i] = &clone
	}
	return out
}
