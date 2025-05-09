package main

import (
	"flag"
	"fmt"
	"govd/bot"
	"govd/config"
	"govd/database"
	"govd/ext"
	"govd/logger"
	"govd/util"
	"os"
	"os/exec"
	"strconv"

	"net/http"
	_ "net/http/pprof" // profiling

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

var logLevel string

func main() {
	parseFlags()
	logger.Init(logLevel)
	defer logger.Sync()

	loadEnv()
	loadExtractorsConfig()
	startProfiler()
	checkFFmpeg()
	util.StartDownloadsCleanup()
	database.Start()

	zap.S().Debugf("loaded %d extractors", len(ext.List))

	go bot.Start()

	select {} // keep the main goroutine alive
}

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		zap.S().Fatal("error loading .env file")
	}
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

func parseFlags() {
	flag.StringVar(&logLevel, "log", "info", "log level (debug, info, warn, error)")
	flag.Parse()
}
