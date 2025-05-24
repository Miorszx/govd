package database

import "govd/models"

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
	err := DB.
		Model(&models.Media{}).
		Where("DATE(created_at) = DATE(NOW())").
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
	err := DB.
		Model(&models.User{}).
		Where("DATE(last_used) = DATE(NOW())").
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
