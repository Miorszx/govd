package handlers

import (
	"fmt"
	"govd/database"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/dustin/go-humanize"
)

type stats struct {
	String    string
	UpdatedAt time.Time
}

var currentStats *stats
var updateInterval = 30 // minutes

var statsMessage = "users: <code>%s</code>\n" +
	"daily users: <code>%s</code>\n" +
	"groups: <code>%s</code>\n\n" +
	"downloads: <code>%s</code>\n" +
	"daily downloads: <code>%s</code>\n\n" +
	"traffic: <code>%s</code>\n" +
	"daily traffic: <code>%s</code>\n\n" +
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
	traffic, err := database.GetTraffic()
	if err != nil {
		traffic = 0
	}
	dailyTraffic, err := database.GetDailyTraffic()
	if err != nil {
		dailyTraffic = 0
	}

	currentStats = &stats{
		String: fmt.Sprintf(
			statsMessage,
			HumanizedInt(users),
			HumanizedInt(dailyUsers),
			HumanizedInt(groups),
			HumanizedInt(downloads),
			HumanizedInt(dailyDownloads),
			humanize.IBytes(uint64(traffic)),
			humanize.IBytes(uint64(dailyTraffic)),
			updateInterval,
		),
		UpdatedAt: time.Now(),
	}
}

func HumanizedInt(d int) string {
	return strings.ReplaceAll(humanize.Comma(int64(d)), ",", ".")
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
