package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"govd/database"
	"govd/enums"
	"govd/models"
	"govd/plugins"
	"govd/util"
	"govd/util/libav"
	"govd/util/mp4box"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func getFileThumbnail(
	ctx context.Context,
	format *models.MediaFormat,
	filePath string,
	config *models.DownloadConfig,
) (string, error) {
	fileDir := filepath.Dir(filePath)
	fileName := filepath.Base(filePath)
	fileExt := filepath.Ext(fileName)
	fileBaseName := fileName[:len(fileName)-len(fileExt)]
	thumbnailFilePath := filepath.Join(fileDir, fileBaseName+".thumb.jpeg")

	if len(format.Thumbnail) > 0 {
		zap.S().Debug("downloading thumbnail from URL")
		file, err := util.DownloadFileInMemory(ctx, format.Thumbnail, config)
		if err != nil {
			return "", fmt.Errorf("failed to download file in memory: %w", err)
		}
		err = util.ImgToJPEG(file, thumbnailFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to convert to JPEG: %w", err)
		}
		return thumbnailFilePath, nil
	}
	if format.Type == enums.MediaTypeVideo {
		zap.S().Debug("extracting video thumbnail with libav")
		err := libav.ExtractVideoThumbnail(filePath, thumbnailFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to extract video thumbnail: %w", err)
		}
		return thumbnailFilePath, nil
	}
	return "", nil
}

func insertVideoInfo(
	format *models.MediaFormat,
	filePath string,
) {
	zap.S().Debug("extracting video info from mp4box")
	duration, width, height := mp4box.ExtractBoxMetadata(filePath)
	if duration == 0 && width == 0 && height == 0 {
		zap.S().Debug("extracting video info with libav")
		duration, width, height = libav.GetVideoInfo(filePath)
	}
	format.Duration = duration
	format.Width = width
	format.Height = height
}

func GetMessageFileID(msg *gotgbot.Message) string {
	switch {
	case msg.Video != nil:
		return msg.Video.FileId
	case msg.Animation != nil:
		return msg.Animation.FileId
	case msg.Photo != nil:
		return msg.Photo[len(msg.Photo)-1].FileId
	case msg.Document != nil:
		return msg.Document.FileId
	case msg.Audio != nil:
		return msg.Audio.FileId
	case msg.Voice != nil:
		return msg.Voice.FileId
	default:
		return ""
	}
}

func GetMessageFileSize(msg *gotgbot.Message) int64 {
	switch {
	case msg.Video != nil:
		return msg.Video.FileSize
	case msg.Animation != nil:
		return msg.Animation.FileSize
	case msg.Photo != nil:
		return msg.Photo[len(msg.Photo)-1].FileSize
	case msg.Document != nil:
		return msg.Document.FileSize
	case msg.Audio != nil:
		return msg.Audio.FileSize
	case msg.Voice != nil:
		return msg.Voice.FileSize
	default:
		return 0
	}
}

func StoreMedias(
	dlCtx *models.DownloadContext,
	msgs []gotgbot.Message,
	medias []*models.DownloadedMedia,
) error {
	if len(medias) == 0 {
		return errors.New("no media to store")
	}

	zap.S().Debugf(
		"storing %d medias for %s (%s)",
		len(medias),
		dlCtx.MatchedContentID,
		dlCtx.Extractor.CodeName,
	)

	storedMedias := make([]*models.Media, 0, len(medias))

	for idx, msg := range msgs {
		fileID := GetMessageFileID(&msg)
		if len(fileID) == 0 {
			return fmt.Errorf("no file ID found for media at index %d", idx)
		}
		fileSize := GetMessageFileSize(&msg)
		medias[idx].Media.Format.FileID = fileID
		medias[idx].Media.Format.FileSize = fileSize
		storedMedias = append(
			storedMedias,
			medias[idx].Media,
		)
	}
	for _, media := range storedMedias {
		err := database.StoreMedia(
			dlCtx.Extractor.CodeName,
			media.ContentID,
			media,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func FormatCaption(
	media *models.Media,
	isEnabled bool,
) string {
	newCaption := fmt.Sprintf(
		"<a href='%s'>source</a> - @govd_bot\n",
		media.ContentURL,
	)
	if isEnabled && media.Caption.Valid {
		text := media.Caption.String
		if len(text) > 600 {
			text = text[:600] + "..."
		}
		newCaption += fmt.Sprintf(
			"<blockquote expandable>%s</blockquote>\n",
			util.EscapeCaption(text),
		)
	}
	return newCaption
}

func TypingEffect(
	bot *gotgbot.Bot,
	chatID int64,
) {
	bot.SendChatAction(
		chatID,
		"typing",
		nil,
	)
}

func SendingEffect(
	bot *gotgbot.Bot,
	chatID int64,
	mediaType enums.MediaType,
) {
	action := "upload_document"
	if mediaType == enums.MediaTypeVideo {
		action = "upload_video"
	}
	if mediaType == enums.MediaTypeAudio {
		action = "upload_audio"
	}
	if mediaType == enums.MediaTypePhoto {
		action = "upload_photo"
	}
	bot.SendChatAction(
		chatID,
		action,
		nil,
	)
}

func HandleErrorMessage(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	err error,
) {
	if zap.S().Level() == zap.DebugLevel {
		zap.S().Errorf("error occurred: %v", err)
	}

	currentError := err

	if errors.Is(currentError, context.Canceled) ||
		errors.Is(currentError, context.DeadlineExceeded) {
		errorMessage := "download request canceled or timed out"
		SendErrorMessage(bot, ctx, errorMessage)
		return
	}

	for currentError != nil {
		var botError *util.Error
		if errors.As(currentError, &botError) {
			errorMessage := "error occurred when downloading: " + currentError.Error()
			SendErrorMessage(bot, ctx, errorMessage)
			return
		}
		currentError = errors.Unwrap(currentError)
	}

	errorMessage := "error occurred when downloading: " + err.Error()

	if strings.Contains(errorMessage, bot.Token) {
		errorMessage = "telegram related error, probably connection issues"
	}

	SendErrorMessage(bot, ctx, errorMessage)
}

func SendErrorMessage(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	errorMessage string,
) {
	switch {
	case ctx.Update.Message != nil:
		ctx.EffectiveMessage.Reply(
			bot,
			errorMessage,
			&gotgbot.SendMessageOpts{
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
					IsDisabled: true,
				},
			},
		)
	case ctx.Update.InlineQuery != nil:
		ctx.InlineQuery.Answer(
			bot,
			nil,
			&gotgbot.AnswerInlineQueryOpts{
				CacheTime: 1,
				Button: &gotgbot.InlineQueryResultsButton{
					Text:           errorMessage,
					StartParameter: "start",
				},
			},
		)
	case ctx.ChosenInlineResult != nil:
		bot.EditMessageText(
			errorMessage,
			&gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.ChosenInlineResult.InlineMessageId,
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
					IsDisabled: true,
				},
			},
		)
	}
}

func ensureMergeFormats(
	media *models.Media,
	videoFormat *models.MediaFormat,
) {
	zap.S().Debugf(
		"ensuring merge formats for %s (%s)",
		media.ContentID, media.ExtractorCodeName,
	)
	if videoFormat.Type != enums.MediaTypeVideo {
		return
	}
	if videoFormat.AudioCodec != "" {
		return
	}
	// video with no audio
	audioFormat := media.GetDefaultAudioFormat()
	if audioFormat == nil {
		return
	}
	videoFormat.AudioCodec = audioFormat.AudioCodec
	videoFormat.Plugins = append(videoFormat.Plugins, plugins.MergeAudio)
}
