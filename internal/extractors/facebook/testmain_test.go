package facebook

import (
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

// TestMain routes reelPageFetch through the test extractor's HTTP client so
// the stub transport in reel_mbasic_*_test.go can serve the page bodies.
// Production uses fetchViaCurl; tests keep the ladder logic testable without
// invoking the curl binary.
func TestMain(m *testing.M) {
	reelPageFetch = func(ctx *models.ExtractorContext, url string) ([]byte, error) {
		resp, err := ctx.Fetch(http.MethodGet, url, &networking.RequestParams{})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, &fetchStatusError{code: resp.StatusCode}
		}
		return io.ReadAll(resp.Body)
	}
	os.Exit(m.Run())
}

type fetchStatusError struct{ code int }

func (e *fetchStatusError) Error() string { return http.StatusText(e.code) }
