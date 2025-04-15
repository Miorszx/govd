package handlers

import (
	"context"
	"govd/bot/core"
	"govd/models"
	"govd/util"
	"strings"
	"time"

	extractors "govd/ext"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func InlineDownloadHandler(
	bot *gotgbot.Bot,
	ctx *ext.Context,
) error {
	url := strings.TrimSpace(ctx.InlineQuery.Query)
	if url == "" {
		ctx.InlineQuery.Answer(bot, []gotgbot.InlineQueryResult{}, &gotgbot.AnswerInlineQueryOpts{
			CacheTime:  1,
			IsPersonal: true,
		})
		return nil
	}
	dlCtx, err := extractors.CtxByURL(url)
	if err != nil || dlCtx == nil || dlCtx.Extractor == nil {
		ctx.InlineQuery.Answer(bot, []gotgbot.InlineQueryResult{}, &gotgbot.AnswerInlineQueryOpts{
			CacheTime:  1,
			IsPersonal: true,
		})
		return nil
	}
	return core.HandleInline(bot, ctx, dlCtx)
}

func InlineDownloadResultHandler(
	bot *gotgbot.Bot,
	ctx *ext.Context,
) error {
	taskID := ctx.ChosenInlineResult.ResultId
	dlCtx, ok := core.GetTask(taskID)
	if !ok {
		return nil
	}
	defer core.DeleteTask(taskID)

	mediaChan := make(chan *models.Media, 1)
	errChan := make(chan error, 1)
	timeout, cancel := context.WithTimeout(
		context.Background(),
		5*time.Minute,
	)
	defer cancel()

	go core.GetInlineFormat(
		bot, ctx, dlCtx,
		mediaChan, errChan,
	)
	select {
	case media := <-mediaChan:
		err := core.HandleInlineCachedResult(
			bot, ctx,
			dlCtx, media,
		)
		if err != nil {
			core.HandleErrorMessage(bot, ctx, err)
			return nil
		}
	case err := <-errChan:
		core.HandleErrorMessage(bot, ctx, err)
		return nil
	case <-timeout.Done():
		core.HandleErrorMessage(bot, ctx, util.ErrTimeout)
		return nil
	}
	return nil
}

func InlineLoadingHandler(
	bot *gotgbot.Bot,
	ctx *ext.Context,
) error {
	ctx.CallbackQuery.Answer(bot, &gotgbot.AnswerCallbackQueryOpts{
		Text:      "wait !",
		ShowAlert: true,
	})
	return nil
}
