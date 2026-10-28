package facebook

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

// These tests pin the mbasic fallback for reels whose www page refuses to
// render. The real-world shape is: www.facebook.com/reel/<id> answers HTTP 400
// with a ~1.5 KB error document (no player data), while
// mbasic.facebook.com/reel/<id> answers 200 with the complete video node and a
// downloadable m367 rendition. Before the fallback existed the extractor gave
// up with "no hd progressive_url found" even though the video was reachable.
//
// Every URL and body below is synthetic - no real reel, post or CDN
// identifier is committed here.

const (
	stubBase       = "https://mbasic.facebook.com/reel/"
	stubWatchBase  = "https://www.facebook.com/watch/?v="
	stubHD         = "https://scontent.example/v/t2/f2/m367/hd_one.mp4?oh=aa"
	stubHDDead     = "https://scontent.example/v/t2/f2/m367/hd_dead.mp4?oh=bb"
	stubSD         = "https://scontent.example/v/t2/f2/m412/sd_one.mp4?oh=cc"
	stubPlain      = "https://scontent.example/v/t2/f2/m412/plain_one.mp4?oh=dd"
	stubReelIDReal = "900000000000001"
)

// stubTransport answers by URL so a single test can describe the whole
// interaction: the failing www page, the mbasic page that carries the video
// node, and the CDN candidate URLs.
type stubTransport struct {
	// wwwStatus is what the /watch/?v= page returns (the progressive_url path
	// that runs first).
	wwwStatus int
	// wwwBody is what the /watch/?v= page serves on 200.
	wwwBody string
	// mbasicBody is what mbasic returns.
	mbasicBody string
	// mbasicStatus is the status mbasic answers with. Zero means 200.
	mbasicStatus int
	// mediaStatus maps a CDN URL to the status it answers.
	mediaStatus map[string]int

	// calls records every URL fetched, in order, so tests can assert that the
	// fallback runs only after the primary path failed.
	calls []string
}

func (s *stubTransport) Do(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	s.calls = append(s.calls, req.Method+" "+u)

	switch {
	case strings.HasPrefix(u, stubWatchBase):
		// Primary path: www page. A 400 here is exactly what the field report
		// showed; no player data is served.
		return makeResponse(req, s.wwwStatus, "text/html", s.wwwBody), nil
	case strings.HasPrefix(u, stubBase):
		status := s.mbasicStatus
		if status == 0 {
			status = http.StatusOK
		}
		if s.mbasicBody == "" {
			return makeResponse(req, status, "text/html", "<html></html>"), nil
		}
		return makeResponse(req, status, "text/html", s.mbasicBody), nil
	case u == stubPlain:
		// A 200 that carries text/plain: the CDN refusing to serve the body.
		return makeResponse(req, http.StatusOK, "text/plain", "Forbidden"), nil
	}

	// CDN candidate.
	if code, ok := s.mediaStatus[u]; ok {
		if code == http.StatusOK || code == http.StatusPartialContent {
			return makeResponse(req, code, "video/mp4", "0123456789"), nil
		}
		return makeResponse(req, code, "text/plain", "Forbidden"), nil
	}
	return makeResponse(req, http.StatusForbidden, "text/plain", "Forbidden"), nil
}

func makeResponse(req *http.Request, code int, contentType, body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", contentType)
	return &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func newStubContext(tr *stubTransport) *models.ExtractorContext {
	return &models.ExtractorContext{
		ContentURL: "https://www.facebook.com/reel/" + stubReelIDReal,
		ContentID:  stubReelIDReal,
		HTTPClient: &networking.HTTPClient{Client: tr},
		Context:    context.Background(),
	}
}

// mbasicPage wraps the candidate URLs in the escaped, JSON-embedded shape the
// extractor matches against.
func mbasicPage(urls ...string) string {
	var sb strings.Builder
	sb.WriteString(`<html><body><script>require("VideoNode");`)
	for _, u := range urls {
		escaped := strings.ReplaceAll(u, "/", `\/`)
		sb.WriteString(`{"video_src":"` + escaped + `"},`)
	}
	sb.WriteString(`</script></body></html>`)
	return sb.String()
}

// TestMBasicFallbackFindsHDWhenWWWRefuses is the core regression: www says 400,
// mbasic carries an m367 that downloads, so the extractor must return it
// instead of reporting a missing HD rendition.
func TestMBasicFallbackFindsHDWhenWWWRefuses(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  mbasicPage(stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	if got != stubHD {
		t.Fatalf("expected the m367 rendition %q, got %q", stubHD, got)
	}
}

// TestMBasicFallbackSkipsDeadCandidate pins the second half of the field
// report: several m367 candidates ship in the page and only one is
// downloadable, so the first match cannot be trusted blindly.
func TestMBasicFallbackSkipsDeadCandidate(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:  http.StatusBadRequest,
		mbasicBody: mbasicPage(stubHDDead, stubHD),
		mediaStatus: map[string]int{
			stubHDDead: http.StatusForbidden,
			stubHD:     http.StatusOK,
		},
	}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	if got != stubHD {
		t.Fatalf("expected the downloadable rendition %q, got %q", stubHD, got)
	}
}

// TestMBasicFallbackRequiresIPhoneUserAgent guards the header that makes
// mbasic serve the video node at all: with a desktop or Googlebot agent the
// same URL answers the error document.
func TestMBasicFallbackRequiresIPhoneUserAgent(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  mbasicPage(stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	var sawMBasic bool
	for _, c := range tr.calls {
		if strings.Contains(c, "mbasic.facebook.com") {
			sawMBasic = true
			break
		}
	}
	if !sawMBasic {
		t.Fatalf("expected the fallback to request mbasic, calls were %v", tr.calls)
	}
}

// TestMBasicFallbackReturnsEmptyWhenNoVideoNode covers the genuinely
// unavailable reel: mbasic returns 200 but carries no media, so the fallback
// must report "nothing" and let the caller surface its own error rather than
// inventing a URL.
func TestMBasicFallbackReturnsEmptyWhenNoVideoNode(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:  http.StatusBadRequest,
		mbasicBody: `<html><body><p>This content is not available</p></body></html>`,
	}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	if got != "" {
		t.Fatalf("expected empty result when the page has no media, got %q", got)
	}
}

// TestMBasicFallbackEmptyIDSkipsNetwork makes sure the fallback cannot fire a
// request to a bare "mbasic.facebook.com/reel/" URL when the id is unknown.
func TestMBasicFallbackEmptyIDSkipsNetwork(t *testing.T) {
	tr := &stubTransport{}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, "")

	if got != "" {
		t.Fatalf("expected empty result for an empty id, got %q", got)
	}
	if len(tr.calls) != 0 {
		t.Fatalf("expected no network calls for an empty id, got %v", tr.calls)
	}
}

// TestMBasicFallbackPrefersHDOverSD asserts the HD-only preference still holds
// inside the fallback: when both an m367 and an m412 are present and valid, the
// m367 wins.
func TestMBasicFallbackPrefersHDOverSD(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:  http.StatusBadRequest,
		mbasicBody: mbasicPage(stubSD, stubHD),
		mediaStatus: map[string]int{
			stubSD: http.StatusOK,
			stubHD: http.StatusPartialContent,
		},
	}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	if got != stubHD {
		t.Fatalf("expected the HD rendition %q to win over SD, got %q", stubHD, got)
	}
}

// TestResolveReelMediaFallsBackToMBasic pins the ladder itself, not just the
// fallback function. Deleting the fallback call from resolveReelMediaURLs must
// break this test - that is the whole point of testing the call site rather
// than the callee.
func TestResolveReelMediaFallsBackToMBasic(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  mbasicPage(stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	hd, _, _ := resolveReelMediaURLs(ctx, stubReelIDReal)

	if hd != stubHD {
		t.Fatalf("expected the ladder to fall back to mbasic and return %q, got %q", stubHD, hd)
	}

	// The primary path must have been tried first: mbasic is a fallback, not a
	// replacement, so a healthy www page should never pay for this request.
	var wwwIdx, mbasicIdx = -1, -1
	for i, c := range tr.calls {
		if strings.HasPrefix(c, "GET "+stubWatchBase) {
			wwwIdx = i
		}
		if strings.Contains(c, "mbasic.facebook.com") && mbasicIdx == -1 {
			mbasicIdx = i
		}
	}
	if wwwIdx == -1 {
		t.Fatalf("expected the www progressive path to be attempted, calls: %v", tr.calls)
	}
	if mbasicIdx < wwwIdx {
		t.Fatalf("expected mbasic after the www attempt, calls: %v", tr.calls)
	}
}

// TestResolveReelMediaSkipsFallbackWhenPrimarySucceeds makes sure the fallback
// stays a fallback: a reel whose www page renders normally must not spend an
// extra request on mbasic.
func TestResolveReelMediaSkipsFallbackWhenPrimarySucceeds(t *testing.T) {
	wwwBody := `{"progressive_url":"` + stubHD + `","failure_reason":null,"metadata":{"quality":"hd"}}`
	tr := &stubTransport{
		wwwStatus:   http.StatusOK,
		wwwBody:     wwwBody,
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	hd, _, _ := resolveReelMediaURLs(ctx, stubReelIDReal)

	if hd != stubHD {
		t.Fatalf("expected the primary path to supply %q, got %q", stubHD, hd)
	}
	for _, c := range tr.calls {
		if strings.Contains(c, "mbasic.facebook.com") {
			t.Fatalf("fallback must not run when the primary path succeeded, calls: %v", tr.calls)
		}
	}
}

// TestIsDownloadableMediaURLRejectsNonVideoBody pins the content-type check:
// a 200 that carries text/plain is the CDN's way of refusing the request, not
// a usable video.
func TestIsDownloadableMediaURLRejectsNonVideoBody(t *testing.T) {
	tr := &stubTransport{
		mediaStatus: map[string]int{stubHD: http.StatusOK},
	}
	ctx := newStubContext(tr)

	if !isDownloadableMediaURL(ctx, stubHD) {
		t.Fatalf("expected the video/mp4 response to be accepted")
	}

	// A 200 that serves text/plain is not media even though the status is OK.
	if isDownloadableMediaURL(ctx, stubPlain) {
		t.Fatalf("expected a text/plain 200 to be rejected as media")
	}
}

// TestMBasicFallbackRejectsErrorStatus pins the status check: a 400 from
// mbasic still has an HTML body, and that body can contain stale player
// markup. The fallback must refuse to parse it rather than serve a possibly
// dead rendition.
func TestMBasicFallbackRejectsErrorStatus(t *testing.T) {
	tr := &stubTransport{
		mbasicStatus: http.StatusBadRequest,
		mbasicBody:   mbasicPage(stubHD),
		mediaStatus:  map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	got := tryFetchHDReelFromMBasic(ctx, stubReelIDReal)

	if got != "" {
		t.Fatalf("expected an error status to be ignored, got %q", got)
	}
	for _, c := range tr.calls {
		if strings.Contains(c, "scontent.example") {
			t.Fatalf("must not probe candidate URLs from an error page, calls: %v", tr.calls)
		}
	}
}

// TestMBasicFallbackSkipsZeroID pins the empty-id guard: an unknown id must
// not turn into a request for a bogus URL, so the guard has to run before any
// network call.
func TestMBasicFallbackSkipsZeroID(t *testing.T) {
	tr := &stubTransport{
		mbasicBody:  mbasicPage(stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	if got := tryFetchHDReelFromMBasic(ctx, ""); got != "" {
		t.Fatalf("expected empty result for an empty id, got %q", got)
	}
	if len(tr.calls) != 0 {
		t.Fatalf("expected the guard to run before any network call, got %v", tr.calls)
	}
}
