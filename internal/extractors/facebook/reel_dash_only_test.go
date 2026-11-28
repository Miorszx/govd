package facebook

import (
	"encoding/json"
	"testing"
)

// Facebook serves a growing number of reels as DASH-only: the watch page
// carries a complete AV1 ladder plus audio but zero progressive_url entries.
// reelMediaFromBody must still surface the DASH tracks so extractReelMedia can
// hand them to buildMedia instead of reporting a false "flagged/private" error.
func TestReelMediaFromBodyDASHOnly(t *testing.T) {
	manifest := `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" mediaPresentationDuration="PT21S">
<Period><AdaptationSet contentType="video">
<Representation id="v" bandwidth="1000000" codecs="av01.0.08M.08.0.111" width="1080" height="1920"><BaseURL>https://cdn.example/v.mp4</BaseURL></Representation>
</AdaptationSet><AdaptationSet contentType="audio">
<Representation id="a" bandwidth="128000" codecs="mp4a.40.5"><BaseURL>https://cdn.example/a.mp4</BaseURL></Representation>
</AdaptationSet></Period></MPD>`
	body := []byte(`{"dash_manifest_xml_string":` + mustJSONString(manifest) + `}`)

	m := reelMediaFromBody(body, "123")
	if m.HD != "" || m.SD != "" {
		t.Fatalf("expected no progressive URLs, got hd=%q sd=%q", m.HD, m.SD)
	}
	if m.DASH == nil || len(m.DASH.Videos) == 0 {
		t.Fatal("DASH ladder lost on a DASH-only reel")
	}
	if m.DASH.Videos[0].Height != 1920 {
		t.Fatalf("peak height = %d, want 1920", m.DASH.Videos[0].Height)
	}
	if m.DASH.AudioURL == "" {
		t.Fatal("audio rendition lost")
	}
}

func mustJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
