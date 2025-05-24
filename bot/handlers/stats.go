package handlers

import (
	"fmt"
	"govd/database"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

type stats struct {
	String    string
	UpdatedAt time.Time
}

var currentStats *stats
var updateInterval = 5 // minutes

var statsMessage = "users: %d\n" +
	"daily users: %d\n" +
	"groups: %d\n\n" +
	"downloads: %d\n" +
	"daily downloads: %d\n\n" +
	"updates every %d minutes"

var statsMessageNoData = "stats temporarily unavailable"

func StatsHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveMessage.Chat.Type != gotgbot.ChatTypePrivate {
		return nil
	}
	ctx.CallbackQuery.Answer(bot, nil)
	stats := GetStats()
	ctx.EffectiveMessage.EditText(
		bot,
		stats,
		&gotgbot.EditMessageTextOpts{
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

func UpdateStats() {
	users, err := database.GetUsersCount()
	if err != nil {
		users = 0
	}
	dailyUsers, err := database.GetDailyUserCount()
	if err != nil {
		dailyUsers = 0
	}
	groups, err := database.GetGroupsCount()
	if err != nil {
		groups = 0
	}
	downloads, err := database.GetMediaCount()
	if err != nil {
		downloads = 0
	}
	dailyDownloads, err := database.GetDailyMediaCount()
	if err != nil {
		dailyDownloads = 0
	}

	currentStats = &stats{
		String: fmt.Sprintf(
			statsMessage,
			users,
			dailyUsers,
			groups,
			downloads,
			dailyDownloads,
			updateInterval,
		),
		UpdatedAt: time.Now(),
	}
}

func GetStats() string {
	if currentStats == nil {
		UpdateStats()
		if currentStats == nil {
			currentStats = &stats{
				String:    statsMessageNoData,
				UpdatedAt: time.Now(),
			}
		}
	} else if currentStats.UpdatedAt.Add(time.Duration(updateInterval) * time.Minute).Before(time.Now()) {
		oldStats := currentStats
		UpdateStats()
		if currentStats == nil {
			currentStats = oldStats
		}
	}
	return currentStats.String
}
