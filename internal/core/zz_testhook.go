package core

import "github.com/govdbot/govd/internal/models"

// DownloadFormats exposes the delivery path so a probe can exercise download
// and post-processing, not just extraction. Extraction succeeding while
// delivery failed was exactly the blind spot that hid the share-link bug.
func DownloadFormats(ctx *models.ExtractorContext, media *models.Media) ([]*models.DownloadedFormat, error) {
	return downloadMediaFormats(ctx, media)
}
