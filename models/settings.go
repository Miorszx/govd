package models

import "gorm.io/gorm"

type GroupSettings struct {
	gorm.Model

	ChatID          int64 `gorm:"primaryKey"`
	NSFW            *bool `gorm:"default:false"`
	Captions        *bool `gorm:"default:false"`
	MediaGroupLimit int   `gorm:"default:10"`
	Silent          *bool `gorm:"default:false"`
}
