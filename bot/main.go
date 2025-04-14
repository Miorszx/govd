package bot

import (
	"log"
	"os"
	"time"

	botHandlers "govd/bot/handlers"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/choseninlineresult"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/inlinequery"
)

var AllowedUpdates = []string{
	"message",
	"callback_query",
	"inline_query",
	"chosen_inline_result",
}

func Start() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatalf("BOT_TOKEN is not provided")
	}
	b, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: NewBotClient(),
	})
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})
	updater := ext.NewUpdater(dispatcher, nil)
	registerHandlers(dispatcher)
	err = updater.StartPolling(b, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9 * 60,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Minute * 10,
			},
			AllowedUpdates: AllowedUpdates,
		},
	})
	if err != nil {
		log.Fatalf("failed to start polling: %v", err)
	}
	log.Printf("bot started on: %s\n", b.User.Username)
}

func registerHandlers(dispatcher *ext.Dispatcher) {
	dispatcher.AddHandler(handlers.NewMessage(
		botHandlers.URLFilter,
		botHandlers.URLHandler,
	))
	dispatcher.AddHandler(handlers.NewCommand(
		"start",
		botHandlers.StartHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("start"),
		botHandlers.StartHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("help"),
		botHandlers.HelpHandler,
	))
	dispatcher.AddHandler(handlers.NewCommand(
		"settings",
		botHandlers.SettingsHandler,
	))
	dispatcher.AddHandler(handlers.NewCommand(
		"captions",
		botHandlers.CaptionsHandler,
	))
	dispatcher.AddHandler(handlers.NewCommand(
		"nsfw",
		botHandlers.NSFWHandler,
	))
	dispatcher.AddHandler(handlers.NewCommand(
		"limit",
		botHandlers.MediaGroupLimitHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("stats"),
		botHandlers.StatsHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("extractors"),
		botHandlers.ExtractorsHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("instances"),
		botHandlers.InstancesHandler,
	))
	dispatcher.AddHandler(handlers.NewInlineQuery(
		inlinequery.All,
		botHandlers.InlineDownloadHandler,
	))
	dispatcher.AddHandler(handlers.NewChosenInlineResult(
		choseninlineresult.All,
		botHandlers.InlineDownloadResultHandler,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		callbackquery.Equal("inline:loading"),
		botHandlers.InlineLoadingHandler,
	))
}
