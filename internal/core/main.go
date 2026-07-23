package core

import (
	"fmt"
	"regexp"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

// staleCaptionPattern matches raw, un-decoded JSON unicode escapes such as
// "\uD83E\uDD40". These indicate the stored caption was cached before a
// caption-decoding bug was fixed and must be re-extracted rather than served
// stale from the database forever.
var staleCaptionPattern = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)

// isStaleCaption reports whether a cached caption still contains encoded
// surrogate/escape sequences that should have been decoded into real text.
func isStaleCaption(caption string) bool {
	return staleCaptionPattern.MatchString(caption)
}

func HandleDownloadTask(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	extractorCtx *models.ExtractorContext,
) error {
	defer extractorCtx.FilesTracker.Cleanup()

	key := extractorCtx.Key()

	acquireQueue(key)
	defer releaseQueue(key)

	message := ctx.EffectiveMessage
	isSpoiler := util.HasHashtagEntity(message, "spoiler") ||
		util.HasHashtagEntity(message, "nsfw")

	taskResult, err := executeDownload(extractorCtx, false)
	if err != nil {
		return err
	}

	caption := formatCaption(
		taskResult.Media,
		bot.Username,
		extractorCtx.Chat.Captions,
	)

	_, err = SendFormats(
		bot, ctx, extractorCtx,
		taskResult.Media, taskResult.Formats,
		&models.SendFormatsOptions{
			Caption:   caption,
			IsSpoiler: isSpoiler,
			IsStored:  taskResult.IsStored,
		},
	)
	if err != nil {
		return err
	}
	return nil
}

// performs the actual download operation
// this function is wrapped by singleflight
// to prevent duplicate downloads
func executeDownload(extractorCtx *models.ExtractorContext, isInline bool) (*models.TaskResult, error) {
	if config.Env.Caching {
		task, err := taskFromDatabase(extractorCtx)
		if err == nil {
			if isInline && len(task.Media.Items) > 1 {
				return nil, util.ErrInlineMediaAlbum
			}
			err = checkAlbumLimit(
				len(task.Media.Items),
				extractorCtx.Chat,
			)
			if err != nil {
				return nil, err
			}
			extractorCtx.Debugf("media found in database")
			return task, nil
		}
	}
	resp, err := extractorCtx.Extractor.GetFunc(extractorCtx)
	if err != nil {
		return nil, err
	}
	if resp.Media == nil || len(resp.Media.Items) == 0 {
		// no media extracted (e.g. text only post)
		return nil, ErrNoMedia
	}

	if isInline && len(resp.Media.Items) > 1 {
		return nil, util.ErrInlineMediaAlbum
	}
	err = checkAlbumLimit(
		len(resp.Media.Items),
		extractorCtx.Chat,
	)
	if err != nil {
		return nil, err
	}

	formats, err := downloadMediaFormats(extractorCtx, resp.Media)
	if err != nil {
		return nil, err
	}

	return &models.TaskResult{
		Media:   resp.Media,
		Formats: formats,
	}, nil
}

func taskFromDatabase(ctx *models.ExtractorContext) (*models.TaskResult, error) {
	mediaRow, err := database.Q().GetMediaByContentID(
		ctx.Context,
		database.GetMediaByContentIDParams{
			ExtractorID: ctx.Extractor.ID,
			ContentID:   ctx.ContentID,
		},
	)
	if err != nil {
		return nil, err
	}

	// A previously-cached caption may have been stored with an encoding
	// bug (raw "\uXXXX" surrogate escapes). Treat such rows as stale so the
	// caller falls through to a fresh extraction instead of serving the
	// broken caption forever. See isStaleCaption.
	if mediaRow.Caption.Valid && isStaleCaption(mediaRow.Caption.String) {
		ctx.Debugf("cached caption for %s is stale (encoded escape leak), re-extracting", ctx.ContentID)
		return nil, fmt.Errorf("stale cached caption")
	}

	media, err := ParseStoredMedia(ctx.Context, ctx.Extractor, &mediaRow)
	if err != nil {
		return nil, err
	}

	// Fresh caption: try to re-fetch caption from the extractor (light HTTP page fetch,
	// no video download). This keeps file_id cache for instant send, but caption is always fresh.
	// If fresh fetch fails (anti-bot 400, network, etc), fallback to cached caption.
	// FIX BUG: Only overwrite if fresh caption non-empty AND contentID matches (prevent cross-post caption leak)
	// Also: if fresh fetch returns empty caption but cached has caption, keep cached (don't clear)
	// If fresh fetch returns empty and post truly has no caption, cached should already be empty
	if freshMedia := tryFetchFreshCaption(ctx); freshMedia != nil {
		// Only accept fresh caption if it's from same content_id (prevent collision leak)
		if freshMedia.ContentID == ctx.ContentID || freshMedia.ContentID == "" {
			if freshMedia.Caption != "" {
				media.Caption = freshMedia.Caption
			} else {
				// Fresh says no caption - respect it if cached caption was from fresh logic
				// But don't clear if cached caption exists and fresh is empty due to parse fail
				// Check if fresh extraction actually succeeded in getting media items
				if len(freshMedia.Items) > 0 {
					// Fresh extraction succeeded but no caption = truly no caption, clear cached if needed
					// Only clear if original fresh parse had no caption logic error
					// For safety: don't auto-clear, keep cached only if fresh is empty
					// User wants: post Ade video ja tapi caption takde = should be empty
					// So if fresh says empty and we got items, set empty
					if media.Caption != "" {
						// Check if cached caption is actually from this same URL by comparing content_url
						// If fresh succeeded with 0 caption, post truly has no caption
						ctx.Debugf("fresh extraction has no caption, clearing stale cached caption")
						media.Caption = ""
					}
				}
			}
		}
		// Also update content_url in case it changed (e.g. short link -> canonical)
		if freshMedia.ContentURL != "" && (freshMedia.ContentID == ctx.ContentID || freshMedia.ContentID == "") {
			media.ContentURL = freshMedia.ContentURL
		}
	}

	formats := make([]*models.DownloadedFormat, 0, len(media.Items))
	for i, item := range media.Items {
		formats = append(formats, &models.DownloadedFormat{
			Format: item.Formats[0],
			Index:  i,
		})
	}

	return &models.TaskResult{
		Media:    media,
		Formats:  formats,
		IsStored: true,
	}, nil
}

// tryFetchFreshCaption does a light extraction (no download) to get fresh caption.
// Returns nil on any error so caller can fallback to cached caption.
func tryFetchFreshCaption(ctx *models.ExtractorContext) *models.Media {
	resp, err := ctx.Extractor.GetFunc(ctx)
	if err != nil {
		return nil
	}
	if resp == nil || resp.Media == nil {
		return nil
	}
	return resp.Media
}

func checkAlbumLimit(n int, chat *database.GetOrCreateChatRow) error {
	if chat.Type == database.ChatTypeGroup {
		if n > int(chat.MediaAlbumLimit) {
			return util.ErrMediaAlbumLimitExceeded
		}
	}
	// global limit
	// TODO: make this configurable
	if n > 30 {
		return util.ErrMediaAlbumGlobalLimitExceeded
	}
	return nil
}

func validateFormat(fmt *models.MediaFormat) error {
	if util.ExceedsMaxFileSize(fmt.FileSize) {
		return util.ErrFileTooLarge
	}
	// Even if the operator's configured budget is larger than what the
	// Telegram Bot API will actually accept, refuse early so we do not
	// waste bandwidth downloading a file that always 413s.
	if fmt.FileSize > 0 && util.ExceedsTelegramFileLimit(fmt.FileSize) {
		return util.ErrTelegramFileTooLarge
	}
	if util.ExceedsMaxDuration(fmt.Duration) {
		return util.ErrDurationTooLong
	}
	return nil
}
