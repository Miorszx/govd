package facebook

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

// These tests pin the caption side of the mbasic fallback. The field report
// was: a share link whose post carries a caption arrives in chat as a bare
// video with no text at all. The cause is ordering, not parsing - the www
// page (the progressive_url path) answers HTTP 400 for these reels, so the
// only document that holds the caption is the mbasic one that the fallback
// already fetches for the rendition. Dropping the caption on the floor there
// loses text the extractor has already paid a request for.
//
// Every body below is synthetic - no real reel, post, caption or CDN
// identifier is committed here.

// captionPage builds an mbasic document carrying one media candidate and an
// og:description caption, in the shape Facebook actually serves.
func captionPage(caption string, urls ...string) string {
	var sb strings.Builder
	sb.WriteString(`<html><head>`)
	if caption != "" {
		sb.WriteString(`<meta property="og:description" content="` + caption + `" />`)
	}
	sb.WriteString(`</head><body><script>require("VideoNode");`)
	for _, u := range urls {
		escaped := strings.ReplaceAll(u, "/", `\/`)
		sb.WriteString(`{"video_src":"` + escaped + `"},`)
	}
	sb.WriteString(`</script></body></html>`)
	return sb.String()
}

// TestMBasicCaptionIsUnescaped pins the encoding path. Non-Latin captions
// arrive as numeric character references, so the raw entity soup must never
// reach the chat: it has to come out as readable text.
func TestMBasicCaptionIsUnescaped(t *testing.T) {
	// Three Cyrillic letters behind entities plus a symbol. A caption that
	// skips unescaping shows the entities instead of the letters.
	page := captionPage(`&#x41f;&#x440;&#x438; &#x274c;`)

	got := mbasicCaptionFromBody([]byte(page))

	want := "При ❌"
	if got != want {
		t.Fatalf("expected the unescaped caption %q, got %q", want, got)
	}
}

// TestMBasicCaptionKeepsHashtagsAndNewlines guards the shape of the text: a
// caption is multi-line and hashtag-led in this family of posts, and the
// cleaning step must not collapse it into one line or strip the markers.
func TestMBasicCaptionKeepsHashtagsAndNewlines(t *testing.T) {
	page := captionPage(`#&#x41f;&#x440;&#x438;\n#&#x43a;&#x43e;`)

	got := mbasicCaptionFromBody([]byte(page))

	if !strings.Contains(got, "#При") {
		t.Fatalf("expected hashtags to survive cleaning, got %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected the caption to stay multi-line, got %q", got)
	}
}

// TestMBasicCaptionRejectsPrivacyWall pins the privacy case: a members-only
// post renders that placeholder in the same meta tag, and it is not a
// caption. Serving it would put Facebook's UI copy in the chat.
func TestMBasicCaptionRejectsPrivacyWall(t *testing.T) {
	for _, placeholder := range []string{
		"Only members can see who's in the group and what they post.",
		"Log in to Facebook to start sharing and connecting with your friends.",
		"Log into Facebook",
	} {
		page := captionPage(placeholder)
		if got := mbasicCaptionFromBody([]byte(page)); got != "" {
			t.Fatalf("expected %q to be rejected as a caption, got %q", placeholder, got)
		}
	}
}

// TestMBasicCaptionRejectsGenericTitle makes sure the generic site name that
// Facebook prints when there is no caption is not passed through as text.
func TestMBasicCaptionRejectsGenericTitle(t *testing.T) {
	for _, generic := range []string{"Facebook", ""} {
		page := captionPage(generic)
		if got := mbasicCaptionFromBody([]byte(page)); got != "" {
			t.Fatalf("expected %q to be rejected as a caption, got %q", generic, got)
		}
	}
}

// TestResolveReelMediaReturnsCaptionFromMBasic is the end-to-end regression.
// The www page refuses (400), mbasic carries both the rendition and the
// caption, and the ladder must return both. Before the fix the caption was
// dropped even though the document holding it had already been fetched.
func TestResolveReelMediaReturnsCaptionFromMBasic(t *testing.T) {
	const wantCaption = "a caption printed on the mbasic document"
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  captionPage(wantCaption, stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	hd, _, title := resolveReelMediaURLs(ctx, stubReelIDReal)

	if hd != stubHD {
		t.Fatalf("expected the mbasic rendition %q, got %q", stubHD, hd)
	}
	if title != wantCaption {
		t.Fatalf("expected the caption %q from the mbasic document, got %q", wantCaption, title)
	}
}

// TestResolveReelMediaCostsOneMBasicFetch pins the reason the caption rides
// along with the rendition instead of being fetched separately: the fallback
// must stay a single request. A second mbasic fetch for the caption would
// double the cost of every reel in this family.
func TestResolveReelMediaCostsOneMBasicFetch(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  captionPage("A caption that lives on the mbasic page", stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	resolveReelMediaURLs(ctx, stubReelIDReal)

	mbasicFetches := 0
	for _, c := range tr.calls {
		if strings.HasPrefix(c, "GET "+stubBase) {
			mbasicFetches++
		}
	}
	if mbasicFetches != 1 {
		t.Fatalf("expected exactly 1 mbasic fetch, got %d (calls: %v)", mbasicFetches, tr.calls)
	}
}

// TestResolveReelMediaNoCaptionFromPrivacyWall makes sure the whole ladder
// stays clean for a members-only reel: the rendition is still returned, but
// the placeholder text is not.
func TestResolveReelMediaNoCaptionFromPrivacyWall(t *testing.T) {
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  captionPage("Only members can see who's in the group and what they post.", stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	hd, _, title := resolveReelMediaURLs(ctx, stubReelIDReal)

	if hd != stubHD {
		t.Fatalf("expected the rendition to still resolve, got %q", hd)
	}
	if title != "" {
		t.Fatalf("expected no caption for a privacy wall, got %q", title)
	}
}

// TestResolveReelMediaKeepsPrimaryCaption pins that the mbasic caption only
// fills a gap. When the www path already supplied text, that text wins - the
// fallback must not overwrite a caption that came from the authoritative
// document.
func TestResolveReelMediaKeepsPrimaryCaption(t *testing.T) {
	const primaryCaption = "caption that came from the www document"
	// The www path reads the caption out of the data-sjs bundle, not out of
	// the meta tags, so the fixture has to carry that shape.
	wwwBody := `{"progressive_url":"` + stubHD + `","failure_reason":null,` +
		`"metadata":{"quality":"hd"}}` +
		`<script>data-sjs>{"bundle":"ScheduledServerJS","comet_sections":` +
		`{"message":{"story":{"message":{"text":"` + primaryCaption + `"}}}}}</script>`
	tr := &stubTransport{
		wwwStatus:   http.StatusOK,
		wwwBody:     wwwBody,
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	_, _, title := resolveReelMediaURLs(ctx, stubReelIDReal)

	if title != primaryCaption {
		t.Fatalf("expected the www caption %q to win, got %q", primaryCaption, title)
	}
	for _, c := range tr.calls {
		if strings.Contains(c, "mbasic.facebook.com") {
			t.Fatalf("fallback must not run when the primary path succeeded, calls: %v", tr.calls)
		}
	}
}

// TestResolveReelMediaKeepsWWWCaptionWhenNoRendition pins the mixed case: the
// www document renders and carries the caption but ships no progressive_url,
// so the ladder still has to fall back for the rendition. That caption is the
// authoritative one and must survive the fallback.
func TestResolveReelMediaKeepsWWWCaptionWhenNoRendition(t *testing.T) {
	const wwwCaption = "caption from a www page with no rendition"
	wwwBody := `<script>data-sjs>{"bundle":"ScheduledServerJS","comet_sections":` +
		`{"message":{"story":{"message":{"text":"` + wwwCaption + `"}}}}}</script>`
	tr := &stubTransport{
		wwwStatus:   http.StatusOK,
		wwwBody:     wwwBody,
		mbasicBody:  captionPage("caption printed on the mbasic document", stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	hd, _, title := resolveReelMediaURLs(ctx, stubReelIDReal)

	if hd != stubHD {
		t.Fatalf("expected the mbasic rendition %q, got %q", stubHD, hd)
	}
	if title != wwwCaption {
		t.Fatalf("expected the www caption %q to survive the fallback, got %q", wwwCaption, title)
	}
}

// TestMBasicReelNodeReturnsMediaAndCaption pins the helper contract directly,
// including the empty-id guard that must run before any network call.
func TestMBasicReelNodeReturnsMediaAndCaption(t *testing.T) {
	const wantCaption = "Both come from one document"
	tr := &stubTransport{
		wwwStatus:   http.StatusBadRequest,
		mbasicBody:  captionPage(wantCaption, stubHD),
		mediaStatus: map[string]int{stubHD: http.StatusPartialContent},
	}
	ctx := newStubContext(tr)

	u, caption := mbasicReelNode(ctx, stubReelIDReal)

	if u != stubHD {
		t.Fatalf("expected rendition %q, got %q", stubHD, u)
	}
	if caption != wantCaption {
		t.Fatalf("expected caption %q, got %q", wantCaption, caption)
	}

	empty := &stubTransport{}
	u, caption = mbasicReelNode(newStubContext(empty), "")
	if u != "" || caption != "" {
		t.Fatalf("expected empty results for an empty id, got %q / %q", u, caption)
	}
	if len(empty.calls) != 0 {
		t.Fatalf("expected no network calls for an empty id, got %v", empty.calls)
	}
}

// TestMBasicCaptionEmptyBodyIsSafe pins that a document without the meta tag
// yields no caption rather than panicking or inventing text.
func TestMBasicCaptionEmptyBodyIsSafe(t *testing.T) {
	for _, body := range []string{"", "<html></html>", `<meta property="og:title" content="x">`} {
		if got := mbasicCaptionFromBody([]byte(body)); got != "" {
			t.Fatalf("expected no caption for body %q, got %q", body, got)
		}
	}
}

// guard: the caption helpers must not need their own HTTP client beyond the
// context they are handed.
var _ = context.Background
var _ = models.NewFilesTracker
var _ = networking.HTTPClient{}
