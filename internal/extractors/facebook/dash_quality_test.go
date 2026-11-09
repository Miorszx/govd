package facebook

import "testing"

// Facebook publishes several representations at the same height with
// different quality labels (480p..960p all at 956x1700). The picker must
// take the highest quality of the tallest group, not the first one in
// manifest order (which is ascending bandwidth).
func TestDASHPickerTakesHighestQualityOfSameHeight(t *testing.T) {
	manifest := `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" mediaPresentationDuration="PT21S">
<Period><AdaptationSet mimeType="video/mp4" contentType="video">
<Representation id="a" bandwidth="970939" codecs="av01.0.08M.08.0.111" mimeType="video/mp4" width="956" height="1700"><BaseURL>https://cdn.example/480p.mp4</BaseURL></Representation>
<Representation id="b" bandwidth="1584852" codecs="av01.0.08M.08.0.111" mimeType="video/mp4" width="956" height="1700"><BaseURL>https://cdn.example/540p.mp4</BaseURL></Representation>
<Representation id="c" bandwidth="4822264" codecs="av01.0.08M.08.0.111" mimeType="video/mp4" width="956" height="1700"><BaseURL>https://cdn.example/960p.mp4</BaseURL></Representation>
<Representation id="d" bandwidth="771870" codecs="av01.0.05M.08.0.111" mimeType="video/mp4" width="720" height="1280"><BaseURL>https://cdn.example/360p.mp4</BaseURL></Representation>
</AdaptationSet></Period></MPD>`
	tracks := parseDASHManifest(manifest)
	if tracks == nil {
		t.Fatal("manifest did not parse")
	}
	rep, ok := pickVideoForBudget(tracks, 50*1024*1024)
	if !ok {
		t.Fatal("expected a rendition to fit")
	}
	if rep.URL != "https://cdn.example/960p.mp4" {
		t.Fatalf("picked %s, want the highest quality of the tallest group", rep.URL)
	}
}
