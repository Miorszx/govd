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
	"os"
	"os/exec"
	"strconv"

	"net/http"
	_ "net/http/pprof" // profiling

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// setup environment variables
	err := godotenv.Load()
	if err != nil {
		zap.S().Warn("failed to load .env file. using system env")
	}

	// setup extractors
	err = config.LoadExtractorConfigs()
	if err != nil {
		zap.S().Fatalf("failed to load extractor configs: %v", err)
	}
	zap.S().Debugf("loaded %d extractors", len(ext.List))
	zap.S().Debugf("loaded %d plugins", len(plugins.List))

	// check for ffmpeg binary
	_, err = exec.LookPath("ffmpeg")
	if err != nil {
		zap.S().Fatal("ffmpeg not found in PATH")
	}

	// setup pprof profiler
	profilerPort, err := strconv.Atoi(os.Getenv("PROFILER_PORT"))
	if err == nil && profilerPort > 0 {
		go func() {
			zap.S().Infof("starting profiler on port %d", profilerPort)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", profilerPort), nil); err != nil {
				zap.S().Fatalf("failed to start profiler: %v", err)
			}
		}()
	}

	// setup logger
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	allowLogFile, err := strconv.ParseBool(os.Getenv("LOG_FILE"))
	if err != nil {
		zap.S().Warn("failed to parse LOG_FILE env, using false")
		allowLogFile = false
	}
	logger.Init(logLevel, allowLogFile)
	defer logger.Sync()

	// cleanup downloads directory
	util.StartDownloadsCleanup()

	// setup database
	database.Start()

	// setup bot client
	go bot.Start()

	select {}
}
