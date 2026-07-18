# Changelog

## [Unreleased] - 2026-07-18

### Added
- Facebook image V2 method: fresh scontent `oh=` signature only, no fallback graph src / og:image / upgrade p600->p1080 (which caused Bad URL hash 403). Flow: `mbasic/share/p/{id}/ iPhone -> og:url group post -> mbasic/groups/.../posts/... iPhone 222KB 11 scontent fresh -> dl 200 OK`
- Facebook reel & group video V2 method: `plugins/video.php?href={{url}}&show_text=0` desktop Chrome UA + fizz TLS impersonate -> `hd_src m366` / `sd_src m412` HD-ONLY only. No fallback `og:video sve_sd`, `browser_native`, `playable_url`, `www/m mbasic` retry which are flagged (1542 Error, 258KB login.php, 50KB shell)
- Unified handling for `/reel/`, `/watch`, `/videos/` as plugins HD method
- Caption fallback: when `show_text=0` has no caption (flagged), try `mbasic/reel/{id}` message extraction + `plugins show_text=1` div parsing for meaningful text
- Thumbnail fix: `libav.ExtractVideoThumbnail` now `ss 0.5s` + `scale=320:trunc(ow/dar/2)*2` + `q:v 2` -> 320x568 36K jpeg, avoids black first frame (previous `gte(n,0)` frame 0) and Telegram 320px limit. Fixes black thumbnail preview

### Changed
- `core/util.go` `formatCaption`: rune-safe truncate `[]rune` 900 reserve 124 chars tag, header `<a href='{{url}}'>source</a> - @{{username}}` + `\n` + `<blockquote expandable>{{text}}</blockquote>` with `Unquote` escaping `< >`. Fixes emoji broken `�`
- `libav/thumbnail.go`: avoids black frame, HQ 0.5s extraction
- `ShareExtractor` simplified: `mbasic/share/{id}/` iPhone for og:url (works when www flagged), then www/share iPhone fallback. Removed `FetchLocation 3x`, `S:_I`, `post_id`, `story_fbid` regex fallbacks
- All Facebook extractors still Golang only (`internal/extractors/facebook/`, `internal/util/libav/`), no Python

### Fixed
- Panic regex `{10,2000}` invalid repeat count causing bot no response for reel/group video (fixed to `[^"]+`)
- FB cookies flagged detection: `mbasic post: no scontent len=51020`, `www 1542 Error`, `m 10KB 1 image`, `plugins 86K empty hd_src` vs `273K with hd_src` desktop difference documented. Watchdog script `fb_cookie_watchdog.py` exists
- Source button + @ignah_bot missing when caption empty - now always returns header even if caption empty

### Build
- `go vet 0`, `go build 42M`, Docker `debian:bookworm-slim + ffmpeg libheif1 ca-certificates`, image `govd-ignah:patched`, container `bot started with username: ignah_bot`
- No external references to specific FB page/video IDs or repo links in code comments - generic descriptions only (privacy)

### Tested Links (generic)
- `share/p` photo post: mbasic permalink 222KB 11 scontent fresh oh= -> 50KB jpeg 200 OK
- `reel` HD: plugins 273KB desktop -> hd_src 11MB 720x1280 48s -> dl 200 OK + thumb 320x568
- `share/v` group video: mbasic share/v iPhone 46K og:url -> /videos/ permalink -> plugins 231K sd_src only case where source is SD 256x144 7s original
- Caption: header + quote blockquote expandable works, thumbnail preview not black
- IG/Threads/TikTok caption consistency preserved

## Previous
- See git log `upstream/main` for govdbot/govd history
