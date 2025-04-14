package main

import (
	"govd/bot"
	"govd/database"
	"govd/util"
	"log"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}
	ok := util.CheckFFmpeg()
	if !ok {
		log.Fatal("ffmpeg executable not found. please install it or add it to your PATH")
	}
	database.Start()
	go bot.Start()

	select {} // keep the main goroutine alive
}
