package facebook

import (
	"strings"
	"testing"
)

// A direct message.text node is already the full post caption for its video.
// The page-name-split heuristic must not truncate its first line.
func TestDirectCaptionKeepsFirstLine(t *testing.T) {
	inner := `{"video_id":"2792910907770618","message":{"text":"This video never get old \\ud83e\\udd2a\\ud83e\\udd23...\\n\\nWara clean jol \\ud83e\\udd23..\\n\\n#fyp\\u30b7\\u309aviral\\u30b7"}}`
	body := []byte(`<script data-sjs>{"__scheduledServerJS":{"ScheduledServerJS":` + inner + `}}</script>`)
	got := tryExtractCaptionFromBodyBytes(body, "2792910907770618")
	if !strings.HasPrefix(got, "This video never get old") {
		t.Fatalf("first line truncated: %q", got)
	}
	if !strings.Contains(got, "Wara clean jol") {
		t.Fatalf("caption incomplete: %q", got)
	}
	if !strings.Contains(got, "#fyp") {
		t.Fatalf("hashtags missing: %q", got)
	}
}

// The page-name heuristic still applies to page-shell text.
func TestPageShellCaptionStillSplitsPageName(t *testing.T) {
	in := "Some Page Name\n\nthe actual caption here"
	got := splitFacebookPageNamePrefix(in)
	if strings.Contains(got, "Some Page Name") {
		t.Fatalf("page name should be dropped: %q", got)
	}
}
