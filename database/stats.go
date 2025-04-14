package database

import "govd/models"

func GetMediaCount() (int64, error) {
	var count int64
	err := DB.
		Model(&models.Media{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func GetUsersCount() (int64, error) {
	var count int64
	err := DB.
		Model(&models.User{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func GetGroupsCount() (int64, error) {
	var count int64
	err := DB.
		Model(&models.GroupSettings{}).
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func GetDailyUserCount() (int64, error) {
	var count int64
	err := DB.
		Model(&models.User{}).
		Where("DATE(last_used) = DATE(NOW())").
		Count(&count).
		Error
	if err != nil {
		return 0, err
	}
	return count, nil
}
