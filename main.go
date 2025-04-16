package main

import (
	"fmt"
	"govd/bot"
	"govd/database"
	"govd/util"
	"log"
	"net/http"
	"os"
	"strconv"

	_ "net/http/pprof"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}
	profilerPort, err := strconv.Atoi(os.Getenv("PROFILER_PORT"))
	if err == nil && profilerPort > 0 {
		go func() {
			http.ListenAndServe(fmt.Sprintf("localhost:%d", profilerPort), nil)
		}()
	}
	util.CleanupDownloadsDir()
	util.StartDownloadsCleanup()
	ok := util.CheckFFmpeg()
	if !ok {
		log.Fatal("ffmpeg executable not found. please install it or add it to your PATH")
	}
	database.Start()
	go bot.Start()

	select {} // keep the main goroutine alive
}
