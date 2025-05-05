package handlers

import (
	"fmt"
	"govd/database"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

type Stats struct {
	TotalUsers      int64
	TotalGroups     int64
	TotalDailyUsers int64
	TotalMedia      int64
	UpdatedAt       time.Time
}

var lastSavedStats *Stats

var statsMessage = "users: %d\nusers today: %d\ngroups: %d\ndownloads: %d\n\nupdates every 10 minutes"

func StatsHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveMessage.Chat.Type != gotgbot.ChatTypePrivate {
		return nil
	}
	ctx.CallbackQuery.Answer(bot, nil)
	stats := GetStats()
	ctx.EffectiveMessage.EditText(
		bot,
		fmt.Sprintf(
			statsMessage,
			stats.TotalUsers,
			stats.TotalDailyUsers,
			stats.TotalGroups,
			stats.TotalMedia,
		),
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
	totalUsers, err := database.GetUsersCount()
	if err != nil {
		return
	}
	totalGroups, err := database.GetGroupsCount()
	if err != nil {
		return
	}
	totalDailyUsers, err := database.GetDailyUserCount()
	if err != nil {
		return
	}
	totalMedia, err := database.GetMediaCount()
	if err != nil {
		return
	}
	lastSavedStats = &Stats{
		TotalUsers:      totalUsers,
		TotalGroups:     totalGroups,
		TotalDailyUsers: totalDailyUsers,
		TotalMedia:      totalMedia,
		UpdatedAt:       time.Now(),
	}
}

func GetStats() *Stats {
	if lastSavedStats == nil {
		UpdateStats()
	}
	if lastSavedStats.UpdatedAt.Add(10 * time.Minute).Before(time.Now()) {
		UpdateStats()
	}
	return lastSavedStats
}
