package facebook

import (
	"os"
	"strings"
	"testing"
)

// mustReadFile reads a sibling source file. These guards check shape rather
// than behaviour: what breaks is a header being trimmed or a parser taking the
// wrong match, not a data structure changing.
func mustReadFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// The Accept header is why restricted posts used to come back empty.
// Facebook picks the response shape from it:
//
//	without image/avif,image/webp -> 210 KB shell, no media payload
//	with                          -> 600-866 KB, payload present
//
// Same URL, same cookies, same user agent. If anyone trims this back, every
// share link silently stops resolving and the symptom is "content isn't
// available" rather than an error.
func TestShareAcceptHeaderHasImageTypes(t *testing.T) {
	src := mustReadFile(t, "sharevideo.go")
	for _, need := range []string{"image/avif", "image/webp"} {
		if !strings.Contains(src, need) {
			t.Fatalf("sharevideo.go Accept header lost %q", need)
		}
	}
}

// The redirect has to be read from the FIRST location header. curl -L emits one
// per hop, and the last is the login wall -- measured 2026-11-24, the extractor
// took the last one and reported "no story_fbid in https://.../login/?next=...".
func TestShareRedirectTakesFirstLocation(t *testing.T) {
	src := mustReadFile(t, "sharevideo.go")
	i := strings.Index(src, "var loc string")
	if i < 0 {
		t.Fatal("redirect parser not found")
	}
	tail := src[i:]
	if j := strings.Index(tail, "\n}\n"); j > 0 {
		tail = tail[:j]
	}
	if !strings.Contains(tail, "break") {
		t.Fatal("redirect parser does not stop at the first location header; " +
			"the last one is the login wall and carries no story_fbid")
	}
}

// Native SD is the muxed MP4. It is not called progressive_url -- that name
// belongs to an older payload and greps to zero on a 2026 page.
func TestNativeURLParser(t *testing.T) {
	page := []byte(`{"browser_native_sd_url":"https:\/\/video-fra3-1.xx.fbcdn.net\/a.mp4?x=1","browser_native_hd_url":null}`)
	muxed, _ := watchPageMedia(page)
	if muxed == "" {
		t.Fatal("no muxed URL parsed")
	}
	if strings.Contains(muxed, `\/`) {
		t.Fatalf("escaped URL not unescaped: %s", muxed)
	}
	if !strings.HasPrefix(muxed, "https://video-fra3-1.xx.fbcdn.net/a.mp4") {
		t.Fatalf("unexpected URL: %s", muxed)
	}
}

// A null hd_url must not overwrite a valid sd_url. Facebook sets hd_url to null
// whenever it has no separate HD rendition, which is the common case.
func TestNullHDPreservesSD(t *testing.T) {
	page := []byte(`{"browser_native_sd_url":"https:\/\/x\/sd.mp4","browser_native_hd_url":null}`)
	muxed, _ := watchPageMedia(page)
	if !strings.Contains(muxed, "sd.mp4") {
		t.Fatalf("sd_url lost when hd_url was null: %q", muxed)
	}
}

// Media is delivered as the muxed file, which is already video+audio in the
// codecs Facebook chose. Merging or transcoding it is what risks the audio, so
// the format must declare both codecs and carry a single URL.
func TestMuxedFormatDeclaresBothCodecs(t *testing.T) {
	src := mustReadFile(t, "sharevideo.go")
	i := strings.Index(src, `FormatID:   "native"`)
	if i < 0 {
		t.Fatal("native format not found")
	}
	block := src[i : i+400]
	if !strings.Contains(block, "VideoCodec") || !strings.Contains(block, "AudioCodec") {
		t.Fatal("native format must declare both codecs so core does not " +
			"attach MergeAudio and re-mux a file that is already muxed")
	}
}
