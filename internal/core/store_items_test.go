package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
)

// failingStore fakes the query subset used when replacing media items. Only
// DeleteMediaItemsByMediaID can fail; the counters record what actually ran so
// the tests can prove inserts never happen after a failed delete.
type failingStore struct {
	deleteErr error
	deleteRan bool
	inserts   int
}

func (f *failingStore) DeleteMediaItemsByMediaID(ctx context.Context, mediaID int64) error {
	f.deleteRan = true
	return f.deleteErr
}

func (f *failingStore) CreateMediaItem(ctx context.Context, mediaID int64) (int64, error) {
	f.inserts++
	return int64(f.inserts), nil
}

func (f *failingStore) CreateMediaFormat(ctx context.Context, arg database.CreateMediaFormatParams) error {
	return nil
}

func testMedia() *models.Media {
	return &models.Media{
		ContentID: "synthetic",
		Items: []*models.MediaItem{
			{Formats: []*models.MediaFormat{{FormatID: "f1", Type: database.MediaTypeVideo}}},
			{Formats: []*models.MediaFormat{{FormatID: "f2", Type: database.MediaTypeVideo}}},
		},
	}
}

// Two items and matching file data, so storeItems walks the full insert path.
func testArgs() ([]string, []int64, []*models.DownloadedFormat) {
	fileIDs := []string{"file-a", "file-b"}
	fileSizes := []int64{111, 222}
	formats := []*models.DownloadedFormat{
		{Format: &models.MediaFormat{FormatID: "f1", Type: database.MediaTypeVideo}},
		{Format: &models.MediaFormat{FormatID: "f2", Type: database.MediaTypeVideo}},
	}
	return fileIDs, fileSizes, formats
}

// TestStoreItemsAbortsOnDeleteFailure is the regression test for the duplicate
// send bug: when deleting the previous items fails, the insert loop must not
// run, because a second set of items makes every later send deliver the post
// twice.
func TestStoreItemsAbortsOnDeleteFailure(t *testing.T) {
	store := &failingStore{deleteErr: errors.New("simulated delete failure")}
	fileIDs, fileSizes, formats := testArgs()

	err := storeItems(context.Background(), store, 42, testMedia(), fileIDs, fileSizes, formats)

	if err == nil {
		t.Fatal("expected error when delete fails, got nil")
	}
	if !store.deleteRan {
		t.Fatal("delete was never attempted")
	}
	if store.inserts != 0 {
		t.Fatalf("expected 0 inserts after failed delete, got %d", store.inserts)
	}
}

// TestStoreItemsErrorMentionsMediaID keeps the failure diagnosable in logs.
func TestStoreItemsErrorMentionsMediaID(t *testing.T) {
	store := &failingStore{deleteErr: errors.New("simulated delete failure")}
	fileIDs, fileSizes, formats := testArgs()

	err := storeItems(context.Background(), store, 987654, testMedia(), fileIDs, fileSizes, formats)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "987654") {
		t.Fatalf("error should name the media row, got %q", err.Error())
	}
}

// TestStoreItemsInsertsWhenDeleteSucceeds proves the happy path still writes one
// item per media item.
func TestStoreItemsInsertsWhenDeleteSucceeds(t *testing.T) {
	store := &failingStore{}
	fileIDs, fileSizes, formats := testArgs()

	if err := storeItems(context.Background(), store, 1, testMedia(), fileIDs, fileSizes, formats); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !store.deleteRan {
		t.Fatal("delete was never attempted")
	}
	if store.inserts != 2 {
		t.Fatalf("expected 2 inserts, got %d", store.inserts)
	}
}

// TestStoreItemsEmptyMediaIsNoop guards the early return: no items means no
// database work at all.
func TestStoreItemsEmptyMediaIsNoop(t *testing.T) {
	store := &failingStore{}
	media := &models.Media{ContentID: "synthetic"}

	if err := storeItems(context.Background(), store, 1, media, nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.deleteRan || store.inserts != 0 {
		t.Fatalf("expected no database work, got delete=%v inserts=%d", store.deleteRan, store.inserts)
	}
}
