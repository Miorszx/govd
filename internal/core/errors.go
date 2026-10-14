package core

import (
	"errors"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/localization"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

func HandleError(
	b *gotgbot.Bot,
	ctx *ext.Context,
	extractorCtx *models.ExtractorContext,
	err error,
) {
	chat := extractorCtx.Chat
	localizer := localization.New(chat.Language)

	// Friendly handling for story expired - don't show hashed ID, show clear message
	if strings.Contains(strings.ToLower(err.Error()), "story may have expired") {
		extractorCtx.Infof("story expired handled: %v", err)
		sendErrorMessage(
			b, ctx, "",
			"This Facebook story has expired or is not accessible anymore.",
			"",
		)
		return
	}

	botError := asBotError(err)
	if botError != nil {
		sendErrorMessage(
			b, ctx, "",
			localizer.T(&i18n.LocalizeConfig{
				MessageID: botError.ID,
			}),
			"",
		)
		return
	}

	if errors.Is(err, ErrNoMedia) {
		return
	}
	if isChatWriteForbidden(err) {
		return
	}
	if isPermissionDenied(err) {
		sendErrorMessage(
			b, ctx, "",
			localizer.T(&i18n.LocalizeConfig{
				MessageID: localization.ErrorPermissionDenied.ID,
			}),
			"",
		)
		return
	}

	errorID := util.HashedError(err)

	extractorCtx.Errorf("unexpected error: [%s] %v", errorID, err)

	// Telegram rejects an oversized multipart upload with HTTP 413
	// "Request Entity Too Large" - e.g. a media album whose combined
	// size exceeds the 50 MiB cloud Bot API cap even when every file in
	// it is individually under it. When that happens, append the real
	// reason after the hashed generic message so the user knows it was a
	// size limit, not a transient bug.
	var note string
	if isTelegramRequestTooLarge(err) {
		note = localizer.T(&i18n.LocalizeConfig{
			MessageID: localization.ErrorTelegramFileTooLarge.ID,
		})
	}

	sendErrorMessage(
		b, ctx, errorID,
		localizer.T(&i18n.LocalizeConfig{
			MessageID: localization.ErrorMessage.ID,
		}),
		note,
	)

	database.Q().LogError(
		extractorCtx.Context,
		database.LogErrorParams{
			ID:      errorID,
			Message: err.Error(),
		},
	)
}

func isChatWriteForbidden(err error) bool {
	return strings.Contains(err.Error(), "CHAT_WRITE_FORBIDDEN")
}

func isPermissionDenied(err error) bool {
	return strings.Contains(err.Error(), "not enough rights")
}

// isTelegramRequestTooLarge reports whether the error came from Telegram
// rejecting an upload because the request payload was too big - the Bot
// API answers oversized multipart uploads (a single file above the cap,
// or a media album whose combined size exceeds it) with HTTP 413
// "Request Entity Too Large".
func isTelegramRequestTooLarge(err error) bool {
	return strings.Contains(
		strings.ToLower(err.Error()),
		"request entity too large",
	)
}

func asBotError(err error) *util.Error {
	currentErr := err
	for currentErr != nil {
		var botError *util.Error
		if errors.As(currentErr, &botError) {
			return botError
		}
		currentErr = errors.Unwrap(currentErr)
	}
	return nil
}

func formatErrorMessage(ctx *ext.Context, message string, errorID string, note string) string {
	var suffix string
	if errorID != "" {
		if ctx.CallbackQuery != nil || ctx.InlineQuery != nil {
			suffix = " [" + errorID + "]"
		} else {
			suffix = " [<code>" + errorID + "</code>]"
		}
	}
	text := "⚠️ " + message + suffix
	if note != "" {
		text += "\n" + note
	}
	return text
}

func sendErrorMessage(
	b *gotgbot.Bot,
	ctx *ext.Context,
	errroID string,
	message string,
	note string,
) {
	message = formatErrorMessage(ctx, message, errroID, note)

	switch {
	case ctx.Message != nil:
		ctx.EffectiveMessage.Reply(b, message, nil)
	case ctx.CallbackQuery != nil:
		ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      message,
			ShowAlert: true,
		})
	case ctx.InlineQuery != nil:
		ctx.InlineQuery.Answer(b, nil,
			&gotgbot.AnswerInlineQueryOpts{
				CacheTime: util.Ptr(int64(0)),
				Button: &gotgbot.InlineQueryResultsButton{
					Text:           message,
					StartParameter: "start",
				},
			},
		)
	case ctx.ChosenInlineResult != nil:
		b.EditMessageText(
			message,
			&gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.ChosenInlineResult.InlineMessageId,
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
					IsDisabled: true,
				},
			},
		)
	}
}
