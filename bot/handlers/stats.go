package handlers

import (
	"fmt"
	"govd/database"
	extractors "govd/ext"
	"sort"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

type Stats struct {
	String    string
	UpdatedAt time.Time
}

type ExtractorStats struct {
	Name  string
	Total int
	Daily int
}

var currentStats *Stats

var statsMessage = "users: %d\n" +
	"daily users: %d\n" +
	"groups: %d\n\n" +
	"downloads: %d\n" +
	"daily downloads: %d\n\n" +
	"extractors:\n" +
	"<blockquote expandable>" +
	"%s" +
	"</blockquote>\n\n" +
	"updates every 30 minutes"

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

	extractorStats := make([]*ExtractorStats, 0, len(extractors.List))

	for _, extractor := range extractors.List {
		if extractor.IsRedirect || extractor.IsHidden {
			continue
		}

		count, err := database.GetExtMediaCount(extractor.CodeName)
		if err != nil {
			count = 0
		}
		dailyCount, err := database.GetExtDailyMediaCount(extractor.CodeName)
		if err != nil {
			dailyCount = 0
		}
		extractorStats = append(extractorStats, &ExtractorStats{
			Name:  extractor.Name,
			Total: count,
			Daily: dailyCount,
		})
	}
	sort.Slice(extractorStats, func(i, j int) bool {
		return extractorStats[i].Total > extractorStats[j].Total
	})
	entries := make([]string, 0, len(extractors.List))
	for _, stat := range extractorStats {
		entries = append(entries,
			fmt.Sprintf(
				"%s\ntotal: %d\ndaily: %d",
				stat.Name, stat.Total, stat.Daily,
			),
		)
	}

	currentStats = &Stats{
		String: fmt.Sprintf(
			statsMessage,
			users,
			dailyUsers,
			groups,
			downloads,
			dailyDownloads,
			strings.Join(entries, "\n\n"),
		),
		UpdatedAt: time.Now(),
	}
}

func GetStats() string {
	if currentStats == nil {
		UpdateStats()
		if currentStats == nil {
			currentStats = &Stats{
				String:    statsMessageNoData,
				UpdatedAt: time.Now(),
			}
		}
	} else if currentStats.UpdatedAt.Add(30 * time.Minute).Before(time.Now()) {
		oldStats := currentStats
		UpdateStats()
		if currentStats == nil {
			currentStats = oldStats
		}
	}
	return currentStats.String
}
