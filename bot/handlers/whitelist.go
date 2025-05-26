package handlers

import (
	"slices"

	"github.com/govdbot/govd/config"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func WhitelistHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	var effectiveID int64
	if ctx.EffectiveChat != nil {
		effectiveID = ctx.EffectiveChat.Id
	} else if ctx.EffectiveUser != nil {
		effectiveID = ctx.EffectiveUser.Id
	} else {
		return ext.ContinueGroups
	}
	if len(config.Env.Whitelist) > 0 {
		if !slices.Contains(config.Env.Whitelist, effectiveID) {
			if ctx.CallbackQuery != nil {
				ctx.CallbackQuery.Answer(bot, nil)
			} else if ctx.InlineQuery != nil {
				ctx.InlineQuery.Answer(bot, []gotgbot.InlineQueryResult{}, nil)
			}
			return ext.EndGroups
		}
	}
	return ext.ContinueGroups
}
