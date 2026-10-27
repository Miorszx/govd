package database

import (
	"strings"
	"testing"
)

// TestGetOrCreateChatGuardsSettingsUpsert is the regression test for the write
// amplification bug. The settings upsert runs on every single message the bot
// handles, and PostgreSQL writes a new row version for DO UPDATE even when the
// assigned value is identical. Without a WHERE guard the settings table grew a
// dead tuple per message.
//
// The guard alone would make RETURNING empty, so the query must also fall back
// to reading the existing settings row. Both halves are asserted here because
// dropping either one breaks the bot: no guard means write amplification, no
// fallback means the JOIN returns zero rows and GetOrCreateChat reports
// "no rows in result set" on every message from an existing chat.
func TestGetOrCreateChatGuardsSettingsUpsert(t *testing.T) {
	q := getOrCreateChat

	if !strings.Contains(q, "WHERE settings.language = 'XX'") {
		t.Error("settings upsert has no WHERE guard: every message rewrites the row")
	}

	// The fallback must exist, otherwise RETURNING is empty for an unchanged
	// chat and the final JOIN yields no row.
	if !strings.Contains(q, "FROM settings\n    WHERE chat_id = $1") {
		t.Error("no fallback read of the existing settings row: RETURNING is empty when the guard skips the update")
	}

	if !strings.Contains(q, "NOT EXISTS (SELECT 1 FROM upsert_settings)") {
		t.Error("fallback read is not conditional on the upsert returning nothing")
	}
}

// TestGetOrCreateChatReturnsAllSettingsColumns guards the row shape: sqlc
// generates GetOrCreateChatRow from the final SELECT, so a dropped column
// silently zeroes a setting for every chat.
func TestGetOrCreateChatReturnsAllSettingsColumns(t *testing.T) {
	q := getOrCreateChat

	columns := []string{
		"c.chat_id",
		"c.type",
		"s.nsfw",
		"s.media_album_limit",
		"s.captions",
		"s.silent",
		"s.language",
		"s.disabled_extractors",
		"s.delete_links",
	}
	for _, col := range columns {
		if !strings.Contains(q, col) {
			t.Errorf("final SELECT is missing %s", col)
		}
	}
}
