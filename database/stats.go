package database

import (
	"govd/models"
	"time"
)

func getDayRange() (time.Time, time.Time) {
	now := DB.NowFunc()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return start, end
}

func GetMediaCount() (int, error) {
	var count int64
	err := DB.
		Model(&models.Media{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func GetDailyMediaCount() (int, error) {
	var count int64

	start, end := getDayRange()
	err := DB.
		Model(&models.Media{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func GetUsersCount() (int, error) {
	var count int64
	err := DB.
		Model(&models.User{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func GetDailyUserCount() (int, error) {
	var count int64

	start, end := getDayRange()
	err := DB.
		Model(&models.User{}).
		Where("last_used >= ? AND last_used < ?", start, end).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func GetGroupsCount() (int, error) {
	var count int64
	err := DB.
		Model(&models.GroupSettings{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}
