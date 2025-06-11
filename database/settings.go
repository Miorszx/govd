package database

import (
	"errors"

	"github.com/govdbot/govd/config"
	"github.com/govdbot/govd/models"
	"gorm.io/gorm"
)

func GetGroupSettings(
	chatID int64,
) (*models.GroupSettings, error) {
	var groupSettings models.GroupSettings

	err := DB.
		Where(&models.GroupSettings{
			ChatID: chatID,
		}).
		First(&groupSettings).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			groupSettings = models.GroupSettings{
				ChatID:          chatID,
				Captions:        &config.Env.DefaultCaptions,
				Silent:          &config.Env.DefaultSilent,
				NSFW:            &config.Env.DefaultNSFW,
				MediaGroupLimit: config.Env.DefaultMediaGroupLimit,
			}
			err = DB.Create(&groupSettings).Error
			if err != nil {
				return nil, err
			}
		}
		return nil, err
	}

	return &groupSettings, nil
}

func UpdateGroupSettings(
	chatID int64,
	settings *models.GroupSettings,
) error {
	err := DB.
		Where(&models.GroupSettings{
			ChatID: chatID,
		}).
		Updates(settings).
		Error
	if err != nil {
		return err
	}
	return nil
}
