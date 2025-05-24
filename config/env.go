package config

import (
	"govd/models"
	"os"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

var Env = GetDefaultConfig()

func LoadEnv() error {
	if value := os.Getenv("DB_HOST"); value != "" {
		Env.DBHost = value
	} else {
		zap.S().Fatalf("DB_HOST env is not set")
	}
	if value := os.Getenv("DB_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			Env.DBPort = port
		} else {
			zap.S().Fatal("DB_PORT env is not a valid integer")
		}
	} else {
		zap.S().Fatalf("DB_PORT env is not set")
	}
	if value := os.Getenv("DB_NAME"); value != "" {
		Env.DBName = value
	} else {
		zap.S().Fatal("DB_NAME env is not set")
	}
	if value := os.Getenv("DB_USER"); value != "" {
		Env.DBUser = value
	} else {
		zap.S().Fatalf("DB_USER env is not set")
	}
	if value := os.Getenv("DB_PASSWORD"); value != "" {
		Env.DBPassword = value
	} else {
		zap.S().Fatalf("DB_PASSWORD env is not set")
	}
	if value := os.Getenv("BOT_TOKEN"); value != "" {
		Env.BotToken = value
	} else {
		zap.S().Fatalf("BOT_TOKEN env is not set")
	}
	if value := os.Getenv("BOT_API_URL"); value != "" {
		Env.BotAPIURL = value
	} else {
		zap.S().Warnf("BOT_API_URL is not set, using default %s", Env.BotAPIURL)
	}
	if value := os.Getenv("CONCURRENT_UPDATES"); value != "" {
		if updates, err := strconv.Atoi(value); err == nil {
			Env.ConcurrentUpdates = updates
		} else {
			zap.S().Fatal("CONCURRENT_UPDATES env is not a valid integer")
		}
	} else {
		zap.S().Warnf("CONCURRENT_UPDATES is not set, using default %d", Env.ConcurrentUpdates)
	}
	if value := os.Getenv("DOWNLOADS_DIR"); value != "" {
		Env.DownloadsDirectory = value
	} else {
		zap.S().Warnf("DOWNLOADS_DIR is not set, using default %s", Env.DownloadsDirectory)
	}
	if value := os.Getenv("HTTP_PROXY"); value != "" {
		Env.HTTPProxy = value
	}
	if value := os.Getenv("HTTPS_PROXY"); value != "" {
		Env.HTTPSProxy = value
	}
	if value := os.Getenv("NO_PROXY"); value != "" {
		Env.NoProxy = value
	}
	if value := os.Getenv("MAX_DURATION"); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			Env.MaxDuration = duration
		} else {
			zap.S().Fatalf("MAX_DURATION env is not a valid duration: %v", err)
		}
	}
	if value := os.Getenv("MAX_FILE_SIZE"); value != "" {
		if size, err := strconv.Atoi(value); err == nil {
			Env.MaxFileSize = int64(size)
		}
	}
	if value := os.Getenv("REPO_URL"); value != "" {
		Env.RepoURL = value
	}
	if value := os.Getenv("PROFILER_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			Env.ProfilerPort = port
		} else {
			zap.S().Fatal("PROFILER_PORT env is not a valid integer")
		}
	}
	if value := os.Getenv("LOG_LEVEL"); value != "" {
		Env.LogLevel = value
	}
	if value := os.Getenv("LOG_FILE"); value != "" {
		if logFile, err := strconv.ParseBool(value); err == nil {
			Env.LogFile = logFile
		} else {
			zap.S().Fatal("LOG_FILE env is not a valid boolean")
		}
	}
	return nil
}

func GetDefaultConfig() *models.EnvConfig {
	return &models.EnvConfig{
		DBHost: "localhost",
		DBPort: 3306,
		DBName: "govd",
		DBUser: "govd",

		BotAPIURL:         gotgbot.DefaultAPIURL,
		ConcurrentUpdates: 50,

		DownloadsDirectory: "downloads",

		MaxDuration: time.Hour,
		MaxFileSize: 1000,
		RepoURL:     "https://github.com/stefanodvx/govd",
		LogLevel:    "info",
	}
}
