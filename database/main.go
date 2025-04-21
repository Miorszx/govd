package database

import (
	"govd/models"

	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Start() {
	DB = connect()
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("failed to get database connection: %v", err)
	}
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	err = sqlDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	err = migrateDatabase()
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
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
		log.Printf("failed to connect to database (attempt %d/%d)", retryCount, maxRetries)
		if retryCount < maxRetries {
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		log.Fatalf("failed to connect to database after %d attempts: %v", maxRetries, err)
	}
	return conn
}

func migrateDatabase() error {
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
