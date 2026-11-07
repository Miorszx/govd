package facebook

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

func TestMeasureReal(t *testing.T) {
	id := os.Getenv("REEL_ID")
	if id == "" {
		t.Skip("no REEL_ID")
	}
	c := &models.ExtractorContext{
		ContentID:  id,
		ContentURL: "https://www.facebook.com/reel/" + id,
		Context:    t.Context(),
		HTTPClient: networking.NewHTTPClient(&networking.NewHTTPClientOptions{
			Cookies: util.ParseCookieFile("facebook.txt"),
		}),
	}
	m := fetchReelMedia(c, id)
	if m.DASH == nil {
		t.Fatal("no DASH")
	}
	t.Logf("DASH %dx%d source=%d bytes", m.DASH.Width, m.DASH.Height, m.DASH.VideoSize)

	src := "/tmp/m_src.mp4"
	req, _ := http.NewRequest("GET", m.DASH.VideoURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("dl %v", err)
	}
	f, _ := os.Create(src)
	n, _ := io.Copy(f, resp.Body)
	f.Close()
	resp.Body.Close()
	t.Logf("downloaded %d bytes", n)

	for _, crf := range []string{"23", "26", "28", "30", "32"} {
		out := "/tmp/m_crf" + crf + ".mp4"
		cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", src,
			"-an", "-c:v", "libx264", "-preset", "veryfast", "-crf", crf,
			"-pix_fmt", "yuv420p", out)
		if err := cmd.Run(); err != nil {
			t.Fatalf("ffmpeg crf%s: %v", crf, err)
		}
		st, _ := os.Stat(out)
		t.Logf("crf=%s out=%d bytes (%.2fx source)", crf, st.Size(), float64(st.Size())/float64(n))
		os.Remove(out)
	}
	os.Remove(src)
}
