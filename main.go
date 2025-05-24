package main

import (
	"fmt"
	"govd/bot"
	"govd/config"
	"govd/database"
	"govd/ext"
	"govd/logger"
	"govd/plugins"
	"govd/util"
	"os/exec"

	"net/http"
	_ "net/http/pprof" // profiling

	"go.uber.org/zap"
)

func main() {
	logger.Init()
	defer logger.Sync()

	// load environment variables and configurations
	config.Load()

	logger.SetLevel(config.Env.LogLevel)
	logger.SetLogFile(config.Env.LogFile)

	zap.S().Debugf("loaded %d extractors", len(ext.List))
	zap.S().Debugf("loaded %d plugins", len(plugins.List))

	// check for ffmpeg binary
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		zap.S().Fatal("ffmpeg not found in PATH")
	}

	// setup pprof profiler
	if config.Env.ProfilerPort > 0 {
		go func() {
			zap.S().Infof("starting profiler on port %d", config.Env.ProfilerPort)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", config.Env.ProfilerPort), nil); err != nil {
				zap.S().Fatalf("failed to start profiler: %v", err)
			}
		}()
	}

	// cleanup downloads directory
	util.StartDownloadsCleanup()

	// setup database
	database.Start()

	// setup bot client
	go bot.Start()

	select {}
}
