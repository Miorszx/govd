package handlers

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var buildHash = "unknown"
var branchName = "unknown"

func getInstanceMessage() string {
	return "current instance\n" +
		"go version: %s\n" +
		"build: <a href='%s'>%s</a>\n" +
		"branch: <a href='%s'>%s</a>\n\n" +
		"public instances\n" +
		"- @govd_bot | main official instance\n" +
		"\nwant to add your own instance? reach us on @govdsupport"
}

func InstancesHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	var commitURL string
	var branchURL string

	repoURL := os.Getenv("REPO_URL")
	if repoURL != "" {
		commitURL = fmt.Sprintf(
			"%s/tree/%s",
			repoURL,
			buildHash,
		)
		branchURL = fmt.Sprintf(
			"%s/tree/%s",
			repoURL,
			branchName,
		)
	}
	messageText := fmt.Sprintf(
		getInstanceMessage(),
		strings.TrimPrefix(runtime.Version(), "go"),
		commitURL,
		buildHash,
		branchURL,
		branchName,
	)
	ctx.CallbackQuery.Answer(bot, nil)
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
