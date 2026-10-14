package core

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// A plain error keeps the bare hashed message - no extra note appended.
func TestFormatErrorMessageNoNote(t *testing.T) {
	ctx := &ext.Context{Update: &gotgbot.Update{}}
	got := formatErrorMessage(ctx, "an error occurred, please try again later", "DEADBEEF", "")
	if strings.Contains(got, "\n") {
		t.Fatalf("unexpected trailing note: %q", got)
	}
	if !strings.Contains(got, "[<code>DEADBEEF</code>]") {
		t.Fatalf("missing hashed code: %q", got)
	}
}

// An oversized-upload error adds a second line explaining the real cause.
func TestFormatErrorMessageWithNote(t *testing.T) {
	ctx := &ext.Context{Update: &gotgbot.Update{}}
	got := formatErrorMessage(
		ctx,
		"an error occurred, please try again later",
		"FD4902CC",
		"this file is too large for telegram and exceeds the maximum allowed size",
	)
	if !strings.Contains(got, "[<code>FD4902CC</code>]") {
		t.Fatalf("missing hashed code: %q", got)
	}
	if !strings.Contains(got, "too large") {
		t.Fatalf("missing size note: %q", got)
	}
}

// Only the real 413 phrase is treated as an oversized upload; a stray
// number in an unrelated error must not trigger the size note.
func TestIsTelegramRequestTooLarge(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"failed to send media group: unable to sendMediaGroup: Request Entity Too Large", true},
		{"request entity too large", true},
		{"some error code 4137 happened", false},
		{"timeout while downloading", false},
	}
	for _, c := range cases {
		if got := isTelegramRequestTooLarge(errString(c.msg)); got != c.want {
			t.Fatalf("isTelegramRequestTooLarge(%q)=%v want %v", c.msg, got, c.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
