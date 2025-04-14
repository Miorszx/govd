package models

type Plugin = func(*DownloadedMedia) error
