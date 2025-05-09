package database

import (
	"govd/models"

	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Start() {
	DB = connect()
	sqlDB, err := DB.DB()
	if err != nil {
		zap.L().Fatal("failed to get database connection", zap.Error(err))
	}
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	err = sqlDB.Ping()
	if err != nil {
		zap.L().Fatal("failed to ping database", zap.Error(err))
	}
	err = migrateDatabase()
	if err != nil {
		zap.L().Fatal("failed to migrate database", zap.Error(err))
	}
}

func connect() *gorm.DB {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	connectionString := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True",
		user, password, host, port, dbname,
	)
	zap.L().Debug("connecting to database")

	var conn *gorm.DB
	var err error

	maxRetries := 10
	retryCount := 0

	for retryCount < maxRetries {
		conn, err = gorm.Open(mysql.Open(connectionString), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
			NowFunc: func() time.Time {
				utc, _ := time.LoadLocation("Europe/Rome")
				return time.Now().In(utc)
			},
		})
		if err == nil {
			break
		}
		retryCount++
		zap.L().Warn("failed to connect to database",
			zap.Int("attempt", retryCount),
			zap.Int("max_retries", maxRetries),
		)
		if retryCount < maxRetries {
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		zap.L().Fatal("failed to connect to database", zap.Error(err))
	}
	return conn
}

func migrateDatabase() error {
	zap.L().Debug("migrating database")
	err := DB.AutoMigrate(
		&models.Media{},
		&models.MediaFormat{},
		&models.GroupSettings{},
		&models.User{},
	)
	if err != nil {
		return err
	}
	return nil
}
