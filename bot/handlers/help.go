package handlers

import (
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var helpMessage = "usage:\n" +
	"- you can add the bot to a group " +
	"to start catching sent links\n" +
	"- you can send a link to the bot privately " +
	"to download the media too\n" +
	"- you can use inline mode " +
	"to download media from any chat\n\n" +
	"group commands:\n" +
	"- /settings = show current settings\n" +
	"- /captions (true|false) = enable/disable descriptions\n" +
	"- /nsfw (true|false) = enable/disable nsfw content\n" +
	"- /limit (int) = set max items in media groups\n\n" +
	"note: the bot is still in beta, " +
	"so expect some bugs and missing features.\n"

var helpKeyboard = gotgbot.InlineKeyboardMarkup{
	InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
		{
			{
				Text:         "back",
				CallbackData: "start",
			},
		},
	},
}

func HelpHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	ctx.CallbackQuery.Answer(bot, nil)
	ctx.EffectiveMessage.EditText(
		bot,
		helpMessage,
		&gotgbot.EditMessageTextOpts{
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
			ReplyMarkup: helpKeyboard,
		},
	)
	return nil
}
