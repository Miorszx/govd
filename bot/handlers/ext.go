package handlers

import (
	extractors "govd/ext"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func ExtractorsHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	ctx.CallbackQuery.Answer(bot, nil)

	messageText := "available extractors:\n"
	extractorNames := make([]string, 0, len(extractors.List))
	for _, extractor := range extractors.List {
		if extractor.IsRedirect {
			continue
		}
		extractorNames = append(extractorNames, extractor.Name)
	}
	messageText += strings.Join(extractorNames, ", ")

	ctx.EffectiveMessage.EditText(
		bot,
		messageText,
		&gotgbot.EditMessageTextOpts{
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{
						{
							Text:         "back",
							CallbackData: "start",
						},
					},
				},
			},
		},
	)
	return nil
}
