package handlers

import (
	"govd/database"
	extractors "govd/ext"
	"slices"
	"sort"
	"strconv"
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
	CodeName string
	Total    int
	Daily    int
}

var currentStats *Stats

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

	var stats strings.Builder
	stats.WriteString("users: " + strconv.Itoa(users) + "\n")
	stats.WriteString("daily users: " + strconv.Itoa(dailyUsers) + "\n")
	stats.WriteString("groups: " + strconv.Itoa(groups) + "\n\n")

	stats.WriteString("downloads: " + strconv.Itoa(downloads) + "\n")
	stats.WriteString("daily downloads: " + strconv.Itoa(dailyDownloads) + "\n\n")

	stats.WriteString("extractors:\n")
	stats.WriteString("<blockquote expandable>")

	extractorStats := make([]*ExtractorStats, 0, len(extractors.List))
	codenames := make([]string, 0, len(extractors.List))

	for _, extractor := range extractors.List {
		if extractor.IsRedirect {
			continue
		}
		if slices.Contains(codenames, extractor.CodeName) {
			continue
		}
		codenames = append(codenames, extractor.CodeName)

		count, err := database.GetExtMediaCount(extractor.CodeName)
		if err != nil {
			count = 0
		}
		dailyCount, err := database.GetExtDailyMediaCount(extractor.CodeName)
		if err != nil {
			dailyCount = 0
		}
		extractorStats = append(extractorStats, &ExtractorStats{
			CodeName: extractor.CodeName,
			Total:    count,
			Daily:    dailyCount,
		})
	}
	sort.Slice(extractorStats, func(i, j int) bool {
		return extractorStats[i].Total > extractorStats[j].Total
	})
	for _, stat := range extractorStats {
		stats.WriteString(stat.CodeName + "\n")
		stats.WriteString("total: " + strconv.Itoa(stat.Total) + "\n")
		stats.WriteString("daily: " + strconv.Itoa(stat.Daily) + "\n\n")
	}
	stats.WriteString("</blockquote>")

	stats.WriteString("\n\nupdates every 30 minutes")

	currentStats = &Stats{
		String:    stats.String(),
		UpdatedAt: time.Now(),
	}
}

func GetStats() string {
	if currentStats == nil {
		UpdateStats()
		if currentStats == nil {
			currentStats = &Stats{
				String:    "stats temporarily unavailable",
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
