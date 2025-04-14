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
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	connectionString := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True",
		user, password, host, port, dbname,
	)
	db, err := gorm.Open(mysql.Open(connectionString), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			utc, _ := time.LoadLocation("Europe/Rome")
			return time.Now().In(utc)
		},
	})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	DB = db
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("failed to get database connection: %v", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	err = sqlDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	err = migrateDatabase()
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}
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
