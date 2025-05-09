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
	loadEnv()
	loadExtractorsConfig()
	startProfiler()
	checkFFmpeg()

	logLevel, allowLogFile := parseLogLevel()
	logger.Init(logLevel, allowLogFile)
	defer logger.Sync()

	util.StartDownloadsCleanup()
	database.Start()

	zap.S().Debugf("loaded %d extractors", len(ext.List))
	zap.S().Debugf("loaded %d plugins", len(plugins.List))

	go bot.Start()

	select {} // keep the main goroutine alive
}

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		zap.S().Fatal("error loading .env file")
	}
}

func parseLogLevel() (string, bool) {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	allowLogFile, err := strconv.ParseBool(os.Getenv("LOG_FILE"))
	if err != nil {
		return logLevel, false
	}
	return logLevel, allowLogFile
}

func checkFFmpeg() {
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		zap.S().Fatal("ffmpeg not found in PATH")
	}
}

func loadExtractorsConfig() {
	err := config.LoadExtractorConfigs()
	if err != nil {
		zap.S().Fatalf("error loading extractor configs: %v", err)
	}
}

func startProfiler() {
	profilerPort, err := strconv.Atoi(os.Getenv("PROFILER_PORT"))
	if err == nil && profilerPort > 0 {
		go func() {
			zap.S().Infof("starting profiler on port %d", profilerPort)
			http.ListenAndServe(fmt.Sprintf(":%d", profilerPort), nil)
		}()
	}
}
