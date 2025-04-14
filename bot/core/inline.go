package core

import (
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"govd/database"
	"govd/enums"
	"govd/models"
	"govd/util"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var InlineTasks = make(map[string]*models.DownloadContext)

func HandleInline(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	dlCtx *models.DownloadContext,
) error {
	if dlCtx.Extractor.Type != enums.ExtractorTypeSingle {
		return util.ErrNotImplemented
	}
	contentID := dlCtx.MatchedContentID
	cached, err := database.GetDefaultMedias(
		dlCtx.Extractor.CodeName,
		contentID,
	)
	if err != nil {
		return err
	}
	if len(cached) > 0 {
		if len(cached) > 1 {
			return util.ErrInlineMediaGroup
		}
		err = HandleInlineCached(
			bot, ctx,
			dlCtx, cached[0],
		)
		if err != nil {
			return err
		}
		return nil
	}
	err = StartInlineTask(bot, ctx, dlCtx)
	if err != nil {
		return err
	}
	return nil
}

func HandleInlineCached(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	dlCtx *models.DownloadContext,
	media *models.Media,
) error {
	var result gotgbot.InlineQueryResult

	format := media.Format
	resultID := fmt.Sprintf("%d:%s", ctx.EffectiveUser.Id, format.FormatID)
	resultTitle := "share"
	mediaCaption := FormatCaption(media, true)
	_, inputFileType := format.GetFormatInfo()

	switch inputFileType {
	case "photo":
		result = &gotgbot.InlineQueryResultCachedPhoto{
			Id:          resultID,
			PhotoFileId: format.FileID,
			Title:       resultTitle,
			Caption:     mediaCaption,
			ParseMode:   "HTML",
		}
	case "video":
		result = &gotgbot.InlineQueryResultCachedVideo{
			Id:          resultID,
			VideoFileId: format.FileID,
			Title:       resultTitle,
			Caption:     mediaCaption,
			ParseMode:   "HTML",
		}
	case "audio":
		result = &gotgbot.InlineQueryResultCachedAudio{
			Id:          resultID,
			AudioFileId: format.FileID,
			Caption:     mediaCaption,
			ParseMode:   "HTML",
		}
	case "document":
		result = &gotgbot.InlineQueryResultCachedDocument{
			Id:             resultID,
			DocumentFileId: format.FileID,
			Title:          resultTitle,
			Caption:        mediaCaption,
			ParseMode:      "HTML",
		}
	default:
		return errors.New("unsupported input file type")
	}
	ctx.InlineQuery.Answer(
		bot, []gotgbot.InlineQueryResult{result},
		&gotgbot.AnswerInlineQueryOpts{
			CacheTime:  1,
			IsPersonal: true,
		},
	)
	return nil
}

func HandleInlineCachedResult(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	dlCtx *models.DownloadContext,
	media *models.Media,
) error {
	format := media.Format
	messageCaption := FormatCaption(media, true)
	inputMedia, err := format.GetInputMediaWithFileID(messageCaption)
	if err != nil {
		return err
	}

	_, _, err = bot.EditMessageMedia(
		inputMedia,
		&gotgbot.EditMessageMediaOpts{
			InlineMessageId: ctx.ChosenInlineResult.InlineMessageId,
		},
	)
	if err != nil {
		return err
	}
	return nil
}

func StartInlineTask(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	dlCtx *models.DownloadContext,
) error {
	randomID, err := uuid.NewUUID()
	if err != nil {
		return errors.New("could not generate task ID")
	}
	taskID := randomID.String()
	inlineResult := &gotgbot.InlineQueryResultArticle{
		Id:    taskID,
		Title: "share",
		InputMessageContent: &gotgbot.InputTextMessageContent{
			MessageText: "loading media plese wait...",
			ParseMode:   "HTML",
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
		},
		ReplyMarkup: &gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "...",
						CallbackData: "inline:loading",
					},
				},
			},
		},
	}
	ok, err := ctx.InlineQuery.Answer(
		bot, []gotgbot.InlineQueryResult{inlineResult},
		&gotgbot.AnswerInlineQueryOpts{
			CacheTime:  1,
			IsPersonal: true,
		},
	)
	if err != nil {
		log.Println("failed to answer inline query:", err)
	}
	if !ok {
		log.Println("failed to answer inline query")
		return nil
	}
	InlineTasks[taskID] = dlCtx
	return nil
}

func GetInlineFormat(
	bot *gotgbot.Bot,
	ctx *ext.Context,
	dlCtx *models.DownloadContext,
	mediaChan chan<- *models.Media,
	errChan chan<- error,
) {
	response, err := dlCtx.Extractor.Run(dlCtx)
	if err != nil {
		errChan <- fmt.Errorf("failed to get media: %w", err)
		return
	}
	mediaList := response.MediaList
	if len(mediaList) == 0 {
		errChan <- fmt.Errorf("no media found for content ID: %s", dlCtx.MatchedContentID)
	}
	if len(mediaList) > 1 {
		errChan <- util.ErrInlineMediaGroup
		return
	}
	for i := range mediaList {
		defaultFormat := mediaList[i].GetDefaultFormat()
		if defaultFormat == nil {
			errChan <- fmt.Errorf("no default format found for media at index %d", i)
			return
		}
		if len(defaultFormat.URL) == 0 {
			errChan <- fmt.Errorf("media format at index %d has no URL", i)
			return
		}
		// ensure we can merge video and audio formats
		ensureMergeFormats(mediaList[i], defaultFormat)
		mediaList[i].Format = defaultFormat
	}
	messageCaption := FormatCaption(mediaList[0], true)
	medias, err := DownloadMedias(mediaList, nil)
	if err != nil {
		errChan <- fmt.Errorf("failed to download medias: %w", err)
		return
	}
	msgs, err := SendMedias(
		bot, ctx, dlCtx,
		medias, &models.SendMediaFormatsOptions{
			Caption: messageCaption,
		},
	)
	if err != nil {
		errChan <- fmt.Errorf("failed to send media: %w", err)
		return
	}
	msg := &msgs[0]
	msg.Delete(bot, nil)
	err = StoreMedias(
		dlCtx, msgs,
		medias,
	)
	if err != nil {
		errChan <- fmt.Errorf("failed to store media: %w", err)
		return
	}
	mediaChan <- medias[0].Media
}
