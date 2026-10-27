package core

import (
	"context"
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
	"github.com/jackc/pgx/v5/pgtype"
)

func StoreMedia(
	ctx context.Context,
	extractor *models.Extractor,
	media *models.Media,
	messages []gotgbot.Message,
	formats []*models.DownloadedFormat,
) error {
	if len(media.Items) == 0 {
		return fmt.Errorf("no item to store")
	}

	fileIDs, fileSizes := collectMessageData(messages)
	if len(fileIDs) != len(media.Items) {
		return fmt.Errorf("number of file IDs does not match number of media items")
	}
	if len(fileSizes) != len(media.Items) {
		return fmt.Errorf("number of file sizes does not match number of media items")
	}

	tx, err := database.Conn().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := database.Q().WithTx(tx)

	mediaID, err := qtx.CreateMedia(ctx, database.CreateMediaParams{
		ExtractorID: extractor.ID,
		ContentUrl:  media.ContentURL,
		ContentID:   media.ContentID,
		Caption: pgtype.Text{
			String: media.Caption,
			Valid:  media.Caption != "",
		},
		Nsfw: media.NSFW,
	})
	if err != nil {
		return err
	}

	// On conflict (upsert), old items are deleted so new caption/file_id takes effect.
	// The delete must succeed: if it silently failed, the inserts below would
	// append a second set of items to the same media row and every later send
	// would deliver the post twice. Abort the transaction instead.
	if err := storeItems(ctx, qtx, mediaID, media, fileIDs, fileSizes, formats); err != nil {
		return err
	}

	logger.L.Debugf("stored media %s with %d items", media.ContentID, len(media.Items))

	return tx.Commit(ctx)
}

// itemStore is the subset of the generated query set that writing media items
// needs. Keeping it narrow lets the delete-before-insert contract be tested
// without a live PostgreSQL server.
type itemStore interface {
	DeleteMediaItemsByMediaID(ctx context.Context, mediaID int64) error
	CreateMediaItem(ctx context.Context, mediaID int64) (int64, error)
	CreateMediaFormat(ctx context.Context, arg database.CreateMediaFormatParams) error
}

// storeItems replaces the item rows of an existing media row. The delete runs
// first and its error is fatal: appending to a row whose old items survived
// would double every item and make the bot send the post twice on every later
// hit.
func storeItems(
	ctx context.Context,
	qtx itemStore,
	mediaID int64,
	media *models.Media,
	fileIDs []string,
	fileSizes []int64,
	formats []*models.DownloadedFormat,
) error {
	if len(media.Items) == 0 {
		return nil
	}

	if err := qtx.DeleteMediaItemsByMediaID(ctx, mediaID); err != nil {
		return fmt.Errorf("delete old media items %d: %w", mediaID, err)
	}

	for i := range media.Items {
		itemID, err := qtx.CreateMediaItem(ctx, mediaID)
		if err != nil {
			return err
		}

		fileID := fileIDs[i]
		fileSize := fileSizes[i]
		format := formats[i].Format

		err = qtx.CreateMediaFormat(ctx, database.CreateMediaFormatParams{
			ItemID:   itemID,
			FormatID: format.FormatID,
			FileID:   fileID,
			Type:     format.Type,
			AudioCodec: database.NullMediaCodec{
				MediaCodec: format.AudioCodec,
				Valid:      format.AudioCodec != "",
			},
			VideoCodec: database.NullMediaCodec{
				MediaCodec: format.VideoCodec,
				Valid:      format.VideoCodec != "",
			},
			FileSize: pgtype.Int8{
				Int64: fileSize,
				Valid: fileSize != 0,
			},
			Duration: pgtype.Int4{
				Int32: format.Duration,
				Valid: format.Duration != 0,
			},
			Title: pgtype.Text{
				String: format.Title,
				Valid:  format.Title != "",
			},
			Artist: pgtype.Text{
				String: format.Artist,
				Valid:  format.Artist != "",
			},
			Width: pgtype.Int4{
				Int32: format.Width,
				Valid: format.Width != 0,
			},
			Height: pgtype.Int4{
				Int32: format.Height,
				Valid: format.Height != 0,
			},
			Bitrate: pgtype.Int8{
				Int64: format.Bitrate,
				Valid: format.Bitrate != 0,
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func collectMessageData(messages []gotgbot.Message) ([]string, []int64) {
	fileIDs := make([]string, 0, len(messages))
	fileSizes := make([]int64, 0, len(messages))
	for _, msg := range messages {
		fileID := util.GetMessageFileID(&msg)
		fileIDs = append(fileIDs, fileID)

		fileSize := util.GetMessageFileSize(&msg)
		fileSizes = append(fileSizes, fileSize)
	}
	return fileIDs, fileSizes
}

func ParseStoredMedia(
	ctx context.Context,
	extractor *models.Extractor,
	mediaRow *database.GetMediaByContentIDRow,
) (*models.Media, error) {
	// Optimized: single JOIN query fetches items+formats in one roundtrip (fixes N+1)
	rows, err := database.Q().GetMediaItemsWithFormats(ctx, mediaRow.ID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no media items found")
	}

	items := make([]*models.MediaItem, 0, len(rows))
	for _, r := range rows {
		// Reconstruct GetMediaFormatRow from joined row
		fRow := database.GetMediaFormatRow{
			FormatID:   r.FormatID,
			ItemID:     r.ItemID_2,
			FileID:     r.FileID,
			Type:       r.Type,
			AudioCodec: r.AudioCodec,
			VideoCodec: r.VideoCodec,
			Duration:   r.Duration,
			FileSize:   r.FileSize,
			Title:      r.Title,
			Artist:     r.Artist,
			Width:      r.Width,
			Height:     r.Height,
			Bitrate:    r.Bitrate,
		}
		items = append(items, &models.MediaItem{
			Formats: []*models.MediaFormat{parseFormatFromDB(&fRow)},
		})
	}

	media := &models.Media{
		ContentID:   mediaRow.ContentID,
		ContentURL:  mediaRow.ContentUrl,
		Caption:     mediaRow.Caption.String,
		NSFW:        mediaRow.Nsfw,
		ExtractorID: extractor.ID,
		Items:       items,
	}

	return media, nil
}
