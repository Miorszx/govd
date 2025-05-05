package handlers

import (
	"fmt"
	"os"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var startMessage = "govd is an open-source telegram bot " +
	"that allows you to download medias from " +
	"various platforms."

func getStartKeyboard(bot *gotgbot.Bot) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text: "add to group",
					Url: fmt.Sprintf(
						"https://t.me/%s?startgroup=true",
						bot.Username,
					),
				},
			},
			{
				{
					Text:         "usage",
					CallbackData: "help",
				},
				{
					Text:         "stats",
					CallbackData: "stats",
				},
			},
			{
				{
					Text:         "extractors",
					CallbackData: "extractors",
				},
				{
					Text: "support",
					Url:  "https://t.me/govdsupport",
				},
			},
			{
				{
					Text:         "instances",
					CallbackData: "instances",
				},
				{
					Text: "github",
					Url:  os.Getenv("REPO_URL"),
				},
			},
		},
	}
}

func StartHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveMessage.Chat.Type != gotgbot.ChatTypePrivate {
		ctx.EffectiveMessage.Reply(
			bot,
			"i'm online! i'll download every media in this group.",
			nil,
		)
		return nil
	}
	keyboard := getStartKeyboard(bot)
	if ctx.Update.Message != nil {
		ctx.EffectiveMessage.Reply(
			bot,
			startMessage,
			&gotgbot.SendMessageOpts{
				ReplyMarkup: &keyboard,
			},
		)
	} else if ctx.Update.CallbackQuery != nil {
		ctx.CallbackQuery.Answer(bot, nil)
		ctx.EffectiveMessage.EditText(
			bot,
			startMessage,
			&gotgbot.EditMessageTextOpts{
				ReplyMarkup: keyboard,
			},
		)

	}
	return nil
}
